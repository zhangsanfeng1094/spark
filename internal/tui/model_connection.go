package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"spark/internal/config"
)

type TestResult struct {
	Success bool
	Message string
	Latency time.Duration
}

func TestModelConnection(profile *config.Profile, model string) TestResult {
	if profile == nil {
		return TestResult{Success: false, Message: "Profile is nil"}
	}

	baseURL := strings.TrimSpace(profile.OpenAIBaseURL)
	if baseURL == "" {
		return TestResult{Success: false, Message: "Base URL is empty"}
	}

	// Normalize base URL
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	// Construct chat completions endpoint
	endpoint := strings.TrimSuffix(baseURL, "/") + "/chat/completions"

	apiKey := strings.TrimSpace(profile.OpenAIAPIKey)

	// Build minimal test request
	testModel := model
	if testModel == "" {
		testModel = "gpt-3.5-turbo"
		if len(profile.Models) > 0 {
			testModel = profile.Models[0]
		}
		if profile.DefaultModel != "" {
			testModel = profile.DefaultModel
		}
	}

	reqBody := map[string]interface{}{
		"model": testModel,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
		"stream":     false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return TestResult{Success: false, Message: "Failed to build request: " + err.Error()}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return TestResult{Success: false, Message: "Failed to create request: " + err.Error()}
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if profile.OpenAIOrg != "" {
		req.Header.Set("OpenAI-Organization", profile.OpenAIOrg)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		if urlErr, ok := err.(*url.Error); ok && urlErr.Timeout() {
			return TestResult{Success: false, Message: "Connection timeout (15s)", Latency: latency}
		}
		return TestResult{Success: false, Message: "Connection failed: " + err.Error(), Latency: latency}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return TestResult{
			Success: true,
			Message: fmt.Sprintf("OK (model: %s)", testModel),
			Latency: latency,
		}
	}

	// Try to extract error message from response
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
		msg := errResp.Error.Message
		if errResp.Error.Type != "" {
			msg = fmt.Sprintf("[%s] %s", errResp.Error.Type, msg)
		}
		return TestResult{Success: false, Message: msg, Latency: latency}
	}

	return TestResult{
		Success: false,
		Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))),
		Latency: latency,
	}
}
