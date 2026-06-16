package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
