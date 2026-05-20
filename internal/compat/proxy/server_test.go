package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
