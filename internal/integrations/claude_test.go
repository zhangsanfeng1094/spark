package integrations

import (
	"testing"

	"spark/internal/config"
)

func TestResolveAnthropicAPIKey(t *testing.T) {
	p := &config.Profile{OpenAIAPIKey: "profile-openai-key"}

	t.Run("env anthropic wins", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "env-anthropic-key")
		got, source := resolveAnthropicAPIKey(p)
		if got != "env-anthropic-key" || source != "env.ANTHROPIC_API_KEY" {
			t.Fatalf("unexpected key=%q source=%q", got, source)
		}
	})

	t.Run("fallback profile openai key", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "")
		got, source := resolveAnthropicAPIKey(p)
		if got != "profile-openai-key" || source != "profile.openai_api_key" {
			t.Fatalf("unexpected key=%q source=%q", got, source)
		}
	})
}

func TestResolveAnthropicAuthToken(t *testing.T) {
	p := &config.Profile{AnthropicAuthToken: "profile-token"}

	t.Run("env wins", func(t *testing.T) {
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-token")
		got, source := resolveAnthropicAuthToken(p, false)
		if got != "env-token" || source != "env.ANTHROPIC_AUTH_TOKEN" {
			t.Fatalf("unexpected token=%q source=%q", got, source)
		}
	})

	t.Run("profile fallback", func(t *testing.T) {
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
		got, source := resolveAnthropicAuthToken(p, false)
		if got != "profile-token" || source != "profile.anthropic_auth_token" {
			t.Fatalf("unexpected token=%q source=%q", got, source)
		}
	})

	t.Run("default ollama", func(t *testing.T) {
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
		got, source := resolveAnthropicAuthToken(&config.Profile{}, true)
		if got != "ollama" || source != "compat.default" {
			t.Fatalf("unexpected token=%q source=%q", got, source)
		}
	})
}

func TestResolveClaudeModelStripsNUL(t *testing.T) {
	p := &config.Profile{
		DefaultModel: " glm-5\x00 ",
		Models:       []string{"other\x00"},
	}

	if got := resolveClaudeModel(p, " glm-5\x00 "); got != "glm-5" {
		t.Fatalf("resolveClaudeModel(flag)=%q", got)
	}
	if got := resolveClaudeModel(p, ""); got != "glm-5" {
		t.Fatalf("resolveClaudeModel(default)=%q", got)
	}
	if got := resolveClaudeModel(&config.Profile{Models: []string{"other\x00"}}, ""); got != "other" {
		t.Fatalf("resolveClaudeModel(models)=%q", got)
	}
}

func TestClaudeUsesDirectModeWhenAPITypeSupportsAnthropicMessages(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL:    "https://gateway.example.com/v1",
		OpenAIAPIKey:     "profile-key",
		OpenAIAPIType:    config.OpenAIAPITypeAnthropicMessages,
		AnthropicBaseURL: "https://gateway.example.com/anthropic",
	}

	if claudeShouldUseCompatProxy(profile) {
		t.Fatalf("expected Anthropic Messages API profile to use Claude direct mode")
	}
}

func TestClaudeUsesCompatProxyWhenAPITypeDoesNotSupportAnthropicMessages(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL:    "https://gateway.example.com/v1",
		OpenAIAPIKey:     "profile-key",
		OpenAIAPIType:    config.OpenAIAPITypeChatCompletions,
		AnthropicBaseURL: "https://gateway.example.com/anthropic",
	}

	if !claudeShouldUseCompatProxy(profile) {
		t.Fatalf("expected non-Anthropic Messages API profile to use Claude compat proxy")
	}
}

func TestClaudeDirectAuthAvoidsDefaultTokenAPIKeyConflict(t *testing.T) {
	apiKey, apiKeySource, token, tokenSource := selectClaudeDirectAuth(
		"profile-key",
		"profile.openai_api_key",
		"ollama",
		"default",
	)

	if apiKey != "" || apiKeySource != "none" {
		t.Fatalf("unexpected api key selection: key=%q source=%q", apiKey, apiKeySource)
	}
	if token != "profile-key" || tokenSource != "profile.openai_api_key" {
		t.Fatalf("expected profile API key to be used as auth token, got token=%q source=%q", token, tokenSource)
	}
}

func TestClaudeDirectAuthPrefersExplicitTokenOverAPIKey(t *testing.T) {
	apiKey, apiKeySource, token, tokenSource := selectClaudeDirectAuth(
		"profile-key",
		"profile.openai_api_key",
		"profile-token",
		"profile.anthropic_auth_token",
	)

	if apiKey != "" || apiKeySource != "none" {
		t.Fatalf("expected API key to be dropped when explicit token is set, got key=%q source=%q", apiKey, apiKeySource)
	}
	if token != "profile-token" || tokenSource != "profile.anthropic_auth_token" {
		t.Fatalf("unexpected token selection: token=%q source=%q", token, tokenSource)
	}
}
