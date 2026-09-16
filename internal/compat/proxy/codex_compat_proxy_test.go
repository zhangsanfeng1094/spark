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

func TestResponsesProxy_BaseURL(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", filepath.Join(t.TempDir(), "codex-compat.log"))

	p, err := StartResponsesProxy("http://127.0.0.1:1/v1", "key", true, ResponsesProxyModeChatCompletionsOnly)
	if err != nil {
		t.Fatalf("StartResponsesProxy() error = %v", err)
	}
	defer p.Close()

	if !strings.HasSuffix(p.BaseURL(), "/v1") {
		t.Fatalf("BaseURL() = %s, want suffix /v1", p.BaseURL())
	}
}

func TestCodexCompatProxy_StreamEndToEnd(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", filepath.Join(t.TempDir(), "codex-compat.log"))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello world!"}}]}`,
			`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"))
	}))
	defer upstream.Close()

	p, err := StartResponsesProxy(upstream.URL+"/v1", "test-key", true, ResponsesProxyModeChatCompletionsOnly)
	if err != nil {
		t.Fatalf("StartResponsesProxy() error = %v", err)
	}
	defer p.Close()

	reqBody := `{
		"model": "gpt-4",
		"input": "Hello",
		"stream": true
	}`
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL()+"/responses", strings.NewReader(reqBody))
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

	if !strings.Contains(bodyStr, "response.output_text.delta") {
		t.Fatalf("expected response.output_text.delta event, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Hello world!") {
		t.Fatalf("expected text content in output, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "response.completed") {
		t.Fatalf("expected response.completed event, got:\n%s", bodyStr)
	}
}

func TestCodexCompatProxy_NonStreamEndToEnd(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", filepath.Join(t.TempDir(), "codex-compat.log"))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id": "chatcmpl-2",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Non-stream response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     8,
				"completion_tokens": 4,
				"total_tokens":      12,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	p, err := StartResponsesProxy(upstream.URL+"/v1", "test-key", true, ResponsesProxyModeChatCompletionsOnly)
	if err != nil {
		t.Fatalf("StartResponsesProxy() error = %v", err)
	}
	defer p.Close()

	reqBody := `{
		"model": "gpt-4",
		"input": "Hello",
		"stream": false
	}`
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL()+"/responses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST to proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var codexResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&codexResp); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}

	if codexResp["status"] != "completed" {
		t.Fatalf("expected status completed, got %v", codexResp["status"])
	}
	output, ok := codexResp["output"].([]any)
	if !ok || len(output) == 0 {
		t.Fatalf("expected output items, got %#v", codexResp["output"])
	}
}
