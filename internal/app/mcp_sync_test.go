package app

import (
	"strings"
	"testing"

	"spark/internal/config"
)

func TestMergeImportedMcpServersPreservesExistingOnConflict(t *testing.T) {
	cfg := &config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"docs": {Command: "uvx", Args: []string{"docs-local"}, Enabled: true},
		},
	}
	imported := map[string]*config.McpServerConfig{
		"docs":   {Command: "npx", Args: []string{"docs-codex"}, Enabled: true},
		"github": {URL: "https://example.com/mcp", Enabled: true},
	}

	result := mergeImportedMcpServers(cfg, imported)

	if result.Added != 1 {
		t.Fatalf("expected 1 added server, got %d", result.Added)
	}
	if result.Skipped != 1 {
		t.Fatalf("expected 1 skipped server, got %d", result.Skipped)
	}
	if got := cfg.McpServers["docs"].Command; got != "uvx" {
		t.Fatalf("expected existing server to be preserved, got command %q", got)
	}
	if cfg.McpServers["github"] == nil {
		t.Fatal("expected github server to be imported")
	}
}

func TestFormatMcpImportSummaryIncludesConflictCount(t *testing.T) {
	summary := formatMcpImportSummary("Claude", mcpImportResult{Added: 2, Skipped: 1})
	if !strings.Contains(summary, "Imported 2 MCP server(s) from Claude.") {
		t.Fatalf("expected imported count in summary, got %q", summary)
	}
	if !strings.Contains(summary, "Skipped 1 existing server(s).") {
		t.Fatalf("expected skipped count in summary, got %q", summary)
	}
}

func TestCanonicalMcpTransferPeer(t *testing.T) {
	tests := map[string]string{
		"codex":  "codex",
		"Codex":  "codex",
		"claude": "claude",
		"Claude": "claude",
		"other":  "",
	}
	for input, want := range tests {
		if got := canonicalMcpTransferPeer(input); got != want {
			t.Fatalf("canonicalMcpTransferPeer(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCountEnabledMcpServers(t *testing.T) {
	servers := map[string]*config.McpServerConfig{
		"docs":   {Command: "npx", Enabled: true},
		"github": {URL: "https://example.com/mcp", Enabled: false},
		"nil":    nil,
	}

	if got := config.CountEnabledMcpServers(servers); got != 1 {
		t.Fatalf("CountEnabledMcpServers() = %d, want 1", got)
	}
}
