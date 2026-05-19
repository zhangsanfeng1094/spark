package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"spark/internal/config"
)

type TestResult struct {
	Success bool
	Message string
	Latency time.Duration
	LogPath string
}

type Request struct {
	EndpointType string
	URL          string
	APIKey       string
	Org          string
	Project      string
	Payload      map[string]any
}

type Response struct {
	Status int
	Body   []byte
}

type JSONPoster interface {
	PostJSON(ctx context.Context, req Request) (Response, error)
}

type CurlPoster struct {
	Path    string
	Timeout time.Duration
}

type connectionTestRequest struct {
	endpointType string
	url          string
	payload      map[string]any
}

type connectionTestResult struct {
	request connectionTestRequest
	status  int
	body    []byte
	err     error
	latency time.Duration
}

func TestModelConnection(profile *config.Profile, model string) TestResult {
	return testModelConnection(profile, model, CurlPoster{Path: "curl", Timeout: 8 * time.Second})
}

func testModelConnection(profile *config.Profile, model string, poster JSONPoster) TestResult {
	logPath := AppendModelConnectionTestLogf("===== model connection test start =====")
	if profile == nil {
		AppendModelConnectionTestLogf("result=fail reason=%q", "Profile is nil")
		return TestResult{Success: false, Message: "Profile is nil", LogPath: logPath}
	}

	baseURL := strings.TrimSpace(profile.OpenAIBaseURL)
	if baseURL == "" {
		AppendModelConnectionTestLogf("result=fail reason=%q", "Base URL is empty")
		return TestResult{Success: false, Message: "Base URL is empty", LogPath: logPath}
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	apiTypes := config.ParseOpenAIAPITypes(profile.OpenAIAPIType)
	if len(apiTypes) == 0 {
		apiTypes = config.ParseOpenAIAPITypes(config.DefaultOpenAIAPIType)
	}
	apiKey := strings.TrimSpace(profile.OpenAIAPIKey)
	AppendModelConnectionTestLogf(
		"config base_url=%q api_types=%q has_api_key=%t org=%q project=%q",
		baseURL,
		strings.Join(apiTypes, ","),
		apiKey != "",
		strings.TrimSpace(profile.OpenAIOrg),
		strings.TrimSpace(profile.OpenAIProject),
	)

	testModel := pickTestModel(profile, model)
	AppendModelConnectionTestLogf("resolved test_model=%q", testModel)

	testStart := time.Now()
	reqSpecs, err := buildConnectionTestRequests(baseURL, apiTypes, testModel)
	if err != nil {
		AppendModelConnectionTestLogf("result=fail reason=%q", err.Error())
		return TestResult{Success: false, Message: "Connection failed: " + err.Error(), LogPath: logPath}
	}
	endpoints := make([]string, 0, len(reqSpecs))
	for _, reqSpec := range reqSpecs {
		endpoints = append(endpoints, reqSpec.endpointType)
	}
	AppendModelConnectionTestLogf("request_endpoints=%q", strings.Join(endpoints, ","))

	if poster == nil {
		poster = CurlPoster{Path: "curl", Timeout: 8 * time.Second}
	}
	const perRequestTimeout = 8 * time.Second
	totalTimeout := perRequestTimeout*time.Duration(len(reqSpecs)) + 4*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	results := make([]connectionTestResult, 0, len(reqSpecs))
	for _, reqSpec := range reqSpecs {
		AppendModelConnectionTestLogf("request endpoint=%s transport=curl url=%q", reqSpec.endpointType, reqSpec.url)
		start := time.Now()
		resp, err := poster.PostJSON(ctx, Request{
			EndpointType: reqSpec.endpointType,
			URL:          reqSpec.url,
			APIKey:       apiKey,
			Org:          strings.TrimSpace(profile.OpenAIOrg),
			Project:      strings.TrimSpace(profile.OpenAIProject),
			Payload:      reqSpec.payload,
		})
		latency := time.Since(start)
		results = append(results, connectionTestResult{
			request: reqSpec,
			status:  resp.Status,
			body:    resp.Body,
			err:     err,
			latency: latency,
		})
		if err != nil {
			AppendModelConnectionTestLogf("request endpoint=%s err=%q latency_ms=%d", reqSpec.endpointType, err.Error(), latency.Milliseconds())
			if ctx.Err() != nil {
				AppendModelConnectionTestLogf("request loop interrupted by context deadline after endpoint=%s", reqSpec.endpointType)
				break
			}
			continue
		}
		AppendModelConnectionTestLogf(
			"request endpoint=%s status=%d latency_ms=%d body=%q",
			reqSpec.endpointType,
			resp.Status,
			latency.Milliseconds(),
			truncateConnectionLogBody(string(resp.Body), 512),
		)
	}
	totalLatency := time.Since(testStart)
	if len(results) == 0 {
		msg := "Connection failed: no request executed"
		AppendModelConnectionTestLogf("result=fail reason=%q", msg)
		return TestResult{Success: false, Message: msg, Latency: totalLatency, LogPath: logPath}
	}

	allSucceeded := true
	for _, r := range results {
		if r.err != nil || r.status < 200 || r.status >= 300 {
			allSucceeded = false
			break
		}
	}
	summary := summarizeConnectionTestResults(results)
	if allSucceeded {
		msg := fmt.Sprintf("OK (model: %s; endpoints: %s)", testModel, summary)
		AppendModelConnectionTestLogf("result=pass model=%q summary=%q total_latency_ms=%d", testModel, summary, totalLatency.Milliseconds())
		return TestResult{
			Success: true,
			Message: msg,
			Latency: totalLatency,
			LogPath: logPath,
		}
	}

	reason := ""
	for _, r := range results {
		if r.err != nil {
			if ctx.Err() != nil {
				reason = fmt.Sprintf("timeout at %s", r.request.endpointType)
			} else {
				reason = fmt.Sprintf("%s error: %s", r.request.endpointType, r.err.Error())
			}
			break
		}
		if r.status < 200 || r.status >= 300 {
			if errMsg := extractEndpointErrorMessage(r.body); errMsg != "" {
				reason = fmt.Sprintf("%s HTTP %d: %s", r.request.endpointType, r.status, errMsg)
			} else {
				reason = fmt.Sprintf("%s HTTP %d", r.request.endpointType, r.status)
			}
			break
		}
	}
	if reason == "" {
		reason = "unknown failure"
	}
	msg := fmt.Sprintf("Failed (%s; endpoints: %s)", reason, summary)
	AppendModelConnectionTestLogf("result=fail reason=%q summary=%q total_latency_ms=%d", reason, summary, totalLatency.Milliseconds())
	return TestResult{
		Success: false,
		Message: msg,
		Latency: totalLatency,
		LogPath: logPath,
	}
}

func buildConnectionTestRequests(baseURL string, apiTypes []string, model string) ([]connectionTestRequest, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("empty base url")
	}
	if len(apiTypes) == 0 {
		apiTypes = config.ParseOpenAIAPITypes(config.DefaultOpenAIAPIType)
	}
	reqs := make([]connectionTestRequest, 0, len(apiTypes))
	for _, endpointType := range apiTypes {
		req, err := buildConnectionTestRequest(base, endpointType, model)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, req)
	}
	return reqs, nil
}

