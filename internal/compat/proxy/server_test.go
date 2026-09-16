package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartResponsesProxy_RegistersResponsesRoute(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", filepath.Join(t.TempDir(), "codex-compat.log"))

	p, err := StartResponsesProxy("http://127.0.0.1:1/v1", "", true, ResponsesProxyModeChatCompletionsOnly)
	if err != nil {
		t.Fatalf("StartResponsesProxy() error = %v", err)
	}
	defer p.Close()

	resp, err := http.Get(p.BaseURL() + "/responses")
	if err != nil {
		t.Fatalf("GET responses route: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestResponsesProxyWritesSessionLogFile(t *testing.T) {
	dir := t.TempDir()
	baseLog := filepath.Join(dir, "codex-compat.log")
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", baseLog)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_session","model":"m","choices":[{"delta":{"content":"ok"}}]}`,
			`data: {"id":"chatcmpl_session","model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"))
	}))
	defer upstream.Close()

	p, err := StartResponsesProxy(upstream.URL+"/v1", "test-key", true, ResponsesProxyModeChatCompletionsOnly)
	if err != nil {
		t.Fatalf("StartResponsesProxy() error = %v", err)
	}
	defer p.Close()

	body := `{"model":"m","stream":true,"client_metadata":{"x-codex-window-id":"window:abc/123"},"input":"hi"}`
	resp, err := http.Post(p.BaseURL()+"/responses", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST responses: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	now := time.Now()
	sessionLog := filepath.Join(dir, now.Format("2006"), now.Format("01"), now.Format("02"), "codex-compat-window-abc-123.log")
	data, err := os.ReadFile(sessionLog)
	if err != nil {
		t.Fatalf("read session log %s: %v", sessionLog, err)
	}
	logs := string(data)
	if !strings.Contains(logs, "POST /v1/responses") && !strings.Contains(logs, "POST /responses") && !strings.Contains(logs, "window:abc/123") {
		t.Fatalf("session log missing request details:\n%s", logs)
	}
}

func TestStartAnthropicProxy_RegistersMessagesRoutes(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_ANTHROPIC_COMPAT_LOG", filepath.Join(t.TempDir(), "anthropic-compat.log"))

	p, err := StartAnthropicProxy("http://127.0.0.1:1/v1", "", "")
	if err != nil {
		t.Fatalf("StartAnthropicProxy() error = %v", err)
	}
	defer p.Close()

	for _, path := range []string{"/messages", "/v1/messages"} {
		resp, err := http.Get(p.BaseURL() + path)
		if err != nil {
			t.Fatalf("GET %s route: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want %d", path, resp.StatusCode, http.StatusMethodNotAllowed)
		}
	}
}

func TestCompatProxyClose_IsIdempotent(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", filepath.Join(t.TempDir(), "codex-compat.log"))

	p, err := StartResponsesProxy("http://127.0.0.1:1/v1", "", true, ResponsesProxyModeChatCompletionsOnly)
	if err != nil {
		t.Fatalf("StartResponsesProxy() error = %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	p.logf("late log after close")
}
