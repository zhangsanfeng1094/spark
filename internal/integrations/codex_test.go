package integrations

import (
	"reflect"
	"testing"

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
		wantMode     responsesProxyMode
		wantUseProxy bool
	}{
		{name: "responses no proxy", apiType: config.OpenAIAPITypeResponses, wantUseProxy: false},
		{name: "responses+chat no proxy", apiType: "responses,chat_completions", wantUseProxy: false},
		{name: "auto prefer responses", apiType: config.OpenAIAPITypeAuto, wantMode: responsesProxyModePreferResponses, wantUseProxy: true},
		{name: "chat only compat", apiType: config.OpenAIAPITypeChatCompletions, wantMode: responsesProxyModeChatCompletionsOnly, wantUseProxy: true},
		{name: "unknown compat", apiType: "unknown", wantMode: responsesProxyModeChatCompletionsOnly, wantUseProxy: true},
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
		if got != "profile-key" || source != "profile.openai_api_key" {
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
