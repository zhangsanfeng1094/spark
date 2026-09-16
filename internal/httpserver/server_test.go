package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spark/internal/auth"
	"spark/internal/compat/engine"
	"spark/internal/config"
)

func TestProfilesDoNotExposeAPIKeyAndPreserveOnEmptyUpdate(t *testing.T) {
	h := newTestHandler(t)
	postJSON(t, h, http.MethodPost, "/api/profiles", `{
		"name":"work",
		"openai_base_url":"https://example.com/v1",
		"api_key":"secret-key",
		"openai_api_type":"responses",
		"models":["gpt-5"],
		"default_model":"gpt-5"
	}`, http.StatusOK)

	body := request(t, h, http.MethodGet, "/api/profiles", "", http.StatusOK)
	if strings.Contains(body, "secret-key") || strings.Contains(body, `"api_key"`) {
		t.Fatalf("profile response exposed api key: %s", body)
	}
	if !strings.Contains(body, `"has_api_key":true`) {
		t.Fatalf("profile response did not include has_api_key: %s", body)
	}

	postJSON(t, h, http.MethodPut, "/api/profiles/work", `{
		"name":"work-renamed",
		"openai_base_url":"https://example.com/v2",
		"openai_api_type":"responses",
		"models":["gpt-5"],
		"default_model":"gpt-5"
	}`, http.StatusOK)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles["work-renamed"].EffectiveAPIKey() != "secret-key" {
		t.Fatalf("expected API key to be preserved, got %q", cfg.Profiles["work-renamed"].EffectiveAPIKey())
	}

	postJSON(t, h, http.MethodPut, "/api/profiles/work-renamed", `{
		"name":"work-renamed",
		"openai_base_url":"https://example.com/v2",
		"clear_api_key":true,
		"openai_api_type":"responses",
		"models":["gpt-5"],
		"default_model":"gpt-5"
	}`, http.StatusOK)
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles["work-renamed"].EffectiveAPIKey() != "" {
		t.Fatalf("expected API key to be cleared")
	}
}

func TestPromptPresetCRUDWritesContentAndDeleteConflict(t *testing.T) {
	h := newTestHandler(t)
	postJSON(t, h, http.MethodPost, "/api/prompts/presets", `{
		"name":"review",
		"description":"Review prompt",
		"mode":"append",
		"content":"check changes"
	}`, http.StatusOK)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	preset := cfg.Prompts.Presets["review"]
	if preset == nil || preset.File != "prompts/review.md" {
		t.Fatalf("unexpected preset: %#v", preset)
	}
	path, err := config.ResolvePromptPath(preset.File)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "check changes" {
		t.Fatalf("prompt content = %q", string(data))
	}

	postJSON(t, h, http.MethodPost, "/api/prompts/bindings", `{
		"integration":"codex",
		"model":"*",
		"preset":"review",
		"enabled":true
	}`, http.StatusOK)
	request(t, h, http.MethodDelete, "/api/prompts/presets/review", "", http.StatusConflict)
}

func TestPromptPresetRejectsEscapingPath(t *testing.T) {
	h := newTestHandler(t)
	postJSON(t, h, http.MethodPost, "/api/prompts/presets", `{
		"name":"bad",
		"file":"../bad.md",
		"mode":"append",
		"content":"x"
	}`, http.StatusBadRequest)
}

func TestProfileDefaultAndDelete(t *testing.T) {
	h := newTestHandler(t)
	postJSON(t, h, http.MethodPost, "/api/profiles", `{
		"name":"backup",
		"openai_base_url":"https://example.com/v1",
		"openai_api_type":"responses",
		"models":["gpt-5"],
		"default_model":"gpt-5"
	}`, http.StatusOK)
	postJSON(t, h, http.MethodPut, "/api/profiles/default", `{"name":"backup"}`, http.StatusOK)
	request(t, h, http.MethodDelete, "/api/profiles/backup", "", http.StatusOK)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultProfile != "default" {
		t.Fatalf("default profile = %q", cfg.DefaultProfile)
	}
	request(t, h, http.MethodDelete, "/api/profiles/default", "", http.StatusConflict)
}

