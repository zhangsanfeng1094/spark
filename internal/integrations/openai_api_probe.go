package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"spark/internal/config"
)

const (
	defaultProbeHTTPTimeout     = 8 * time.Second
	defaultProbeTotalTimeout    = 12 * time.Second
	defaultProbeEndpointTimeout = 6 * time.Second
	defaultProbeModel           = "gpt-4o-mini"
	maxProbeErrorBodyBytes      = 64 * 1024
)

func detectOpenAIAPIType(baseURL, apiKey, model string) (string, error) {
	client := &http.Client{Timeout: defaultProbeHTTPTimeout}
	return detectOpenAIAPITypeWithClient(baseURL, apiKey, model, client)
}

func detectOpenAIAPITypeWithClient(baseURL, apiKey, model string, client *http.Client) (string, error) {
	return DetectOpenAIAPIType(
		context.Background(),
		baseURL,
		apiKey,
		"",
		model,
		client,
		defaultProbeTotalTimeout,
		defaultProbeEndpointTimeout,
	)
}

func DetectOpenAIAPIType(ctx context.Context, baseURL, apiKey, org, model string, client *http.Client, totalTimeout, perEndpointTimeout time.Duration) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("empty base url")
	}
	model = strings.TrimSpace(model)
	if client == nil {
		client = &http.Client{Timeout: defaultProbeHTTPTimeout}
	}
	if model == "" {
		model = defaultProbeModel
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if totalTimeout <= 0 {
		totalTimeout = defaultProbeTotalTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()
	if perEndpointTimeout <= 0 {
		perEndpointTimeout = defaultProbeEndpointTimeout
	}

	resStatus, _, resErr := ProbeOpenAIEndpoint(ctx, client, base, apiKey, org, model, config.OpenAIAPITypeResponses, perEndpointTimeout)
	if resErr == nil && resStatus >= 200 && resStatus < 300 {
		return config.OpenAIAPITypeResponses, nil
	}

	chatStatus, _, chatErr := ProbeOpenAIEndpoint(ctx, client, base, apiKey, org, model, config.OpenAIAPITypeChatCompletions, perEndpointTimeout)
	if chatErr == nil && chatStatus >= 200 && chatStatus < 300 {
		return config.OpenAIAPITypeChatCompletions, nil
	}

	geminiStatus, _, geminiErr := ProbeOpenAIEndpoint(ctx, client, base, apiKey, org, model, config.OpenAIAPITypeGeminiGenerateContent, perEndpointTimeout)
	if geminiErr == nil && geminiStatus >= 200 && geminiStatus < 300 {
		return config.OpenAIAPITypeGeminiGenerateContent, nil
	}

	if resErr != nil {
		return "", resErr
	}
	if chatErr != nil {
		return "", chatErr
	}
	if geminiErr != nil {
		return "", geminiErr
	}
	return "", fmt.Errorf("probe failed (responses=%d chat_completions=%d gemini_generate_content=%d)", resStatus, chatStatus, geminiStatus)
}

func ProbeOpenAIEndpoint(ctx context.Context, client *http.Client, baseURL, apiKey, org, model, endpointType string, timeout time.Duration) (int, []byte, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return 0, nil, fmt.Errorf("empty base url")
	}
	path, payload, err := openAIProbePayload(strings.TrimSpace(model), endpointType)
	if err != nil {
		return 0, nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return postProbeJSON(ctx, client, base+path, apiKey, org, endpointType, payload)
}

func openAIProbePayload(model, endpointType string) (string, map[string]any, error) {
	if model == "" {
		model = defaultProbeModel
	}
	switch endpointType {
	case config.OpenAIAPITypeResponses:
		return "/responses", map[string]any{
			"model":             model,
			"input":             "ping",
			"max_output_tokens": 1024,
			"stream":            false,
		}, nil
	case config.OpenAIAPITypeChatCompletions:
		return "/chat/completions", map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": "ping"},
			},
			"max_tokens": 1024,
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
				"maxOutputTokens": 1024,
			},
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported endpoint type: %s", endpointType)
	}
}

func postProbeJSON(ctx context.Context, client *http.Client, url, apiKey, org, endpointType string, payload map[string]any) (int, []byte, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultProbeHTTPTimeout}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		if endpointType == config.OpenAIAPITypeGeminiGenerateContent {
			req.Header.Set("x-goog-api-key", strings.TrimSpace(apiKey))
		} else {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
		}
	}
	if endpointType != config.OpenAIAPITypeGeminiGenerateContent && strings.TrimSpace(org) != "" {
		req.Header.Set("OpenAI-Organization", strings.TrimSpace(org))
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxProbeErrorBodyBytes))
	return resp.StatusCode, data, nil
}
