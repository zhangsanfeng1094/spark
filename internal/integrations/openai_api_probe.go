package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"spark/internal/config"
)

func detectOpenAIAPIType(baseURL, apiKey, model string) (string, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	return detectOpenAIAPITypeWithClient(baseURL, apiKey, model, client)
}

func detectOpenAIAPITypeWithClient(baseURL, apiKey, model string, client *http.Client) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("empty base url")
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}

	responsesReq := map[string]any{
		"model":             model,
		"input":             "ping",
		"max_output_tokens": 1,
		"stream":            false,
	}
	resStatus, _, resErr := postJSON(client, base+"/responses", apiKey, responsesReq)
	if resErr == nil && resStatus >= 200 && resStatus < 300 {
		return config.OpenAIAPITypeResponses, nil
	}

	chatReq := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
		"stream":     false,
	}
	chatStatus, _, chatErr := postJSON(client, base+"/chat/completions", apiKey, chatReq)
	if chatErr == nil && chatStatus >= 200 && chatStatus < 300 {
		return config.OpenAIAPITypeChatCompletions, nil
	}

	if resErr != nil {
		return "", resErr
	}
	if chatErr != nil {
		return "", chatErr
	}
	return "", fmt.Errorf("probe failed (responses=%d chat_completions=%d)", resStatus, chatStatus)
}

func postJSON(client *http.Client, url, apiKey string, payload map[string]any) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, nil
}
