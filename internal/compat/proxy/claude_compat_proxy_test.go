package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeCompatProxy_StreamEndToEnd(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_ANTHROPIC_COMPAT_LOG", filepath.Join(t.TempDir(), "anthropic-compat.log"))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl-claude-1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello Claude!"}}]}`,
			`data: {"id":"chatcmpl-claude-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"))
	}))
	defer upstream.Close()

	p, err := StartAnthropicProxy(upstream.URL+"/v1", "test-key", "claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("StartAnthropicProxy() error = %v", err)
	}
	defer p.Close()

	reqBody := `{
		"model": "claude-3-5-sonnet",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello"}
		],
		"stream": true
	}`
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL()+"/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST to proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	bodyStr := string(bodyBytes)

	if !strings.Contains(bodyStr, "message_start") {
		t.Fatalf("expected message_start event, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Hello Claude!") {
		t.Fatalf("expected text content in output, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "message_stop") {
		t.Fatalf("expected message_stop event, got:\n%s", bodyStr)
	}
}

func TestClaudeCompatProxy_NonStreamEndToEnd(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_ANTHROPIC_COMPAT_LOG", filepath.Join(t.TempDir(), "anthropic-compat.log"))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id": "chatcmpl-claude-2",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Claude non-stream answer",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     15,
				"completion_tokens": 8,
				"total_tokens":      23,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	p, err := StartAnthropicProxy(upstream.URL+"/v1", "test-key", "claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("StartAnthropicProxy() error = %v", err)
	}
	defer p.Close()

	reqBody := `{
		"model": "claude-3-5-sonnet",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello"}
		],
		"stream": false
	}`
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL()+"/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST to proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var anthropicResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}

	if anthropicResp["type"] != "message" {
		t.Fatalf("expected type message, got %v", anthropicResp["type"])
	}
	if anthropicResp["role"] != "assistant" {
		t.Fatalf("expected role assistant, got %v", anthropicResp["role"])
	}
	content, ok := anthropicResp["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content blocks, got %#v", anthropicResp["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok || block["text"] != "Claude non-stream answer" {
		t.Fatalf("expected block text 'Claude non-stream answer', got %#v", block)
	}
}
