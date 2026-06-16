package integrations

import (
	"os"
	"reflect"
	"strings"
	"testing"

	compatproxy "spark/internal/compat/proxy"
	"spark/internal/config"
)

func TestCodexArgs(t *testing.T) {
	c := &Codex{}
	got := c.args("glm-5:cloud", "https://api.example.com/v1", []string{"--no-alt-screen"})
	want := []string{
		"-c", `model_providers.spark.name="Spark"`,
		"-c", `model_providers.spark.base_url="https://api.example.com/v1"`,
		"-c", `model_providers.spark.env_key="OPENAI_API_KEY"`,
		"-c", `model_providers.spark.wire_api="responses"`,
		"-c", `model_providers.spark.requires_openai_auth=false`,
		"-c", `model_provider="spark"`,
		"-m", "glm-5:cloud",
		"--no-alt-screen",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codex args mismatch, got %v want %v", got, want)
	}
}

func TestCodexPromptArgsReplace(t *testing.T) {
	c := &Codex{}
	got := c.argsWithPrompt("gpt-5", "https://api.example.com/v1", []string{"resume"}, &config.PromptInjection{
		Mode: config.PromptModeReplace,
		Path: "/tmp/prompt.md",
	})
	wantTail := []string{"-c", "model_instructions_file=/tmp/prompt.md", "resume"}
	if !reflect.DeepEqual(got[len(got)-len(wantTail):], wantTail) {
		t.Fatalf("unexpected replace prompt args: %v", got)
	}
}

func TestCodexPromptArgsNilPath(t *testing.T) {
	got := codexPromptArgs(&config.PromptInjection{
		Mode:    config.PromptModeReplace,
		Content: "some content",
	})
	if got != nil {
		t.Fatalf("expected nil args when path is empty, got %v", got)
	}
}

func TestCodexPromptArgsNil(t *testing.T) {
	if got := codexPromptArgs(nil); got != nil {
		t.Fatalf("expected nil for nil prompt, got %v", got)
	}
}

func TestExtractDeveloperPrompt(t *testing.T) {
	input := []byte(`[
		{"type":"message","role":"developer","content":[
			{"type":"input_text","text":"You are a helpful assistant."},
			{"type":"input_text","text":"Be concise."}
		]},
		{"type":"message","role":"user","content":[
			{"type":"input_text","text":"hello"}
		]}
	]`)
	got, err := extractDeveloperPrompt(input)
	if err != nil {
		t.Fatalf("extractDeveloperPrompt failed: %v", err)
	}
	want := "You are a helpful assistant.\nBe concise."
	if got != want {
		t.Fatalf("extractDeveloperPrompt = %q, want %q", got, want)
	}
}

func TestExtractDeveloperPromptNoDeveloper(t *testing.T) {
	input := []byte(`[
		{"type":"message","role":"user","content":[
			{"type":"input_text","text":"hello"}
		]}
	]`)
	_, err := extractDeveloperPrompt(input)
	if err == nil {
		t.Fatal("expected error for missing developer message")
	}
}