func TestProfileFromDTO_SwitchProviderUpdatesAuthRef(t *testing.T) {
	existing := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		AuthProvider:  "codex",
		AuthRef:       "codex:default",
		Credential: config.CredentialConfig{
			Mode:    config.CredentialModeAuth,
			AuthRef: "codex:default",
			APIKey:  "secret-key",
		},
		APIKey: "secret-key",
	}
	got, err := profileFromDTO(existing, profileDTO{
		OpenAIBaseURL: "https://api.anthropic.com",
		AuthProvider:  "claude",
		OpenAIAPIType: "anthropic_messages",
		Models:        []string{"claude-sonnet-4-20250514"},
		DefaultModel:  "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthProvider != "claude" {
		t.Fatalf("AuthProvider = %q", got.AuthProvider)
	}
	if got.EffectiveAuthRef() != "claude:default" {
		t.Fatalf("EffectiveAuthRef = %q, want claude:default", got.EffectiveAuthRef())
	}
	if got.Credential.AuthRef != "claude:default" || got.AuthRef != "claude:default" {
		t.Fatalf("refs not synced: cred=%q auth=%q", got.Credential.AuthRef, got.AuthRef)
	}
	if got.EffectiveAPIKey() != "secret-key" {
		t.Fatalf("API key not preserved: %q", got.EffectiveAPIKey())
	}
	if got.Credential.Mode != config.CredentialModeAuth {
		t.Fatalf("mode = %q, want auth so stored API key is not auto-activated", got.Credential.Mode)
	}
	emptyStore := auth.NewStore(filepath.Join(t.TempDir(), "auth"))
	cred := engine.ResolveRequestCredentialWithStore(context.Background(), got, nil, emptyStore)
	if cred.Value != "" || cred.Source == "profile.api_key" {
		t.Fatalf("old API key was used for new provider: %+v", cred)
	}

	existing.AuthProvider = "claude"
	existing.AuthRef = "claude:work"
	existing.Credential.AuthRef = "claude:work"
	existing.Credential.Mode = config.CredentialModeAuth
	kept, err := profileFromDTO(existing, profileDTO{
		OpenAIBaseURL: "https://api.anthropic.com/v2",
		OpenAIAPIType: "anthropic_messages",
		Models:        []string{"claude-sonnet-4-20250514"},
		DefaultModel:  "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	if kept.EffectiveAuthRef() != "claude:work" {
		t.Fatalf("custom account reset: %q", kept.EffectiveAuthRef())
	}

	cleared, err := profileFromDTO(existing, profileDTO{
		OpenAIBaseURL: "https://api.anthropic.com/v2",
		OpenAIAPIType: "anthropic_messages",
		ClearAPIKey:   true,
		Models:        []string{"claude-sonnet-4-20250514"},
		DefaultModel:  "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.EffectiveAPIKey() != "" {
		t.Fatal("expected API key cleared")
	}
	if cleared.EffectiveAuthRef() != "claude:work" {
		t.Fatalf("clearing API key reset auth ref: %q", cleared.EffectiveAuthRef())
	}
}

func TestProfileFromDTO_SwitchProviderFromAPIKeyDoesNotSendOldKey(t *testing.T) {
	existing := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		AuthProvider:  "codex",
		AuthRef:       "codex:default",
		Credential: config.CredentialConfig{
			Mode:    config.CredentialModeAPIKey,
			AuthRef: "codex:default",
			APIKey:  "fake-old-provider-key",
		},
		APIKey: "fake-old-provider-key",
	}
	got, err := profileFromDTO(existing, profileDTO{
		OpenAIBaseURL: "https://api.anthropic.com",
		AuthProvider:  "claude",
		OpenAIAPIType: "anthropic_messages",
		Models:        []string{"claude-sonnet-4-20250514"},
		DefaultModel:  "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthProvider != "claude" {
		t.Fatalf("AuthProvider = %q", got.AuthProvider)
	}
	if got.EffectiveAuthRef() != "claude:default" {
		t.Fatalf("EffectiveAuthRef = %q", got.EffectiveAuthRef())
	}
	if got.EffectiveAPIKey() != "fake-old-provider-key" {
		t.Fatalf("API key should be preserved inactive, got %q", got.EffectiveAPIKey())
	}
	if got.Credential.Mode != config.CredentialModeAuth {
		t.Fatalf("mode = %q, want auth so old api_key is not sent", got.Credential.Mode)
	}

	emptyStore := auth.NewStore(filepath.Join(t.TempDir(), "auth"))
	emptyCred := engine.ResolveRequestCredentialWithStore(context.Background(), got, nil, emptyStore)
	if emptyCred.Value != "" || emptyCred.Source == "profile.api_key" {
		t.Fatalf("old API key was used for new provider: %+v", emptyCred)
	}

	loggedInStore := auth.NewStore(filepath.Join(t.TempDir(), "auth"))
	if err := loggedInStore.Save(&auth.Auth{
		Provider:    auth.ProviderClaude,
		Kind:        auth.KindOAuth,
		AccessToken: "claude-login-token",
	}); err != nil {
		t.Fatal(err)
	}
	loggedInCred := engine.ResolveRequestCredentialWithStore(context.Background(), got, nil, loggedInStore)
	if loggedInCred.Value != "claude-login-token" || loggedInCred.Source != "oauth.claude" {
		t.Fatalf("expected claude login cred, got %+v", loggedInCred)
	}

	newKey := "sk-new-claude-key"
	replaced, err := profileFromDTO(existing, profileDTO{
		OpenAIBaseURL: "https://api.anthropic.com",
		AuthProvider:  "claude",
		OpenAIAPIType: "anthropic_messages",
		APIKey:        &newKey,
		Models:        []string{"claude-sonnet-4-20250514"},
		DefaultModel:  "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Credential.Mode != config.CredentialModeAPIKey {
		t.Fatalf("mode = %q, want api_key after explicit replacement key", replaced.Credential.Mode)
	}
	if replaced.EffectiveAPIKey() != newKey {
		t.Fatalf("replacement key = %q", replaced.EffectiveAPIKey())
	}
	replacedCred := engine.ResolveRequestCredentialWithStore(context.Background(), replaced, nil, loggedInStore)
	if replacedCred.Value != newKey || replacedCred.Source != "profile.api_key" {
		t.Fatalf("expected replacement API key, got %+v", replacedCred)
	}
}

func TestProfileUpdateSwitchesAuthProviderBinding(t *testing.T) {
	h := newTestHandler(t)
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "work",
		Profiles: map[string]*config.Profile{
			"work": {
				OpenAIBaseURL: "https://api.openai.com/v1",
				OpenAIAPIType: "responses",
				AuthProvider:  "codex",
				AuthRef:       "codex:default",
				Credential: config.CredentialConfig{
					Mode:    config.CredentialModeAuth,
					AuthRef: "codex:default",
					APIKey:  "secret-key",
				},
				APIKey:       "secret-key",
				Models:       []string{"gpt-5"},
				DefaultModel: "gpt-5",
			},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	postJSON(t, h, http.MethodPut, "/api/profiles/work", `{
		"name":"work",
		"auth_provider":"claude",
		"openai_base_url":"https://api.anthropic.com",
		"openai_api_type":"anthropic_messages",
		"models":["claude-sonnet-4-20250514"],
		"default_model":"claude-sonnet-4-20250514"
	}`, http.StatusOK)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["work"]
	if p == nil {
		t.Fatal("missing profile")
	}
	if p.AuthProvider != "claude" {
		t.Fatalf("AuthProvider = %q", p.AuthProvider)
	}
	if p.EffectiveAuthRef() != "claude:default" {
		t.Fatalf("EffectiveAuthRef = %q", p.EffectiveAuthRef())
	}
	if p.Credential.AuthRef != "claude:default" || p.AuthRef != "claude:default" {
		t.Fatalf("auth refs not synced: cred=%q auth_ref=%q", p.Credential.AuthRef, p.AuthRef)
	}
	if p.EffectiveAPIKey() != "secret-key" {
		t.Fatalf("API key should be preserved, got %q", p.EffectiveAPIKey())
	}
	if p.Credential.Mode != config.CredentialModeAuth {
		t.Fatalf("mode = %q, want auth after provider switch", p.Credential.Mode)
	}
	emptyStore := auth.NewStore(filepath.Join(t.TempDir(), "auth"))
	cred := engine.ResolveRequestCredentialWithStore(context.Background(), p, nil, emptyStore)
	if cred.Value != "" || cred.Source == "profile.api_key" {
		t.Fatalf("old API key was sent to new provider: %+v", cred)
	}

	postJSON(t, h, http.MethodPut, "/api/profiles/work", `{
		"name":"work",
		"openai_base_url":"https://api.anthropic.com/v2",
		"openai_api_type":"anthropic_messages",
		"models":["claude-sonnet-4-20250514"],
		"default_model":"claude-sonnet-4-20250514"
	}`, http.StatusOK)
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p = cfg.Profiles["work"]
	p.Credential.AuthRef = "claude:work"
	p.AuthRef = "claude:work"
	p.AuthProvider = "claude"
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	postJSON(t, h, http.MethodPut, "/api/profiles/work", `{
		"name":"work",
		"openai_base_url":"https://api.anthropic.com/v3",
		"openai_api_type":"anthropic_messages",
		"models":["claude-sonnet-4-20250514"],
		"default_model":"claude-sonnet-4-20250514"
	}`, http.StatusOK)
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p = cfg.Profiles["work"]
	if p.EffectiveAuthRef() != "claude:work" {
		t.Fatalf("custom account binding lost: %q", p.EffectiveAuthRef())
	}

	postJSON(t, h, http.MethodPut, "/api/profiles/work", `{
		"name":"work",
		"openai_base_url":"https://api.anthropic.com/v3",
		"clear_api_key":true,
		"openai_api_type":"anthropic_messages",
		"models":["claude-sonnet-4-20250514"],
		"default_model":"claude-sonnet-4-20250514"
	}`, http.StatusOK)
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles["work"].EffectiveAPIKey() != "" {
		t.Fatal("expected API key to be cleared")
	}
	if cfg.Profiles["work"].EffectiveAuthRef() != "claude:work" {
		t.Fatalf("clearing API key reset auth ref: %q", cfg.Profiles["work"].EffectiveAuthRef())
	}
}

func TestProfileUpdateSwitchesAPIKeyModeAwayFromOldKey(t *testing.T) {
	h := newTestHandler(t)
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "work",
		Profiles: map[string]*config.Profile{
			"work": {
				OpenAIBaseURL: "https://api.openai.com/v1",
				OpenAIAPIType: "responses",
				AuthProvider:  "codex",
				AuthRef:       "codex:default",
				Credential: config.CredentialConfig{
					Mode:    config.CredentialModeAPIKey,
					AuthRef: "codex:default",
					APIKey:  "fake-old-provider-key",
				},
				APIKey:       "fake-old-provider-key",
				Models:       []string{"gpt-5"},
				DefaultModel: "gpt-5",
			},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	postJSON(t, h, http.MethodPut, "/api/profiles/work", `{
		"name":"work",
		"auth_provider":"claude",
		"openai_base_url":"https://api.anthropic.com",
		"openai_api_type":"anthropic_messages",
		"models":["claude-sonnet-4-20250514"],
		"default_model":"claude-sonnet-4-20250514"
	}`, http.StatusOK)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["work"]
	if p == nil {
		t.Fatal("missing profile")
	}
	if p.AuthProvider != "claude" {
		t.Fatalf("AuthProvider = %q", p.AuthProvider)
	}
	if p.EffectiveAuthRef() != "claude:default" {
		t.Fatalf("EffectiveAuthRef = %q", p.EffectiveAuthRef())
	}
	if p.EffectiveAPIKey() != "fake-old-provider-key" {
		t.Fatalf("API key should be preserved inactive, got %q", p.EffectiveAPIKey())
	}
	if p.Credential.Mode != config.CredentialModeAuth {
		t.Fatalf("mode = %q, want auth after provider switch from api_key", p.Credential.Mode)
	}

	emptyStore := auth.NewStore(filepath.Join(t.TempDir(), "auth"))
	emptyCred := engine.ResolveRequestCredentialWithStore(context.Background(), p, nil, emptyStore)
	if emptyCred.Value != "" || emptyCred.Source == "profile.api_key" {
		t.Fatalf("old API key was sent to new provider: %+v", emptyCred)
	}

	loggedInStore := auth.NewStore(filepath.Join(t.TempDir(), "auth"))
	if err := loggedInStore.Save(&auth.Auth{
		Provider:    auth.ProviderClaude,
		Kind:        auth.KindOAuth,
		AccessToken: "claude-login-token",
	}); err != nil {
		t.Fatal(err)
	}
	loggedInCred := engine.ResolveRequestCredentialWithStore(context.Background(), p, nil, loggedInStore)
	if loggedInCred.Value != "claude-login-token" || loggedInCred.Source != "oauth.claude" {
		t.Fatalf("expected claude login cred after reload, got %+v", loggedInCred)
	}

	postJSON(t, h, http.MethodPut, "/api/profiles/work", `{
		"name":"work",
		"auth_provider":"gemini",
		"openai_base_url":"https://generativelanguage.googleapis.com/v1beta",
		"openai_api_type":"gemini_generate_content",
		"api_key":"sk-new-gemini-key",
		"models":["gemini-2.5-pro"],
		"default_model":"gemini-2.5-pro"
	}`, http.StatusOK)
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p = cfg.Profiles["work"]
	if p.AuthProvider != "gemini" {
		t.Fatalf("AuthProvider = %q", p.AuthProvider)
	}
	if p.Credential.Mode != config.CredentialModeAPIKey {
		t.Fatalf("mode = %q, want api_key after explicit replacement key", p.Credential.Mode)
	}
	if p.EffectiveAPIKey() != "sk-new-gemini-key" {
		t.Fatalf("replacement key = %q", p.EffectiveAPIKey())
	}
	replacedCred := engine.ResolveRequestCredentialWithStore(context.Background(), p, nil, emptyStore)
	if replacedCred.Value != "sk-new-gemini-key" || replacedCred.Source != "profile.api_key" {
		t.Fatalf("expected replacement API key after reload, got %+v", replacedCred)
	}
}

func TestSPAFallback(t *testing.T) {
	h := newTestHandler(t)
	body := request(t, h, http.MethodGet, "/missing/route", "", http.StatusOK)
	if !strings.Contains(body, `<div id="root"`) {
		t.Fatalf("expected SPA index fallback, got %s", body)
	}
}

func TestCodexModelsEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CODEX_HOME", tmpDir)

	// Create test models_cache.json
	cacheContent := `{
		"fetched_at": "2024-01-15T10:30:00Z",
		"etag": "abc123",
		"client_version": "1.0.0",
		"models": [
			{
				"slug": "test-model-1",
				"display_name": "Test Model 1",
				"description": "First test model",
				"base_instructions": "You are test model 1.",
				"context_window": 100000
			},
			{
				"slug": "test-model-2",
				"display_name": "Test Model 2",
				"description": "Second test model",
				"base_instructions": "You are test model 2.",
				"context_window": 200000
			}
		]
	}`

	cachePath := tmpDir + "/models_cache.json"
	if err := os.WriteFile(cachePath, []byte(cacheContent), 0644); err != nil {
		t.Fatalf("failed to write test cache file: %v", err)
	}

	h := newTestHandler(t)
	body := request(t, h, http.MethodGet, "/api/codex/models", "", http.StatusOK)

	var result struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.Models))
	}

	if result.Models[0] != "test-model-1" {
		t.Errorf("expected first model 'test-model-1', got %q", result.Models[0])
	}

	if result.Models[1] != "test-model-2" {
		t.Errorf("expected second model 'test-model-2', got %q", result.Models[1])
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, err := newHandler("")
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func postJSON(t *testing.T, h http.Handler, method, target, body string, want int) string {
	t.Helper()
	return request(t, h, method, target, body, want)
}

func request(t *testing.T, h http.Handler, method, target, body string, want int) string {
	t.Helper()
	var rbody *bytes.Reader
	if body == "" {
		rbody = bytes.NewReader(nil)
	} else {
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			t.Fatalf("invalid test json: %v", err)
		}
		rbody = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, target, rbody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, target, rec.Code, want, rec.Body.String())
	}
	return rec.Body.String()
}
