package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestWriteClaudeLaunchHome(t *testing.T) {
	tmpDir := t.TempDir()
	realClaude := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(filepath.Join(realClaude, "skills"), 0o755); err != nil {
		t.Fatalf("create skills dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(realClaude, "plugins"), 0o755); err != nil {
		t.Fatalf("create plugins dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realClaude, "skills", "test.md"), []byte("# test"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realClaude, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatalf("write mcp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realClaude, ".auth.lock"), []byte("lock"), 0o600); err != nil {
		t.Fatalf("write auth lock: %v", err)
	}

	// Mock user home
	t.Setenv("HOME", tmpDir)

	// Mock ~/.claude.json
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

	launchHome := filepath.Join(tmpDir, "launch-claude")
	mcpPath, err := writeClaudeLaunchHome(launchHome)
	if err != nil {
		t.Fatalf("writeClaudeLaunchHome failed: %v", err)
	}

	if mcpPath == "" {
		t.Fatalf("expected mcpPath to be non-empty")
	}

	// Verify skills directory is symlinked
	skillsLink := filepath.Join(launchHome, "skills")
	fi, err := os.Lstat(skillsLink)
	if err != nil {
		t.Fatalf("skills link missing: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("skills should be a symlink")
	}

	// Verify .claude.json is seeded as an isolated regular file (NOT a symlink)
	claudeJSONPath := filepath.Join(launchHome, ".claude.json")
	claudeFI, err := os.Lstat(claudeJSONPath)
	if err != nil {
		t.Fatalf(".claude.json missing in launch home: %v", err)
	}
	if claudeFI.Mode()&os.ModeSymlink != 0 {
		t.Fatalf(".claude.json must be a regular file, not a symlink, to prevent tampering with host config")
	}
	claudeData, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		t.Fatalf("read launch .claude.json: %v", err)
	}
	var claudeRoot map[string]any
	if err := json.Unmarshal(claudeData, &claudeRoot); err != nil {
		t.Fatalf("parse launch .claude.json: %v", err)
	}
	if completed, ok := claudeRoot["hasCompletedOnboarding"].(bool); !ok || !completed {
		t.Fatalf("expected hasCompletedOnboarding to be true, got %v", claudeRoot["hasCompletedOnboarding"])
	}
	// Verify mcpServers in .claude.json is empty map so --mcp-config is authoritative
	if srvs, ok := claudeRoot["mcpServers"].(map[string]any); !ok || len(srvs) != 0 {
		t.Fatalf("expected mcpServers in launch .claude.json to be empty, got %#v", claudeRoot["mcpServers"])
	}

	// Verify excluded files
	if _, err := os.Lstat(filepath.Join(launchHome, ".auth.lock")); err == nil {
		t.Fatalf(".auth.lock should not be mirrored")
	}

	// Verify generated mcp.json contains claude-custom server
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

func TestShouldSkipClaudeMirror(t *testing.T) {
	if !shouldSkipClaudeMirror("mcp.json") {
		t.Errorf("mcp.json should be skipped")
	}
	if !shouldSkipClaudeMirror("claude.json") {
		t.Errorf("claude.json should be skipped")
	}
	if !shouldSkipClaudeMirror(".claude.json") {
		t.Errorf(".claude.json should be skipped")
	}
	if !shouldSkipClaudeMirror(".auth.lock") {
		t.Errorf(".auth.lock should be skipped")
	}
	if !shouldSkipClaudeMirror("session.lock") {
		t.Errorf("session.lock should be skipped")
	}
	if shouldSkipClaudeMirror("skills") {
		t.Errorf("skills should not be skipped")
	}
	if shouldSkipClaudeMirror("plugins") {
		t.Errorf("plugins should not be skipped")
	}
	if shouldSkipClaudeMirror("projects") {
		t.Errorf("projects should not be skipped")
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

func TestWriteClaudeLaunchConfig_NoHostConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	launchHome := filepath.Join(tmpDir, "launch")
	if err := os.MkdirAll(launchHome, 0o755); err != nil {
		t.Fatalf("mkdir launch: %v", err)
	}

	if err := writeClaudeLaunchConfig(launchHome); err != nil {
		t.Fatalf("writeClaudeLaunchConfig failed: %v", err)
	}

	cfgPath := filepath.Join(launchHome, ".claude.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read launch config: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	if val, ok := root["hasCompletedOnboarding"].(bool); !ok || !val {
		t.Fatalf("expected hasCompletedOnboarding=true, got %v", root["hasCompletedOnboarding"])
	}
}

func TestWriteClaudeLaunchConfig_DoesNotTamperHostConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	hostCfgPath := filepath.Join(tmpDir, ".claude.json")
	originalContent := `{
  "hasCompletedOnboarding": false,
  "theme": "light",
  "numStartups": 5
}`
	if err := os.WriteFile(hostCfgPath, []byte(originalContent), 0o600); err != nil {
		t.Fatalf("write host config: %v", err)
	}

	launchHome := filepath.Join(tmpDir, "launch")
	if err := os.MkdirAll(launchHome, 0o755); err != nil {
		t.Fatalf("mkdir launch: %v", err)
	}

	if err := writeClaudeLaunchConfig(launchHome); err != nil {
		t.Fatalf("writeClaudeLaunchConfig failed: %v", err)
	}

	// Verify launch config has hasCompletedOnboarding=true
	launchCfgPath := filepath.Join(launchHome, ".claude.json")
	data, err := os.ReadFile(launchCfgPath)
	if err != nil {
		t.Fatalf("read launch config: %v", err)
	}
	var launchRoot map[string]any
	if err := json.Unmarshal(data, &launchRoot); err != nil {
		t.Fatalf("unmarshal launch config: %v", err)
	}
	if val, ok := launchRoot["hasCompletedOnboarding"].(bool); !ok || !val {
		t.Fatalf("expected launch hasCompletedOnboarding=true")
	}

	// Simulate Claude Code writing/mutating launch config during session
	launchRoot["numStartups"] = 6
	launchRoot["projects"] = map[string]any{"/some/project": true}
	modifiedData, _ := json.MarshalIndent(launchRoot, "", "  ")
	if err := os.WriteFile(launchCfgPath, modifiedData, 0o600); err != nil {
		t.Fatalf("write modified launch config: %v", err)
	}

	// Verify host config was NOT tampered with
	hostData, err := os.ReadFile(hostCfgPath)
	if err != nil {
		t.Fatalf("read host config: %v", err)
	}
	if string(hostData) != originalContent {
		t.Fatalf("host config was tampered with! expected:\n%s\ngot:\n%s", originalContent, string(hostData))
	}
}