func TestExtractDeveloperPromptInvalidJSON(t *testing.T) {
	_, err := extractDeveloperPrompt([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCodexModelCatalogArgs(t *testing.T) {
	c := &Codex{}
	got := c.argsWithConfigAndPrompt("glm-5.1", "https://api.example.com/v1", &config.IntegrationConfig{
		ModelCatalogJSON: " /home/me/.codex/custom_models.json ",
	}, []string{"resume"}, nil)
	wantTail := []string{
		"-c", `model_catalog_json="/home/me/.codex/custom_models.json"`,
		"-m", "glm-5.1",
		"resume",
	}
	if !reflect.DeepEqual(got[len(got)-len(wantTail):], wantTail) {
		t.Fatalf("unexpected model catalog args tail: %v", got)
	}
}

func TestCodexEnv(t *testing.T) {
	profile := &config.Profile{
		OpenAIOrg:     "org-test",
		OpenAIProject: "proj-test",
	}

	t.Run("does not inject OPENAI_BASE_URL", func(t *testing.T) {
		got := codexEnv(profile, "key-123")
		if containsEnvKey(got, "OPENAI_BASE_URL") {
			t.Fatalf("expected OPENAI_BASE_URL to be omitted, got %v", got)
		}
		if !containsEnvEntry(got, "OPENAI_ORG_ID=org-test") {
			t.Fatalf("expected OPENAI_ORG_ID to be preserved, got %v", got)
		}
		if !containsEnvEntry(got, "OPENAI_PROJECT_ID=proj-test") {
			t.Fatalf("expected OPENAI_PROJECT_ID to be preserved, got %v", got)
		}
		if !containsEnvEntry(got, "OPENAI_API_KEY=key-123") {
			t.Fatalf("expected OPENAI_API_KEY to be preserved, got %v", got)
		}
		if !containsEnvEntry(got, "CODEX_API_KEY=key-123") {
			t.Fatalf("expected CODEX_API_KEY to be preserved, got %v", got)
		}
	})

	t.Run("omits empty key entries", func(t *testing.T) {
		got := codexEnv(profile, "")
		if containsEnvKey(got, "OPENAI_API_KEY") {
			t.Fatalf("expected OPENAI_API_KEY to be omitted, got %v", got)
		}
		if containsEnvKey(got, "CODEX_API_KEY") {
			t.Fatalf("expected CODEX_API_KEY to be omitted, got %v", got)
		}
	})
}

func TestCodexProxyModeForAPIType(t *testing.T) {
	tests := []struct {
		name         string
		apiType      string
		wantMode     compatproxy.ResponsesProxyMode
		wantUseProxy bool
	}{
		{name: "responses no proxy", apiType: config.OpenAIAPITypeResponses, wantUseProxy: false},
		{name: "responses+chat direct", apiType: config.DefaultOpenAIAPIType, wantUseProxy: false},
		{name: "chat only compat", apiType: config.OpenAIAPITypeChatCompletions, wantMode: compatproxy.ResponsesProxyModeChatCompletionsOnly, wantUseProxy: true},
		{name: "anthropic messages compat", apiType: config.OpenAIAPITypeAnthropicMessages, wantMode: compatproxy.ResponsesProxyModeAnthropicMessagesOnly, wantUseProxy: true},
		{name: "unknown compat", apiType: "unknown", wantMode: compatproxy.ResponsesProxyModeChatCompletionsOnly, wantUseProxy: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMode, gotUseProxy := codexProxyModeForAPIType(tt.apiType)
			if gotUseProxy != tt.wantUseProxy || gotMode != tt.wantMode {
				t.Fatalf("codexProxyModeForAPIType(%q)=(%q,%v) want (%q,%v)", tt.apiType, gotMode, gotUseProxy, tt.wantMode, tt.wantUseProxy)
			}
		})
	}
}

func TestResolveOpenAIAPIKey(t *testing.T) {
	t.Run("profile key wins", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		t.Setenv("CODEX_API_KEY", "")
		got, source := resolveOpenAIAPIKey("profile-key")
		if got != "profile-key" || source != "profile.api_key" {
			t.Fatalf("expected profile key, got key=%q source=%q", got, source)
		}
	})

	t.Run("fallback to OPENAI_API_KEY", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "env-openai")
		t.Setenv("CODEX_API_KEY", "env-codex")
		got, source := resolveOpenAIAPIKey("")
		if got != "env-openai" || source != "env.OPENAI_API_KEY" {
			t.Fatalf("expected OPENAI_API_KEY, got key=%q source=%q", got, source)
		}
	})

	t.Run("does not fallback to CODEX_API_KEY", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		t.Setenv("CODEX_API_KEY", "env-codex")
		got, source := resolveOpenAIAPIKey("")
		if got != "" || source != "none" {
			t.Fatalf("expected empty key, got key=%q source=%q", got, source)
		}
	})

	t.Run("env overrides profile", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "env-openai")
		t.Setenv("CODEX_API_KEY", "env-codex")
		got, source := resolveOpenAIAPIKey("profile-key")
		if got != "env-openai" || source != "env.OPENAI_API_KEY" {
			t.Fatalf("expected env OPENAI_API_KEY to override profile, got key=%q source=%q", got, source)
		}
	})
}

