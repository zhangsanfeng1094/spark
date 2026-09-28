package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"spark/internal/config"
)

// bindingTestCfg builds a config where "shared" is enabled for codex+claude
// but disabled for one/grok/agy, and "grokonly" is enabled only for grok.
func bindingTestCfg(t *testing.T) *config.RootConfig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "default",
		Profiles:       map[string]*config.Profile{"default": {}},
		Integrations:   map[string]*config.IntegrationConfig{},
		McpServers: map[string]*config.McpServerConfig{
			"shared":   {Command: "shared-mcp", Enabled: true},
			"grokonly": {Command: "grok-only-mcp", Enabled: false},
		},
	}
	off := false
	on := true
	cfg.McpServers["shared"].Agents = map[string]*config.McpAgentBinding{
		"one":  {Agent: "one", Enabled: &off},
		"grok": {Agent: "grok", Enabled: &off},
		"agy":  {Agent: "agy", Enabled: &off},
	}
	cfg.McpServers["grokonly"].Agents = map[string]*config.McpAgentBinding{
		"grok": {Agent: "grok", Enabled: &on},
	}

	sparkDir := filepath.Join(home, ".spark")
	if err := os.MkdirAll(sparkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sparkDir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAgentBindingsDifferPerAgent(t *testing.T) {
	cfg := bindingTestCfg(t)

	if !cfg.McpAgentEnabled("shared", "codex") || !cfg.McpAgentEnabled("shared", "claude") {
		t.Fatal("shared should be enabled for codex+claude")
	}
	for _, agent := range []string{"one", "grok", "agy"} {
		if cfg.McpAgentEnabled("shared", agent) {
			t.Fatalf("shared should be disabled for %s", agent)
		}
	}
	if !cfg.McpAgentEnabled("grokonly", "grok") {
		t.Fatal("grokonly should be enabled for grok")
	}
	if cfg.McpAgentEnabled("grokonly", "codex") {
		t.Fatal("grokonly should be disabled for codex (definition default off)")
	}
}

func TestCodexInjectionOnlyIncludesCodexEnabled(t *testing.T) {
	cfg := bindingTestCfg(t)
	got := codexMCPArgs(cfg.McpServersForAgent("codex"))

	joined := argJoin(got)
	if !strings.Contains(joined, `mcp_servers.shared.command="shared-mcp"`) {
		t.Fatalf("expected shared server in codex args, got %v", got)
	}
	if strings.Contains(joined, "grokonly") || strings.Contains(joined, "grok-only-mcp") {
		t.Fatalf("codex args must not include grok-only server, got %v", got)
	}
}

func TestClaudeInjectionOnlyIncludesClaudeEnabled(t *testing.T) {
	bindingTestCfg(t)

	mcpPath, err := writeClaudeTempMCP()
	if err != nil {
		t.Fatalf("writeClaudeTempMCP: %v", err)
	}
	defer os.Remove(mcpPath)

	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["shared"]; !ok {
		t.Fatalf("expected shared in claude temp mcp, got %v", servers)
	}
	if _, ok := servers["grokonly"]; ok {
		t.Fatalf("claude temp mcp must not include grokonly, got %v", servers)
	}
}

func TestGrokInjectionOnlyIncludesGrokEnabled(t *testing.T) {
	bindingTestCfg(t)

	profile := &config.Profile{
		OpenAIBaseURL: "http://gw.example/v1",
		OpenAIAPIKey:  "sk-test",
		OpenAIAPIType: "responses,chat_completions",
	}
	if err := syncGrokLaunchConfig(profile, "grok-4.5", "spark-grok-4.5"); err != nil {
		t.Fatalf("syncGrokLaunchConfig: %v", err)
	}

	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".grok", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if _, err := toml.Decode(string(data), &root); err != nil {
		t.Fatal(err)
	}
	mcpServers := root["mcp_servers"].(map[string]any)
	if _, ok := mcpServers["grokonly"]; !ok {
		t.Fatalf("grok mcp_servers must include grokonly, got %#v", mcpServers)
	}
	if _, ok := mcpServers["shared"]; ok {
		t.Fatalf("grok mcp_servers must not include shared, got %#v", mcpServers)
	}
}

func TestOneInjectionOnlyIncludesOneEnabled(t *testing.T) {
	bindingTestCfg(t)

	if err := syncOneMCP(); err != nil {
		t.Fatalf("syncOneMCP: %v", err)
	}

	home, _ := os.UserHomeDir()
	mcpPath := filepath.Join(home, ".one", "agent", "mcp.json")
	if _, err := os.Stat(mcpPath); os.IsNotExist(err) {
		// No servers enabled for one → no file written. That is the
		// expected outcome: nothing injected.
		return
	}
	root := readMap(mcpPath)
	servers, _ := root["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Fatalf("one mcp.json should be empty (shared disabled for one), got %#v", servers)
	}
}

func TestAgyInjectionOnlyIncludesAgyEnabled(t *testing.T) {
	bindingTestCfg(t)

	if err := writeAgyMCP(filepath.Join(homeDir(t), ".gemini")); err != nil {
		t.Fatalf("writeAgyMCP: %v", err)
	}

	home, _ := os.UserHomeDir()
	root := readMap(filepath.Join(home, ".gemini", "config", "mcp_config.json"))
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["shared"]; ok {
		t.Fatalf("agy mcp_config must not include shared, got %#v", servers)
	}
	if _, ok := servers["grokonly"]; ok {
		t.Fatalf("agy mcp_config must not include grokonly, got %#v", servers)
	}
}

func TestLegacyEnabledInheritedForAllAgents(t *testing.T) {
	// Old configs have no Agents map; the definition Enabled must keep
	// working for every agent.
	cfg := &config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"legacy": {Command: "legacy-mcp", Enabled: true},
	}}
	for _, agent := range config.McpAgents() {
		if !cfg.McpAgentEnabled("legacy", agent) {
			t.Fatalf("legacy server should be enabled for %s", agent)
		}
	}
	servers := cfg.McpServersForAgent("codex")
	if _, ok := servers["legacy"]; !ok {
		t.Fatal("legacy server missing from codex effective servers")
	}
}

