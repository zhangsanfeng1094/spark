package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"spark/internal/config"
	"spark/internal/integrations"
)

type TestResult struct {
	Success bool
	Message string
	Latency time.Duration
	LogPath string
}

type endpointProbeResult struct {
	endpointType string
	status       int
	body         []byte
	err          error
	latency      time.Duration
}

func TestModelConnection(profile *config.Profile, model string) TestResult {
	logPath := appendModelConnectionTestLogf("===== model connection test start =====")
	if profile == nil {
		appendModelConnectionTestLogf("result=fail reason=%q", "Profile is nil")
		return TestResult{Success: false, Message: "Profile is nil", LogPath: logPath}
	}

	baseURL := strings.TrimSpace(profile.OpenAIBaseURL)
	if baseURL == "" {
		appendModelConnectionTestLogf("result=fail reason=%q", "Base URL is empty")
		return TestResult{Success: false, Message: "Base URL is empty", LogPath: logPath}
	}

	// Normalize base URL
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	apiTypes := config.ParseOpenAIAPITypes(profile.OpenAIAPIType)
	if len(apiTypes) == 0 {
		apiTypes = []string{config.OpenAIAPITypeAuto}
	}

	apiKey := strings.TrimSpace(profile.OpenAIAPIKey)
	appendModelConnectionTestLogf(
		"input base_url=%q api_type=%q model_arg=%q default_model=%q models=%q api_key_set=%t org=%q",
		baseURL,
		profile.OpenAIAPIType,
		strings.TrimSpace(model),
		strings.TrimSpace(profile.DefaultModel),
		strings.Join(profile.Models, ","),
		apiKey != "",
		strings.TrimSpace(profile.OpenAIOrg),
	)

	// Build minimal test request
	testModel := pickTestModel(profile, model)
	appendModelConnectionTestLogf("resolved test_model=%q", testModel)

	const perEndpointTimeout = 8 * time.Second

	endpointOrder := make([]string, 0, 2)
	if apiTypes[0] == config.OpenAIAPITypeAuto || config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeResponses) {
		endpointOrder = append(endpointOrder, config.OpenAIAPITypeResponses)
	}
	if apiTypes[0] == config.OpenAIAPITypeAuto || config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeChatCompletions) {
		endpointOrder = append(endpointOrder, config.OpenAIAPITypeChatCompletions)
	}
	if len(endpointOrder) == 0 {
		endpointOrder = append(endpointOrder, config.OpenAIAPITypeResponses)
	}
	appendModelConnectionTestLogf("endpoint_order=%q", strings.Join(endpointOrder, ","))
	totalTimeout := perEndpointTimeout*time.Duration(len(endpointOrder)) + 4*time.Second

	testStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	client := &http.Client{Timeout: perEndpointTimeout}
	results := make([]endpointProbeResult, 0, len(endpointOrder))
	for _, endpointType := range endpointOrder {
		start := time.Now()
		status, body, err := integrations.ProbeOpenAIEndpoint(
			ctx,
			client,
			baseURL,
			apiKey,
			profile.OpenAIOrg,
			testModel,
			endpointType,
			perEndpointTimeout,
		)
		latency := time.Since(start)
		results = append(results, endpointProbeResult{
			endpointType: endpointType,
			status:       status,
			body:         body,
			err:          err,
			latency:      latency,
		})
		if err != nil {
			appendModelConnectionTestLogf("probe endpoint=%s err=%q latency_ms=%d", endpointType, err.Error(), latency.Milliseconds())
			if ctx.Err() != nil {
				appendModelConnectionTestLogf("probe loop interrupted by context deadline after endpoint=%s", endpointType)
				break
			}
			continue
		}
		appendModelConnectionTestLogf(
			"probe endpoint=%s status=%d latency_ms=%d body=%q",
			endpointType,
			status,
			latency.Milliseconds(),
			truncateConnectionLogBody(string(body), 512),
		)
	}

	if len(results) == 0 {
		msg := "Connection failed: no probe executed"
		appendModelConnectionTestLogf("result=fail reason=%q", msg)
		return TestResult{Success: false, Message: msg, Latency: time.Since(testStart), LogPath: logPath}
	}

	allSucceeded := true
	for _, r := range results {
		if r.err != nil || r.status < 200 || r.status >= 300 {
			allSucceeded = false
			break
		}
	}

	totalLatency := time.Since(testStart)
	summary := summarizeEndpointProbeResults(results)
	if allSucceeded {
		msg := fmt.Sprintf("OK (model: %s; endpoints: %s)", testModel, summary)
		appendModelConnectionTestLogf("result=pass model=%q summary=%q total_latency_ms=%d", testModel, summary, totalLatency.Milliseconds())
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
			if urlErr, ok := r.err.(*url.Error); ok && urlErr.Timeout() {
				reason = fmt.Sprintf("timeout at %s", r.endpointType)
			} else if errors.Is(r.err, context.DeadlineExceeded) {
				reason = fmt.Sprintf("timeout at %s", r.endpointType)
			} else {
				reason = fmt.Sprintf("%s error: %s", r.endpointType, r.err.Error())
			}
			break
		}
		if r.status < 200 || r.status >= 300 {
			if errMsg := extractEndpointErrorMessage(r.body); errMsg != "" {
				reason = fmt.Sprintf("%s HTTP %d: %s", r.endpointType, r.status, errMsg)
			} else {
				reason = fmt.Sprintf("%s HTTP %d", r.endpointType, r.status)
			}
			break
		}
	}
	if reason == "" {
		reason = "unknown failure"
	}
	msg := fmt.Sprintf("Failed (%s; endpoints: %s)", reason, summary)
	appendModelConnectionTestLogf("result=fail reason=%q summary=%q total_latency_ms=%d", reason, summary, totalLatency.Milliseconds())
	return TestResult{
		Success: false,
		Message: msg,
		Latency: totalLatency,
		LogPath: logPath,
	}
}

