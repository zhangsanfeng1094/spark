package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMcpAgentEnabledBindingOverridesDefinition(t *testing.T) {
	cfg := &RootConfig{McpServers: map[string]*McpServerConfig{
		"docs": {Command: "npx", Enabled: true},
	}}
	off := false
	cfg.McpServers["docs"].Agents = map[string]*McpAgentBinding{
		"claude": {Agent: "claude", Enabled: &off},
	}

	if !cfg.McpAgentEnabled("docs", "codex") {
		t.Fatal("codex should inherit enabled definition")
	}
	if cfg.McpAgentEnabled("docs", "claude") {
		t.Fatal("claude binding should override to disabled")
	}
}

func TestToggleMcpAgent(t *testing.T) {
	cfg := &RootConfig{McpServers: map[string]*McpServerConfig{
		"docs": {Command: "npx", Enabled: true},
	}}

	cfg.ToggleMcpAgent("docs", "grok", false)
	if cfg.McpAgentEnabled("docs", "grok") {
		t.Fatal("grok should be disabled after toggle")
	}
	cfg.ToggleMcpAgent("docs", "grok", true)
	if !cfg.McpAgentEnabled("docs", "grok") {
		t.Fatal("grok should be enabled after toggle")
	}
	// codex still inherits definition.
	if !cfg.McpAgentEnabled("docs", "codex") {
		t.Fatal("codex should still inherit enabled definition")
	}
}

func TestMcpCanonicalAgent(t *testing.T) {
	if got := McpCanonicalAgent("Grok-Build"); got != "grok" {
		t.Fatalf("grok-build alias failed: %q", got)
	}
	if got := McpCanonicalAgent(" Codex "); got != "codex" {
		t.Fatalf("codex normalization failed: %q", got)
	}
}

func TestMcpEffectiveServerAppliesOverrides(t *testing.T) {
	cfg := &RootConfig{McpServers: map[string]*McpServerConfig{
		"docs": {
			Command: "base-cmd",
			Args:    []string{"base-arg"},
			Env:     map[string]string{"A": "1", "B": "2"},
			Enabled: true,
		},
	}}
	cfg.SetMcpBinding("docs", "claude", &McpAgentBinding{
		Command: "claude-cmd",
		Env:     map[string]string{"B": "override"},
	})

	eff := cfg.McpEffectiveServer("docs", "claude")
	if eff.Command != "claude-cmd" {
		t.Fatalf("command override failed: %q", eff.Command)
	}
	if len(eff.Args) != 0 {
		t.Fatalf("command override should reset args, got %v", eff.Args)
	}
	if eff.Env["A"] != "1" || eff.Env["B"] != "override" {
		t.Fatalf("env merge failed: %v", eff.Env)
	}

	// codex view unaffected.
	effCodex := cfg.McpEffectiveServer("docs", "codex")
	if effCodex.Command != "base-cmd" {
		t.Fatalf("codex view should use definition command, got %q", effCodex.Command)
	}
}

func TestMcpServersForAgent(t *testing.T) {
	cfg := &RootConfig{McpServers: map[string]*McpServerConfig{
		"a": {Command: "a", Enabled: true},
		"b": {Command: "b", Enabled: false},
	}}
	on := true
	cfg.McpServers["b"].Agents = map[string]*McpAgentBinding{
		"grok": {Agent: "grok", Enabled: &on},
	}

	codexServers := cfg.McpServersForAgent("codex")
	if _, ok := codexServers["a"]; !ok {
		t.Fatal("a should be enabled for codex")
	}
	if _, ok := codexServers["b"]; ok {
		t.Fatal("b should be disabled for codex")
	}

	grokServers := cfg.McpServersForAgent("grok")
	if _, ok := grokServers["b"]; !ok {
		t.Fatal("b should be enabled for grok via binding")
	}
}

func TestNormalizeMigratesLegacyCodexFieldsToBinding(t *testing.T) {
	timeout := 20
	cfg := &RootConfig{McpServers: map[string]*McpServerConfig{
		"legacy": {
			Command:        "cmd",
			Enabled:        true,
			StartupTimeout: &timeout,
			EnabledTools:   []string{"a", "b"},
		},
	}}
	Normalize(cfg)

	binding := cfg.McpBinding("legacy", "codex")
	if binding == nil {
		t.Fatal("expected codex binding to exist after normalize")
	}
	if binding.StartupTimeout == nil || *binding.StartupTimeout != 20 {
		t.Fatalf("startup timeout not migrated: %#v", binding.StartupTimeout)
	}
	if len(binding.EnabledTools) != 2 {
		t.Fatalf("enabled_tools not migrated: %v", binding.EnabledTools)
	}
	// Legacy storage fields preserved for older builds.
	if cfg.McpServers["legacy"].StartupTimeout == nil {
		t.Fatal("legacy storage field must be preserved")
	}
}

func TestLegacyConfigRoundTripKeepsGlobalEnabled(t *testing.T) {
	// An old-format config (no agents key) loads, stays enabled for all
	// agents, and saves back without data loss.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, ".spark", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{
		"version": 1,
		"profiles": {"default": {}},
		"mcp_servers": {
			"old": {"command": "legacy-cmd", "args": ["--x"], "enabled": true}
		}
	}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, agent := range McpAgents() {
		if !cfg.McpAgentEnabled("old", agent) {
			t.Fatalf("legacy server should be enabled for %s", agent)
		}
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, _ := os.ReadFile(path)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	servers := raw["mcp_servers"].(map[string]any)
	old := servers["old"].(map[string]any)
	if old["command"] != "legacy-cmd" || old["enabled"] != true {
		t.Fatalf("legacy fields lost on save: %v", old)
	}
}

func TestCountEnabledMcpServersWithBindings(t *testing.T) {
	on := true
	servers := map[string]*McpServerConfig{
		"def": {Command: "a", Enabled: true},
		"binding-only": {
			Command: "b",
			Enabled: false,
			Agents:  map[string]*McpAgentBinding{"grok": {Agent: "grok", Enabled: &on}},
		},
		"off": {Command: "c", Enabled: false},
	}
	if got := CountEnabledMcpServers(servers); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}
