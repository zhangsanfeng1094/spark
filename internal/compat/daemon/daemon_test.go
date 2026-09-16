package daemon

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spark/internal/auth"
	"spark/internal/config"
)

func TestWriteDaemonInfoTightensExistingPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "spark")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "daemon.json")
	if err := os.WriteFile(path, []byte(`{"pid":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeDaemonInfo(DaemonInfo{PID: 2, Addr: "127.0.0.1:1", ManagementToken: "secret-token"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("daemon.json mode = %o, want 0600", st.Mode().Perm())
	}
}

func TestDaemonServer_Health(t *testing.T) {
	ctx := context.Background()
	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	go func() {
		_ = server.Start()
	}()
	defer server.Shutdown(ctx)

	time.Sleep(50 * time.Millisecond)

	hr, err := CheckHealth(server.BaseURL())
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	if hr.Status != "ok" {
		t.Fatalf("expected status ok, got %s", hr.Status)
	}
	if hr.PID <= 0 {
		t.Fatalf("invalid PID: %d", hr.PID)
	}
	if !hr.Supports(ProtocolGemini) || !hr.Supports(ProtocolClaude) || !hr.Supports(ProtocolCodex) {
		t.Fatalf("protocols=%v", hr.Protocols)
	}
	if hr.ExePath == "" || hr.ExeMTime == 0 || hr.ExeSize == 0 {
		t.Fatalf("missing binary identity: path=%q mtime=%d size=%d", hr.ExePath, hr.ExeMTime, hr.ExeSize)
	}
	if !hr.MatchesCurrent(CurrentIdentity()) {
		t.Fatalf("health identity should match current test binary")
	}
}

func TestHealthResponse_SupportsLegacyDaemon(t *testing.T) {
	t.Parallel()
	legacy := &HealthResponse{Status: "ok"}
	if !legacy.Supports(ProtocolCodex) || !legacy.Supports(ProtocolClaude) {
		t.Fatal("legacy daemon should support codex and claude")
	}
	if legacy.Supports(ProtocolGemini) {
		t.Fatal("legacy daemon should not advertise gemini")
	}
}

func TestResolveProfileFromRequest_ThinkingToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.Save(&config.RootConfig{
		Version: 1, DefaultProfile: "work",
		Profiles: map[string]*config.Profile{"work": {Thinking: &config.ThinkingConfig{Mode: "auto", Effort: "low"}}},
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer spark-profile:work?think=8k&sid=abc")
	got := ResolveProfileFromRequest(req, "gpt-5")
	if got == nil || got.Thinking == nil || got.Thinking.BudgetTokens == nil || *got.Thinking.BudgetTokens != 8192 {
		t.Fatalf("thinking override not resolved: %#v", got)
	}
}

func TestDaemonServer_CodexIngress(t *testing.T) {
	ctx := context.Background()

	// Mock upstream OpenAI chat completion server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl-daemon-1","choices":[{"index":0,"delta":{"role":"assistant","content":"Daemon Codex response"}}]}`,
			`data: {"id":"chatcmpl-daemon-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"))
	}))
	defer upstream.Close()

	// Configure a test profile in memory / environment
	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "test-profile",
		Profiles: map[string]*config.Profile{
			"test-profile": {
				OpenAIBaseURL: upstream.URL + "/v1",
				APIKey:        "test-key",
			},
		},
	}
	_ = config.Save(cfg)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	go func() {
		_ = server.Start()
	}()
	defer server.Shutdown(ctx)

	time.Sleep(50 * time.Millisecond)

	reqBody := `{
		"model": "gpt-4",
		"input": "Hello",
		"stream": true
	}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.BaseURL()+"/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer spark-profile:test-profile")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request to daemon failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(bodyBytes)

	if !strings.Contains(bodyStr, "Daemon Codex response") {
		t.Fatalf("expected text content in output, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "response.output_text.delta") {
		t.Fatalf("expected output_text.delta event, got:\n%s", bodyStr)
	}
}

func TestDaemonServer_ClaudeIngress(t *testing.T) {
	ctx := context.Background()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl-claude-daemon","choices":[{"index":0,"delta":{"role":"assistant","content":"Daemon Claude response"}}]}`,
			`data: {"id":"chatcmpl-claude-daemon","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"))
	}))
	defer upstream.Close()

	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "claude-profile",
		Profiles: map[string]*config.Profile{
			"claude-profile": {
				OpenAIBaseURL: upstream.URL + "/v1",
				APIKey:        "test-key",
			},
		},
	}
	_ = config.Save(cfg)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	go func() {
		_ = server.Start()
	}()
	defer server.Shutdown(ctx)

	time.Sleep(50 * time.Millisecond)

	reqBody := `{
		"model": "claude-3-5-sonnet",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello"}
		],
		"stream": true
	}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.BaseURL()+"/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "spark-profile:claude-profile")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request to daemon failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(bodyBytes)

	if !strings.Contains(bodyStr, "message_start") {
		t.Fatalf("expected message_start event, got:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Daemon Claude response") {
		t.Fatalf("expected text content in output, got:\n%s", bodyStr)
	}
}

func TestDaemonServer_GeminiIngress(t *testing.T) {
	ctx := context.Background()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl-gemini-daemon","choices":[{"index":0,"delta":{"role":"assistant","content":"Daemon Gemini response"}}]}`,
			`data: {"id":"chatcmpl-gemini-daemon","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"))
	}))
	defer upstream.Close()

	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "gemini-profile",
		Profiles: map[string]*config.Profile{
			"gemini-profile": {
				OpenAIBaseURL: upstream.URL + "/v1",
				APIKey:        "test-key",
			},
		},
	}
	_ = config.Save(cfg)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	go func() {
		_ = server.Start()
	}()
	defer server.Shutdown(ctx)

	time.Sleep(50 * time.Millisecond)

	reqBody := `{"contents":[{"role":"user","parts":[{"text":"Hello"}]}]}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.BaseURL()+"/v1beta/models/gemini-3.7-flash:streamGenerateContent?alt=sse", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", "spark-profile:gemini-profile")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request to daemon failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(bodyBytes)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, bodyStr)
	}
	if !strings.Contains(bodyStr, "Daemon Gemini response") {
		t.Fatalf("expected text content in output, got:\n%s", bodyStr)
	}
}