func summarizeEndpointProbeResults(results []endpointProbeResult) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if r.err != nil {
			parts = append(parts, fmt.Sprintf("%s=ERR", r.endpointType))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", r.endpointType, r.status))
	}
	return strings.Join(parts, ",")
}

func extractEndpointErrorMessage(body []byte) string {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if len(body) == 0 || json.Unmarshal(body, &errResp) != nil || errResp.Error.Message == "" {
		return ""
	}
	if errResp.Error.Type != "" {
		return fmt.Sprintf("[%s] %s", errResp.Error.Type, errResp.Error.Message)
	}
	return errResp.Error.Message
}

var modelConnectionLogMu sync.Mutex

func appendModelConnectionTestLogf(format string, args ...any) string {
	modelConnectionLogMu.Lock()
	defer modelConnectionLogMu.Unlock()

	logPath := strings.TrimSpace(os.Getenv("SPARK_MODEL_TEST_LOG"))
	if logPath == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return ""
		}
		logPath = filepath.Join(home, ".spark", "logs", fmt.Sprintf("model-connection-%s.log", time.Now().Format("2006-01-02")))
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return ""
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return ""
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

func DetectOpenAIAPIType(profile *config.Profile, model string) (string, error) {
	if profile == nil {
		return "", fmt.Errorf("profile is nil")
	}
	baseURL := strings.TrimSpace(profile.OpenAIBaseURL)
	if baseURL == "" {
		return "", fmt.Errorf("base URL is empty")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	testModel := pickTestModel(profile, model)

	apiKey := strings.TrimSpace(profile.OpenAIAPIKey)
	client := &http.Client{Timeout: 8 * time.Second}
	return integrations.DetectOpenAIAPIType(
		context.Background(),
		baseURL,
		apiKey,
		profile.OpenAIOrg,
		testModel,
		client,
		12*time.Second,
		6*time.Second,
	)
}

func FetchOpenAIModels(profile *config.Profile) ([]string, error) {
	if profile == nil {
		return nil, fmt.Errorf("profile is nil")
	}
	baseURL := strings.TrimSpace(profile.OpenAIBaseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is empty")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	client := &http.Client{Timeout: 10 * time.Second}
	return fetchOpenAIModelsWithClient(baseURL, strings.TrimSpace(profile.OpenAIAPIKey), strings.TrimSpace(profile.OpenAIOrg), strings.TrimSpace(profile.OpenAIProject), client)
}

func fetchOpenAIModelsWithClient(baseURL, apiKey, org, project string, client *http.Client) ([]string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("empty base url")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if org != "" {
		req.Header.Set("OpenAI-Organization", org)
	}
	if project != "" {
		req.Header.Set("OpenAI-Project", project)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models request failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	models, err := parseOpenAIModelsResponse(body)
	if err != nil {
		return nil, err
	}
	sort.Strings(models)
	return models, nil
}

func parseOpenAIModelsResponse(body []byte) ([]string, error) {
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse models response: %w", err)
	}

	models := make([]string, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		models = append(models, id)
	}
	return config.NormalizeModels(models), nil
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
