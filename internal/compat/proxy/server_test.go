package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spark/internal/compat/ir"
	"spark/internal/usage"
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
		}, "\n")))
	}))
	defer upstream.Close()

	p, err := StartResponsesProxy(upstream.URL+"/v1", "", true, ResponsesProxyModeChatCompletionsOnly)
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
	if !strings.Contains(logs, "upstream POST") || !strings.Contains(logs, "stream parse summary") {
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

func TestCompatProxyClose_IsIdempotentAndRestoresUsageRecorder(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", filepath.Join(t.TempDir(), "codex-compat.log"))

	var recorded []usage.Record
	usage.SetRecorder(func(record usage.Record) error {
		recorded = append(recorded, record)
		return nil
	})
	defer usage.SetRecorder(nil)

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

	usage.RecordIR(ir.Usage{InputTokens: 1, OutputTokens: 2}, "model-after-close", false, time.Now())
	if len(recorded) != 1 {
		t.Fatalf("recorded count = %d, want 1", len(recorded))
	}
	if recorded[0].Client != "" {
		t.Fatalf("restored recorder saw client = %q, want empty", recorded[0].Client)
	}

	p.logf("late log after close")
}

func TestInstallUsageRecorderAddsClientAndFallbackModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	restore := installUsageRecorder("codex", "gpt-fallback", nil)
	usage.RecordIR(ir.Usage{InputTokens: 3, OutputTokens: 4}, "", true, time.Now())
	restore()
	defer usage.SetRecorder(nil)

	path, err := usage.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	records, err := usage.Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].Client != "codex" || records[0].Model != "gpt-fallback" {
		t.Fatalf("record identity = client %q model %q, want codex gpt-fallback", records[0].Client, records[0].Model)
	}
}

func TestPostUpstreamJSON_SetsPathHeadersAndBody(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", filepath.Join(t.TempDir(), "codex-compat.log"))

	var gotPath string
	var gotAuth string
	var gotAcceptEncoding string
	var gotPayload map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p, err := StartResponsesProxy(upstream.URL+"/v1", "secret", true, ResponsesProxyModeChatCompletionsOnly)
	if err != nil {
		t.Fatalf("StartResponsesProxy() error = %v", err)
	}
	defer p.Close()

	resp, err := p.postChatCompletions(context.Background(), map[string]any{"model": "m"})
	if err != nil {
		t.Fatalf("postChatCompletions() error = %v", err)
	}
	defer resp.Body.Close()

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization = %q, want bearer token", gotAuth)
	}
	if gotAcceptEncoding != "identity" {
		t.Fatalf("accept-encoding = %q, want identity", gotAcceptEncoding)
	}
	if gotPayload["model"] != "m" {
		t.Fatalf("payload model = %v, want m", gotPayload["model"])
	}
}

func TestPostAnthropicMessagesJSONSetsAnthropicHeaders(t *testing.T) {
	t.Setenv("AGENT_LAUNCH_COMPAT_LOG", filepath.Join(t.TempDir(), "codex-compat.log"))

	var gotPath string
	var gotAPIKey string
	var gotVersion string
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p, err := StartResponsesProxy(upstream.URL+"/v1", "anthropic-secret", true, ResponsesProxyModeAnthropicMessagesOnly)
	if err != nil {
		t.Fatalf("StartResponsesProxy() error = %v", err)
	}
	defer p.Close()

	resp, err := p.postAnthropicMessages(context.Background(), map[string]any{"model": "claude"})
	if err != nil {
		t.Fatalf("postAnthropicMessages() error = %v", err)
	}
	defer resp.Body.Close()

	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
	if gotAPIKey != "anthropic-secret" {
		t.Fatalf("x-api-key = %q, want anthropic-secret", gotAPIKey)
	}
	if gotVersion == "" {
		t.Fatalf("missing anthropic-version header")
	}
	if gotAuth != "" {
		t.Fatalf("authorization = %q, want empty", gotAuth)
	}
}