func TestDaemonServer_GeminiCatchAllPath(t *testing.T) {
	ctx := context.Background()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "p",
		Profiles: map[string]*config.Profile{
			"p": {OpenAIBaseURL: upstream.URL + "/v1", APIKey: "k"},
		},
	}
	_ = config.Save(cfg)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go func() { _ = server.Start() }()
	defer server.Shutdown(ctx)
	time.Sleep(50 * time.Millisecond)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.BaseURL()+"/custom/models/gemini-pro:generateContent", strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestDaemonServer_GeminiQueryAPIKey(t *testing.T) {
	ctx := context.Background()

	matched := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"from-query-key"},"finish_reason":"stop"}]}`))
	}))
	defer matched.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-2","choices":[{"index":0,"message":{"role":"assistant","content":"from-default"},"finish_reason":"stop"}]}`))
	}))
	defer fallback.Close()

	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "fallback",
		Profiles: map[string]*config.Profile{
			"fallback": {OpenAIBaseURL: fallback.URL + "/v1", APIKey: "other-key"},
			"matched":  {OpenAIBaseURL: matched.URL + "/v1", APIKey: "secret-key"},
		},
	}
	_ = config.Save(cfg)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go func() { _ = server.Start() }()
	defer server.Shutdown(ctx)
	time.Sleep(50 * time.Millisecond)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.BaseURL()+"/v1beta/models/gemini-pro:generateContent?key=secret-key", strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "from-query-key") {
		t.Fatalf("expected query-key profile, got %s", body)
	}
}

