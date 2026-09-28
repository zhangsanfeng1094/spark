package app

import (
	"fmt"
	"sort"
	"strings"

	"spark/internal/config"
)

type mcpImportResult struct {
	Added   int
	Skipped int
}

func importMcpFromCodex(dryRun bool) (string, error) {
	return importMcpFromPeer("codex", dryRun)
}

func importMcpFromClaude(dryRun bool) (string, error) {
	return importMcpFromPeer("claude", dryRun)
}

func syncMcpToCodex(dryRun bool) (string, error) {
	return exportMcpToPeer("codex", dryRun)
}

func syncMcpToClaude(dryRun bool) (string, error) {
	return exportMcpToPeer("claude", dryRun)
}

func importMcpFromPeer(peer string, dryRun bool) (string, error) {
	peer = canonicalMcpTransferPeer(peer)
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	servers, label, err := loadMcpServersFromPeer(peer)
	if err != nil {
		return "", err
	}
	if len(servers) == 0 {
		return fmt.Sprintf("No MCP servers found in %s config.", label), nil
	}
	if dryRun {
		return describeMcpServers("Import from "+label, servers), nil
	}
	result := mergeImportedMcpServers(cfg, servers)
	if err := config.Save(cfg); err != nil {
		return "", err
	}
	return formatMcpImportSummary(label, result), nil
}

func exportMcpToPeer(peer string, dryRun bool) (string, error) {
	peer = canonicalMcpTransferPeer(peer)
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	label := transferPeerLabel(peer)
	// Exports are per-agent: the target peer receives the servers whose
	// binding for that agent is enabled.
	servers := cfg.McpServersForAgent(peer)
	if dryRun {
		return describeMcpServers("Export to "+label, servers), nil
	}
	if err := saveMcpServersToPeer(peer, servers); err != nil {
		return "", err
	}
	return fmt.Sprintf("Exported %d MCP server(s) enabled for %s to %s config.", len(servers), peer, label), nil
}

func mergeImportedMcpServers(cfg *config.RootConfig, servers map[string]*config.McpServerConfig) mcpImportResult {
	result := cfg.ImportMcpServers(servers)
	return mcpImportResult{
		Added:   result.Added,
		Skipped: result.Skipped,
	}
}

func formatMcpImportSummary(source string, result mcpImportResult) string {
	parts := []string{fmt.Sprintf("Imported %d MCP server(s) from %s.", result.Added, source)}
	if result.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("Skipped %d existing server(s).", result.Skipped))
	}
	return strings.Join(parts, " ")
}

func canonicalMcpTransferPeer(peer string) string {
	switch strings.ToLower(strings.TrimSpace(peer)) {
	case "codex":
		return "codex"
	case "claude":
		return "claude"
	default:
		return ""
	}
}

func transferPeerLabel(peer string) string {
	switch canonicalMcpTransferPeer(peer) {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude"
	default:
		return ""
	}
}

func loadMcpServersFromPeer(peer string) (map[string]*config.McpServerConfig, string, error) {
	switch canonicalMcpTransferPeer(peer) {
	case "codex":
		servers, err := config.LoadCodexMcpServers("")
		return servers, "Codex", err
	case "claude":
		servers, err := config.LoadClaudeUserMcpServers("")
		return servers, "Claude", err
	default:
		return nil, "", fmt.Errorf("unsupported import source: %s", peer)
	}
}

func saveMcpServersToPeer(peer string, servers map[string]*config.McpServerConfig) error {
	switch canonicalMcpTransferPeer(peer) {
	case "codex":
		return config.SaveCodexMcpServers("", servers)
	case "claude":
		return config.SaveClaudeUserMcpServers("", servers)
	default:
		return fmt.Errorf("unsupported export target: %s", peer)
	}
}

func describeMcpServers(title string, servers map[string]*config.McpServerConfig) string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	agents := config.McpAgents()
	lines := []string{title + ":"}
	for _, name := range names {
		server := servers[name]
		transport := strings.TrimSpace(server.Command)
		if transport == "" {
			transport = strings.TrimSpace(server.URL)
		}
		state := "disabled"
		if config.CountEnabledMcpServers(map[string]*config.McpServerConfig{name: server}) > 0 {
			state = "enabled"
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", name, state, transport))
		// Per-agent binding summary (definition default shown as base).
		var cells []string
		for _, agent := range agents {
			enabled := server.Enabled
			if binding := server.Agents[agent]; binding != nil && binding.Enabled != nil {
				enabled = *binding.Enabled
			}
			cells = append(cells, fmt.Sprintf("%s=%t", agent, enabled))
		}
		lines = append(lines, "    "+strings.Join(cells, " "))
	}
	return strings.Join(lines, "\n")
}