func TestCodexLegacyAdvancedFieldsLiveOnBinding(t *testing.T) {
	// Legacy codex fields on the definition migrate onto the codex binding
	// during Normalize and still drive codex injection.
	timeout := 15
	cfg := &config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"withadv": {Command: "adv-mcp", Enabled: true, StartupTimeout: &timeout},
	}}
	config.Normalize(cfg)

	binding := cfg.McpBinding("withadv", "codex")
	if binding == nil || binding.StartupTimeout == nil || *binding.StartupTimeout != 15 {
		t.Fatalf("expected legacy startup_timeout on codex binding, got %#v", binding)
	}

	args := codexMCPArgs(cfg.McpServersForAgent("codex"))
	joined := argJoin(args)
	if !strings.Contains(joined, "mcp_servers.withadv.startup_timeout_sec=15") {
		t.Fatalf("expected startup_timeout in codex args, got %v", args)
	}

	// The advanced field must NOT leak into other agents' effective config.
	eff := cfg.McpEffectiveServer("withadv", "claude")
	if eff.StartupTimeout != nil && *eff.StartupTimeout == 15 && eff.Command == "adv-mcp" {
		// StartupTimeout is definition-level storage; only codex injection
		// consumes it, claude ignores it (EncodeClaudeServerMap drops it).
		encoded := config.EncodeClaudeServerMap(eff)
		if _, ok := encoded["startup_timeout_sec"]; ok {
			t.Fatal("claude encoding must not carry codex startup_timeout")
		}
	}
}

func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func argJoin(args []string) string {
	return strings.Join(args, " ")
}