func TestDaemonServer_GeminiResponsesOnlySkipsChatStream(t *testing.T) {
	ctx := context.Background()

	var streamCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"stream":true`) {
			streamCalls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"auth_unavailable: no auth available (providers=antigravity, model=gemini-3.7-flash-high)"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"non-stream ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "cpa",
		Profiles: map[string]*config.Profile{
			"cpa": {
				OpenAIBaseURL: upstream.URL + "/v1",
				APIKey:        "cpa-key",
				OpenAIAPIType: config.OpenAIAPITypeResponses,
				DefaultModel:  "gemini-3.7-flash-high",
			},
		},
	}
	_ = config.Save(cfg)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go func() { _ = server.Start() }()
	defer server.Shutdown(ctx)
	time.Sleep(50 * time.Millisecond)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.BaseURL()+"/v1beta/models/gemini-3.7-flash:streamGenerateContent?alt=sse", strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"你好"}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", "cpa-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if streamCalls != 0 {
		t.Fatalf("responses-only profile should not hit chat stream, streamCalls=%d", streamCalls)
	}
	if !strings.Contains(string(body), "non-stream ok") {
		t.Fatalf("expected non-stream sse body, got %s", body)
	}
}

func TestDaemonServer_ManagementAPI(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go func() { _ = server.Start() }()
	defer server.Shutdown(ctx)
	time.Sleep(50 * time.Millisecond)

	token := server.ManagementToken()

	// 1. Providers endpoint
	resp := mustManagement(t, server, http.MethodGet, "/v0/management/providers", token)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "claude") || !strings.Contains(string(body), "commandcode") {
		t.Fatalf("unexpected providers response: %s", body)
	}

	// 2. Auth URL endpoint
	resp = mustManagement(t, server, http.MethodGet, "/v0/management/claude-auth-url", token)
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "auth_url") {
		t.Fatalf("unexpected auth-url response: %s", body)
	}

	// 2b. Command Code Auth URL endpoint
	resp = mustManagement(t, server, http.MethodGet, "/v0/management/commandcode-auth-url", token)
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "commandcode.ai/studio/auth/cli") {
		t.Fatalf("unexpected commandcode-auth-url response: %s", body)
	}

	// 3. Accounts endpoint
	resp = mustManagement(t, server, http.MethodGet, "/v0/management/accounts", token)
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "accounts") {
		t.Fatalf("unexpected accounts response: %s", body)
	}

	// 4. Logout endpoint
	resp = mustManagement(t, server, http.MethodPost, "/v0/management/claude-logout", token)
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ok") {
		t.Fatalf("unexpected logout response: %s", body)
	}
}

func TestDaemonServer_ManagementLogoutSecurity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("HOME", root)

	unrelated := filepath.Join(root, ".spark", "unrelated")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(unrelated, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := auth.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&auth.Auth{Provider: auth.ProviderClaude, AccessToken: "tok", Account: "work"}); err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go func() { _ = server.Start() }()
	defer server.Shutdown(ctx)
	time.Sleep(50 * time.Millisecond)
	token := server.ManagementToken()

	unauth := mustManagement(t, server, http.MethodPost, "/v0/management/claude-logout", "")
	defer unauth.Body.Close()
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated logout status = %d", unauth.StatusCode)
	}

	evil, err := http.NewRequest(http.MethodPost, server.BaseURL()+"/v0/management/claude-logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	evil.Header.Set(managementTokenHeader, token)
	evil.Header.Set("Origin", "https://evil.example")
	evilResp, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatal(err)
	}
	evilResp.Body.Close()
	if evilResp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin logout status = %d, want 403", evilResp.StatusCode)
	}

	getLogout := mustManagement(t, server, http.MethodGet, "/v0/management/logout?provider=claude", token)
	defer getLogout.Body.Close()
	if getLogout.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET logout status = %d, want 405", getLogout.StatusCode)
	}

	for _, path := range []string{
		"/v0/management/logout?provider=../unrelated",
		"/v0/management/logout?provider=foo/bar",
		"/v0/management/logout?provider=not-a-provider",
	} {
		resp := mustManagement(t, server, http.MethodPost, path, token)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status = %d body=%s, want 400", path, resp.StatusCode, body)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated data was deleted: %v", err)
	}
	got, err := store.Get(auth.ProviderClaude)
	if err != nil || got == nil {
		t.Fatalf("valid auth should remain after rejected logout: %+v err=%v", got, err)
	}

	alias := mustManagement(t, server, http.MethodPost, "/v0/management/anthropic-logout", token)
	defer alias.Body.Close()
	body, _ := io.ReadAll(alias.Body)
	if alias.StatusCode != http.StatusOK || !strings.Contains(string(body), "claude") {
		t.Fatalf("alias logout: status=%d body=%s", alias.StatusCode, body)
	}
	got, err = store.Get(auth.ProviderClaude)
	if err != nil || got != nil {
		t.Fatalf("expected claude auth deleted, got %+v err=%v", got, err)
	}
}

func mustManagement(t *testing.T, server *Server, method, path, token string) *http.Response {
	t.Helper()
	resp, err := ManagementRequest(method, server.BaseURL(), path, token, nil)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestDaemonServer_OpenAIIngress(t *testing.T) {
	ctx := context.Background()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-openai-daemon",
			"object": "chat.completion",
			"created": 1677610602,
			"model": "gpt-4o",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Daemon OpenAI response"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15
			}
		}`))
	}))
	defer upstream.Close()

	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "openai-profile",
		Profiles: map[string]*config.Profile{
			"openai-profile": {
				OpenAIBaseURL: upstream.URL + "/v1",
				APIKey:        "test-key",
				DefaultModel:  "gpt-4o",
			},
		},
	}
	_ = config.Save(cfg)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go func() { _ = server.Start() }()
	defer server.Shutdown(ctx)
	time.Sleep(50 * time.Millisecond)

	reqBody := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Hi"}],
		"stream": false
	}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.BaseURL()+"/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer spark-profile:openai-profile")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Daemon OpenAI response") {
		t.Fatalf("expected response content, got: %s", body)
	}

	// Also test /v1/models
	reqModels, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.BaseURL()+"/v1/models", nil)
	respModels, err := http.DefaultClient.Do(reqModels)
	if err != nil {
		t.Fatalf("request /v1/models failed: %v", err)
	}
	defer respModels.Body.Close()
	bodyModels, _ := io.ReadAll(respModels.Body)
	if !strings.Contains(string(bodyModels), "gpt-4o") {
		t.Fatalf("expected model gpt-4o in models list, got: %s", bodyModels)
	}
}