func TestFetchCodexModelCatalog(t *testing.T) {
	// Create a temporary test directory
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

	models, err := fetchCodexModelCatalog()
	if err != nil {
		t.Fatalf("fetchCodexModelCatalog failed: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	if models[0].Slug != "test-model-1" {
		t.Errorf("expected first model slug 'test-model-1', got %q", models[0].Slug)
	}

	if models[0].BaseInstructions != "You are test model 1." {
		t.Errorf("expected first model instructions 'You are test model 1.', got %q", models[0].BaseInstructions)
	}

	if models[1].Slug != "test-model-2" {
		t.Errorf("expected second model slug 'test-model-2', got %q", models[1].Slug)
	}
}

func TestFetchCodexModelInstructions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CODEX_HOME", tmpDir)

	cacheContent := `{
		"models": [
			{
				"slug": "gpt-5.2",
				"base_instructions": "You are GPT 5.2."
			},
			{
				"slug": "gpt-4o",
				"base_instructions": "You are GPT 4o."
			}
		]
	}`

	cachePath := tmpDir + "/models_cache.json"
	if err := os.WriteFile(cachePath, []byte(cacheContent), 0644); err != nil {
		t.Fatalf("failed to write test cache file: %v", err)
	}

	t.Run("exact match", func(t *testing.T) {
		got, err := fetchCodexModelInstructions("gpt-4o")
		if err != nil {
			t.Fatalf("fetchCodexModelInstructions failed: %v", err)
		}
		want := "You are GPT 4o."
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("prefix match", func(t *testing.T) {
		got, err := fetchCodexModelInstructions("gpt-5")
		if err != nil {
			t.Fatalf("fetchCodexModelInstructions failed: %v", err)
		}
		want := "You are GPT 5.2."
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty slug returns first model", func(t *testing.T) {
		got, err := fetchCodexModelInstructions("")
		if err != nil {
			t.Fatalf("fetchCodexModelInstructions failed: %v", err)
		}
		want := "You are GPT 5.2."
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("model not found", func(t *testing.T) {
		_, err := fetchCodexModelInstructions("nonexistent-model")
		if err == nil {
			t.Fatal("expected error for nonexistent model")
		}
	})
}

func TestFetchCodexModelCatalogMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CODEX_HOME", tmpDir)

	_, err := fetchCodexModelCatalog()
	if err == nil {
		t.Fatal("expected error when models_cache.json doesn't exist")
	}
}

func TestFetchCodexModelCatalogInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CODEX_HOME", tmpDir)

	cachePath := tmpDir + "/models_cache.json"
	if err := os.WriteFile(cachePath, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("failed to write test cache file: %v", err)
	}

	_, err := fetchCodexModelCatalog()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGetCodexHome(t *testing.T) {
	t.Run("uses CODEX_HOME env var", func(t *testing.T) {
		t.Setenv("CODEX_HOME", "/custom/codex/path")
		got, err := getCodexHome()
		if err != nil {
			t.Fatalf("getCodexHome failed: %v", err)
		}
		if got != "/custom/codex/path" {
			t.Errorf("expected '/custom/codex/path', got %q", got)
		}
	})

	t.Run("defaults to ~/.codex", func(t *testing.T) {
		t.Setenv("CODEX_HOME", "")
		got, err := getCodexHome()
		if err != nil {
			t.Fatalf("getCodexHome failed: %v", err)
		}
		// Should end with .codex
		if !strings.HasSuffix(got, ".codex") {
			t.Errorf("expected path to end with '.codex', got %q", got)
		}
	})
}

func containsEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func containsEnvEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