func buildConnectionTestRequest(base, endpointType, model string) (connectionTestRequest, error) {
	path, payload, err := connectionTestPayload(strings.TrimSpace(model), endpointType)
	if err != nil {
		return connectionTestRequest{}, err
	}
	if endpointType == config.OpenAIAPITypeAnthropicMessages && strings.HasSuffix(base, "/v1") && path == "/v1/messages" {
		path = "/messages"
	}
	return connectionTestRequest{endpointType: endpointType, url: base + path, payload: payload}, nil
}

func connectionTestPayload(model, endpointType string) (string, map[string]any, error) {
	if model == "" {
		model = "gpt-4o-mini"
	}
	switch endpointType {
	case config.OpenAIAPITypeResponses:
		return "/responses", map[string]any{
			"model":             model,
			"input":             "ping",
			"max_output_tokens": 16,
			"stream":            false,
		}, nil
	case config.OpenAIAPITypeChatCompletions:
		return "/chat/completions", map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": "ping"},
			},
			"max_tokens": 16,
			"stream":     false,
		}, nil
	case config.OpenAIAPITypeGeminiGenerateContent:
		return "/models/" + model + ":generateContent", map[string]any{
			"contents": []map[string]any{
				{
					"role": "user",
					"parts": []map[string]any{
						{"text": "ping"},
					},
				},
			},
			"generationConfig": map[string]any{
				"maxOutputTokens": 16,
			},
		}, nil
	case config.OpenAIAPITypeAnthropicMessages:
		return "/v1/messages", map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": "ping"},
			},
			"max_tokens": 16,
			"stream":     false,
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported endpoint type: %s", endpointType)
	}
}