func TestDaemonServer_CPASubscriptionConversion(t *testing.T) {
	ctx := context.Background()

	// Upstream Anthropic server returning native Anthropic wire response
	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/messages") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_cpa_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-5-sonnet-20241022",
			"content": [
				{
					"type": "text",
					"text": "Hello from CPA converted Anthropic subscription"
				}
			],
			"stop_reason": "end_turn",
			"usage": {
				"input_tokens": 12,
				"output_tokens": 8
			}
		}`))
	}))
	defer anthropicUpstream.Close()

	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "claude-sub",
		Profiles: map[string]*config.Profile{
			"claude-sub": {
				AnthropicBaseURL: anthropicUpstream.URL + "/v1",
				OpenAIAPIType:    config.OpenAIAPITypeAnthropicMessages,
				APIKey:           "sk-ant-test",
				DefaultModel:     "claude-3-5-sonnet-20241022",
			},
		},
	}
	_ = config.Save(cfg)

	server, err := NewServer(ctx, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go func() { _ = server.Start() }()
	defer server.Shutdown(ctx)
	time.Sleep(50 * time.Millisecond)

	// Client sends standard OpenAI chat completions request
	reqBody := `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [{"role": "user", "content": "Hello Claude!"}],
		"stream": false
	}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.BaseURL()+"/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer spark-profile:claude-sub")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body: %s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Client receives standard OpenAI format with converted content
	if !strings.Contains(bodyStr, "Hello from CPA converted Anthropic subscription") {
		t.Fatalf("expected CPA converted response text, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "chat.completion") {
		t.Fatalf("expected object=chat.completion in response, got: %s", bodyStr)
	}
}
