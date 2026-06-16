package integrations

import (
	"reflect"
	"testing"

	"spark/internal/config"
)

func TestClaudePromptArgs(t *testing.T) {
	appendArgs := claudePromptArgs(&config.PromptInjection{Mode: config.PromptModeAppend, Path: "/tmp/extra.md"})
	if !reflect.DeepEqual(appendArgs, []string{"--append-system-prompt-file", "/tmp/extra.md"}) {
		t.Fatalf("unexpected append args: %v", appendArgs)
	}

	replaceArgs := claudePromptArgs(&config.PromptInjection{Mode: config.PromptModeReplace, Path: "/tmp/base.md"})
	if !reflect.DeepEqual(replaceArgs, []string{"--system-prompt-file", "/tmp/base.md"}) {
		t.Fatalf("unexpected replace args: %v", replaceArgs)
	}
}

func TestClaudePromptArgsEmptyPath(t *testing.T) {
	got := claudePromptArgs(&config.PromptInjection{Mode: config.PromptModeReplace, Content: "inline content"})
	if got != nil {
		t.Fatalf("expected nil when path is empty, got %v", got)
	}
}

func TestClaudePromptArgsNil(t *testing.T) {
	if got := claudePromptArgs(nil); got != nil {
		t.Fatalf("expected nil for nil prompt, got %v", got)
	}
}

func TestResolveClaudeDirectToken(t *testing.T) {
	p := &config.Profile{APIKey: "profile-key"}

	t.Run("profile key only", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "env-anthropic-key")
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
		got, source := resolveClaudeDirectToken(p)
		if got != "profile-key" || source != "profile.api_key" {
			t.Fatalf("unexpected token=%q source=%q", got, source)
		}
	})

	t.Run("does not read anthropic env", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "env-anthropic-key")
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-token")
		got, source := resolveClaudeDirectToken(&config.Profile{})
		if got != "" || source != "none" {
			t.Fatalf("unexpected token=%q source=%q", got, source)
		}
	})
}

func TestResolveClaudeCompatToken(t *testing.T) {
	t.Run("default ollama", func(t *testing.T) {
		got, source := resolveClaudeCompatToken(true)
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
		APIKey:           "profile-key",
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
		APIKey:           "profile-key",
		OpenAIAPIType:    config.OpenAIAPITypeChatCompletions,
		AnthropicBaseURL: "https://gateway.example.com/anthropic",
	}

	if !claudeShouldUseCompatProxy(profile) {
		t.Fatalf("expected non-Anthropic Messages API profile to use Claude compat proxy")
	}
}

func TestClaudeDirectAuthDropsDefaultToken(t *testing.T) {
	apiKey, apiKeySource, token, tokenSource := selectClaudeDirectAuth(
		"ollama",
		"default",
	)

	if apiKey != "" || apiKeySource != "none" {
		t.Fatalf("unexpected api key selection: key=%q source=%q", apiKey, apiKeySource)
	}
	if token != "" || tokenSource != "none" {
		t.Fatalf("expected default token to be dropped, got token=%q source=%q", token, tokenSource)
	}
}

func TestClaudeDirectAuthKeepsProfileToken(t *testing.T) {
	apiKey, apiKeySource, token, tokenSource := selectClaudeDirectAuth(
		"profile-token",
		"profile.api_key",
	)

	if apiKey != "" || apiKeySource != "none" {
		t.Fatalf("unexpected api key selection: key=%q source=%q", apiKey, apiKeySource)
	}
	if token != "profile-token" || tokenSource != "profile.api_key" {
		t.Fatalf("unexpected token selection: token=%q source=%q", token, tokenSource)
	}
}