func (p CurlPoster) PostJSON(ctx context.Context, req Request) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	path := strings.TrimSpace(p.Path)
	if path == "" {
		path = "curl"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	body, err := json.Marshal(req.Payload)
	if err != nil {
		return Response{}, err
	}

	args := curlPostJSONArgs(req, timeout)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(string(body))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	resp, parseErr := parseCurlResponse(stdout.Bytes())
	if parseErr != nil {
		if runErr != nil {
			return resp, fmt.Errorf("%s: %w", curlErrorMessage(stderr.String(), runErr), parseErr)
		}
		return resp, parseErr
	}
	if runErr != nil {
		return resp, curlErrorMessage(stderr.String(), runErr)
	}
	return resp, nil
}

func curlPostJSONArgs(req Request, timeout time.Duration) []string {
	seconds := strconv.FormatFloat(timeout.Seconds(), 'f', -1, 64)
	args := []string{
		"--silent",
		"--show-error",
		"--location",
		"--max-time", seconds,
		"--connect-timeout", seconds,
		"--request", "POST",
		req.URL,
		"--header", "Content-Type: application/json",
		"--header", "Accept: application/json",
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey != "" {
		switch req.EndpointType {
		case config.OpenAIAPITypeAnthropicMessages:
			args = append(args, "--header", "x-api-key: "+apiKey)
		default:
			args = append(args, "--header", "Authorization: Bearer "+apiKey)
		}
	}
	if req.EndpointType == config.OpenAIAPITypeAnthropicMessages {
		args = append(args, "--header", "anthropic-version: 2023-06-01")
	}
	if req.EndpointType != config.OpenAIAPITypeGeminiGenerateContent && req.EndpointType != config.OpenAIAPITypeAnthropicMessages {
		if org := strings.TrimSpace(req.Org); org != "" {
			args = append(args, "--header", "OpenAI-Organization: "+org)
		}
		if project := strings.TrimSpace(req.Project); project != "" {
			args = append(args, "--header", "OpenAI-Project: "+project)
		}
	}
	args = append(args,
		"--data-binary", "@-",
		"--write-out", "\n__SPARK_HTTP_STATUS__:%{http_code}",
	)
	return args
}

func parseCurlResponse(out []byte) (Response, error) {
	const marker = "\n__SPARK_HTTP_STATUS__:"
	idx := bytes.LastIndex(out, []byte(marker))
	if idx < 0 {
		return Response{Body: out}, fmt.Errorf("curl output missing HTTP status")
	}
	body := out[:idx]
	statusText := strings.TrimSpace(string(out[idx+len(marker):]))
	status, err := strconv.Atoi(statusText)
	if err != nil {
		return Response{Body: body}, fmt.Errorf("parse curl HTTP status %q: %w", statusText, err)
	}
	return Response{Status: status, Body: body}, nil
}

func curlErrorMessage(stderr string, err error) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("curl failed: %w", err)
	}
	return fmt.Errorf("curl failed: %s", stderr)
}

func summarizeConnectionTestResults(results []connectionTestResult) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if r.err != nil {
			parts = append(parts, fmt.Sprintf("%s=ERR", r.request.endpointType))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", r.request.endpointType, r.status))
	}
	return strings.Join(parts, ",")
}

func extractEndpointErrorMessage(body []byte) string {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		if errObj, ok := parsed["error"].(map[string]any); ok {
			for _, key := range []string{"message", "type", "code"} {
				if v, ok := errObj[key].(string); ok && strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
		if msg, ok := parsed["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return strings.TrimSpace(msg)
		}
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	return truncateConnectionLogBody(trimmed, 160)
}

var modelConnectionTestLogMu sync.Mutex

func AppendModelConnectionTestLogf(format string, args ...any) string {
	logPath := strings.TrimSpace(os.Getenv("SPARK_MODEL_TEST_LOG"))
	if logPath == "" {
		configPath, err := config.ConfigPath()
		if err != nil {
			return ""
		}
		logPath = filepath.Join(filepath.Dir(configPath), "model_connection_test.log")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return logPath
	}

	modelConnectionTestLogMu.Lock()
	defer modelConnectionTestLogMu.Unlock()

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return logPath
	}
	defer f.Close()

	line := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(f, "%s [model-test] %s\n", time.Now().Format(time.RFC3339), line)
	return logPath
}

func truncateConnectionLogBody(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func pickTestModel(profile *config.Profile, model string) string {
	testModel := strings.TrimSpace(model)
	if testModel != "" {
		return testModel
	}
	if profile != nil {
		if strings.TrimSpace(profile.DefaultModel) != "" {
			return strings.TrimSpace(profile.DefaultModel)
		}
		if len(profile.Models) > 0 && strings.TrimSpace(profile.Models[0]) != "" {
			return strings.TrimSpace(profile.Models[0])
		}
	}
	return "gpt-3.5-turbo"
}
