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

func TestGeminiCompatProxy_StreamEndToEnd(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_GEMINI_COMPAT_LOG", filepath.Join(t.TempDir(), "gemini-compat.log"))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl-gemini-1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello Gemini!"}}]}`,
			`data: {"id":"chatcmpl-gemini-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"))
	}))
	defer upstream.Close()

	p, err := StartGeminiProxy(upstream.URL+"/v1", "test-key", "gemini-3.7-flash")
	if err != nil {
		t.Fatalf("StartGeminiProxy() error = %v", err)
	}
	defer p.Close()

	reqBody := `{
		"contents": [{"role":"user","parts":[{"text":"Hello"}]}],
		"generationConfig": {"maxOutputTokens": 64}
	}`
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL()+"/v1beta/models/gemini-3.7-flash:streamGenerateContent?alt=sse", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", "test-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST to proxy failed: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	bodyStr := string(bodyBytes)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, bodyStr)
	}
	if !strings.Contains(bodyStr, "Hello Gemini!") {
		t.Fatalf("expected text content in output, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"finishReason":"STOP"`) {
		t.Fatalf("expected finishReason STOP, got:\n%s", bodyStr)
	}
}

func TestGeminiCompatProxy_StreamFallsBackToNonStream(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_GEMINI_COMPAT_LOG", filepath.Join(t.TempDir(), "gemini-compat.log"))

	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"auth_unavailable: no auth available (providers=antigravity, model=gemini-3.7-flash-high)"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-gemini-fallback",
			"choices": []map[string]any{
				{
					"index":         0,
					"message":       map[string]any{"role": "assistant", "content": "fallback from non-stream"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 4, "total_tokens": 6},
		})
	}))
	defer upstream.Close()

	p, err := StartGeminiProxy(upstream.URL+"/v1", "test-key", "gemini-3.7-flash-high")
	if err != nil {
		t.Fatalf("StartGeminiProxy() error = %v", err)
	}
	defer p.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL()+"/v1beta/models/gemini-3.7-flash:streamGenerateContent?alt=sse", strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"你好"}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", "test-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST to proxy failed: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	bodyStr := string(bodyBytes)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, bodyStr)
	}
	if calls < 2 {
		t.Fatalf("expected stream then non-stream upstream calls, got %d", calls)
	}
	if !strings.Contains(bodyStr, "fallback from non-stream") {
		t.Fatalf("expected fallback text in sse, got:\n%s", bodyStr)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type=%q", resp.Header.Get("Content-Type"))
	}
}

func TestGeminiCompatProxy_StripsToolsAfterAuthUnavailable(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_GEMINI_COMPAT_LOG", filepath.Join(t.TempDir(), "gemini-compat.log"))

	var sawTools, sawPlain bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), `"tools"`) {
			sawTools = true
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"auth_unavailable: no auth available (providers=antigravity, model=gemini-3.7-flash-high)"}}`))
			return
		}
		sawPlain = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-plain",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "hello without tools"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer upstream.Close()

	p, err := StartGeminiProxy(upstream.URL+"/v1", "test-key", "gemini-3.7-flash-high")
	if err != nil {
		t.Fatalf("StartGeminiProxy() error = %v", err)
	}
	defer p.Close()

	reqBody := `{
		"contents": [{"role":"user","parts":[{"text":"你好"}]}],
		"tools": [{"functionDeclarations":[{"name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]}]
	}`
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL()+"/v1beta/models/gemini-3.7-flash:streamGenerateContent?alt=sse", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if !sawTools || !sawPlain {
		t.Fatalf("sawTools=%t sawPlain=%t", sawTools, sawPlain)
	}
	if !strings.Contains(string(body), "hello without tools") {
		t.Fatalf("body=%s", body)
	}
}

func TestGeminiCompatProxy_NonStreamEndToEnd(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_GEMINI_COMPAT_LOG", filepath.Join(t.TempDir(), "gemini-compat.log"))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-gemini-2",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Gemini non-stream answer",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     9,
				"completion_tokens": 6,
				"total_tokens":      15,
			},
		})
	}))
	defer upstream.Close()

	p, err := StartGeminiProxy(upstream.URL+"/v1", "test-key", "gemini-3.7-flash")
	if err != nil {
		t.Fatalf("StartGeminiProxy() error = %v", err)
	}
	defer p.Close()

	reqBody := `{
		"contents": [{"role":"user","parts":[{"text":"Hello"}]}]
	}`
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL()+"/v1beta/models/gemini-3.7-flash:generateContent", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST to proxy failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, body)
	}

	var geminiResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}
	candidates, _ := geminiResp["candidates"].([]any)
	if len(candidates) == 0 {
		t.Fatalf("expected candidates, got %#v", geminiResp)
	}
	candidate, _ := candidates[0].(map[string]any)
	content, _ := candidate["content"].(map[string]any)
	parts, _ := content["parts"].([]any)
	if len(parts) == 0 {
		t.Fatalf("expected parts, got %#v", candidate)
	}
	part, _ := parts[0].(map[string]any)
	if part["text"] != "Gemini non-stream answer" {
		t.Fatalf("text=%v", part["text"])
	}
}
