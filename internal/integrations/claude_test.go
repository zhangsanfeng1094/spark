package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"spark/internal/config"
)

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

func TestWriteClaudeTempMCP(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	claudeJSON := filepath.Join(tmpDir, ".claude.json")
	claudeContent := `{
		"mcpServers": {
			"claude-custom": {
				"command": "custom-mcp",
				"args": ["--port", "8080"]
			}
		}
	}`
	if err := os.WriteFile(claudeJSON, []byte(claudeContent), 0o644); err != nil {
		t.Fatalf("write claude.json: %v", err)
	}

	mcpPath, err := writeClaudeTempMCP()
	if err != nil {
		t.Fatalf("writeClaudeTempMCP failed: %v", err)
	}
	defer os.Remove(mcpPath)

	if mcpPath == "" {
		t.Fatalf("expected mcpPath to be non-empty")
	}

	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse mcp.json: %v", err)
	}
	mcpServers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcpServers map in mcp.json")
	}
	if _, ok := mcpServers["claude-custom"]; !ok {
		t.Fatalf("expected claude-custom server in mcpServers")
	}
}

func TestCliArgsHasMcpConfigFlag(t *testing.T) {
	if !cliArgsHasMcpConfigFlag([]string{"--verbose", "--mcp-config", "path.json"}) {
		t.Errorf("expected true for --mcp-config")
	}
	if !cliArgsHasMcpConfigFlag([]string{"--mcp-config=path.json"}) {
		t.Errorf("expected true for --mcp-config=...")
	}
	if cliArgsHasMcpConfigFlag([]string{"--model", "claude-sonnet"}) {
		t.Errorf("expected false when --mcp-config is absent")
	}
}
