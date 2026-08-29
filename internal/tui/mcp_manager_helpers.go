package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"spark/internal/config"
)

func (m *mcpManagerModel) refreshNames() {
	m.names = m.cfg.ListMcpServers()
	if len(m.names) == 0 {
		m.selected = 0
		return
	}
	if m.selected >= len(m.names) {
		m.selected = len(m.names) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func (m *mcpManagerModel) currentName() string {
	if m.selected < 0 || m.selected >= len(m.names) {
		return ""
	}
	return m.names[m.selected]
}

func (m *mcpManagerModel) selectByName(name string) {
	for i, candidate := range m.names {
		if candidate == name {
			m.selected = i
			return
		}
	}
}

func (m *mcpManagerModel) currentStatus() mcpStatusSummary {
	return summarizeMCPStatus(m.currentName(), m.cfg.GetMcpServer(m.currentName()), m.probes[m.currentName()])
}

func (m *mcpManagerModel) browseActions() []mcpActionItem {
	actions := []mcpActionItem{}
	if m.currentName() != "" {
		toggleLabel := "Disable"
		toggleDesc := "Temporarily disable this server"
		if server := m.cfg.GetMcpServer(m.currentName()); server != nil && !server.Enabled {
			toggleLabel = "Enable"
			toggleDesc = "Re-enable this server"
		}
		actions = append(actions,
			mcpActionItem{Key: "probe", Label: "Probe", Description: "Run initialize and tools/list"},
			mcpActionItem{Key: "edit", Label: "Edit", Description: "Open the focused server form"},
			mcpActionItem{Key: "toggle", Label: toggleLabel, Description: toggleDesc},
			mcpActionItem{Key: "delete", Label: "Delete", Description: "Remove this server from config"},
		)
	}
	actions = append(actions,
		mcpActionItem{Key: "transfer", Label: "Transfer", Description: "Import or export MCP servers"},
	)
	if len(actions) == 0 {
		return actions
	}
	m.actionIndex = clampIndex(m.actionIndex, len(actions))
	return actions
}

func clampIndex(value, length int) int {
	if length <= 0 {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value >= length {
		return length - 1
	}
	return value
}

func summarizeMCPStatus(name string, server *config.McpServerConfig, probe *mcpProbeResult) mcpStatusSummary {
	if server == nil {
		return mcpStatusSummary{
			Kind:     mcpStatusBroken,
			Badge:    "✕",
			Headline: "broken",
			Detail:   "server not found in current config",
		}
	}

	if detail, suggestions := validateMCPServerConfig(server); detail != "" {
		return mcpStatusSummary{
			Kind:        mcpStatusBroken,
			Badge:       "✕",
			Headline:    "broken",
			Detail:      detail,
			Suggestions: suggestions,
		}
	}

	if probe == nil {
		return mcpStatusSummary{
			Kind:        mcpStatusUnknown,
			Badge:       "?",
			Headline:    "unknown",
			Detail:      "not probed yet",
			Suggestions: []string{"Press P to run spawn/connect → initialize → tools/list and refresh diagnostics."},
		}
	}

	if probe.Err != "" {
		return mcpStatusSummary{
			Kind:        mcpStatusBroken,
			Badge:       "✕",
			Headline:    "broken",
			Detail:      fmt.Sprintf("%s failed: %s", probe.Stage, probe.Err),
			Suggestions: diagnoseMCPFailure(server, probe),
		}
	}

	if isHTTPMCPServer(server) {
		return mcpStatusSummary{
			Kind:        mcpStatusReachable,
			Badge:       "●",
			Headline:    "reachable",
			Detail:      fmt.Sprintf("connected successfully and listed %d tool(s)", probe.ToolsCount),
			Suggestions: []string{"No fix needed. Probe only checks handshake reachability and tools/list, then closes immediately."},
		}
	}

	return mcpStatusSummary{
		Kind:        mcpStatusConfigured,
		Badge:       "✓",
		Headline:    "configured",
		Detail:      fmt.Sprintf("stdio handshake completed and listed %d tool(s)", probe.ToolsCount),
		Suggestions: []string{"No fix needed. Spark only probes the server and kills it immediately after tools/list."},
	}
}

func validateMCPServerConfig(server *config.McpServerConfig) (string, []string) {
	hasCommand := strings.TrimSpace(server.Command) != ""
	hasURL := strings.TrimSpace(server.URL) != ""
	switch {
	case hasCommand && hasURL:
		return "invalid config: both command and url are set", []string{
			"Keep exactly one transport. Use command+args for stdio or url for HTTP/SSE.",
		}
	case !hasCommand && !hasURL:
		return "invalid config: missing transport", []string{
			"Set a command for stdio servers or a url for HTTP/SSE servers.",
		}
	case hasCommand && strings.TrimSpace(server.Command) == "":
		return "invalid config: command is empty", []string{
			"Set the executable name or absolute path in command.",
		}
	}
	return "", nil
}

func diagnoseMCPFailure(server *config.McpServerConfig, probe *mcpProbeResult) []string {
	var suggestions []string
	errText := strings.ToLower(probe.Err)
	if strings.Contains(errText, "executable file not found") || strings.Contains(errText, "file not found") {
		suggestions = append(suggestions, "Install the server binary and verify the command is available in PATH.")
	}
	if strings.Contains(errText, "connection refused") {
		suggestions = append(suggestions, "Start the remote MCP service and verify the URL/port are correct.")
	}
	if strings.Contains(errText, "deadline exceeded") || strings.Contains(errText, "timeout") {
		suggestions = append(suggestions, "Increase startup time on the server side or verify the endpoint responds to initialize quickly.")
	}
	if strings.Contains(errText, "401") || strings.Contains(errText, "403") || strings.Contains(errText, "unauthorized") {
		suggestions = append(suggestions, "Check auth headers, tokens, or reverse proxy rules for this MCP endpoint.")
	}
	if isHTTPMCPServer(server) {
		suggestions = append(suggestions, "Verify the URL points to the MCP endpoint, not a human-facing docs page.")
	} else {
		suggestions = append(suggestions, "Run the configured command manually to confirm it starts and speaks MCP over stdio.")
	}
	if len(suggestions) == 0 {
		suggestions = append(suggestions, "Inspect the raw error, then verify transport settings, credentials, and server startup behavior.")
	}
	return uniqueStrings(suggestions)
}

func transportLabel(server *config.McpServerConfig) string {
	if isHTTPMCPServer(server) {
		return "http/sse"
	}
	return "stdio"
}

func isHTTPMCPServer(server *config.McpServerConfig) bool {
	return server != nil && strings.TrimSpace(server.URL) != ""
}

func cloneMCPServerConfig(server *config.McpServerConfig) *config.McpServerConfig {
	if server == nil {
		return nil
	}
	clone := *server
	if len(server.Args) > 0 {
		clone.Args = append([]string{}, server.Args...)
	}
	if len(server.Env) > 0 {
		clone.Env = make(map[string]string, len(server.Env))
		for k, v := range server.Env {
			clone.Env[k] = v
		}
	}
	if len(server.EnabledTools) > 0 {
		clone.EnabledTools = append([]string{}, server.EnabledTools...)
	}
	if len(server.DisabledTools) > 0 {
		clone.DisabledTools = append([]string{}, server.DisabledTools...)
	}
	if len(server.Scopes) > 0 {
		clone.Scopes = append([]string{}, server.Scopes...)
	}
	if len(server.Tools) > 0 {
		clone.Tools = make(map[string]map[string]any, len(server.Tools))
		for k, v := range server.Tools {
			if v == nil {
				clone.Tools[k] = nil
				continue
			}
			nested := make(map[string]any, len(v))
			for nestedKey, nestedValue := range v {
				nested[nestedKey] = nestedValue
			}
			clone.Tools[k] = nested
		}
	}
	if server.OAuthResource != nil {
		value := *server.OAuthResource
		clone.OAuthResource = &value
	}
	return &clone
}

func successStatus(message string) string {
	return "✓ " + strings.TrimSpace(message)
}

func errorStatus(message string) string {
	return "✗ " + strings.TrimSpace(message)
}

func infoStatus(message string) string {
	return "… " + strings.TrimSpace(message)
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func parseBoolLoose(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseLineList(v string) []string {
	lines := strings.Split(v, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseEnvLines(v string) (map[string]string, error) {
	lines := strings.Split(v, "\n")
	out := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid env line: %q", line)
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out, nil
}

func formatEnvLines(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+env[key])
	}
	return strings.Join(lines, "\n")
}

func currentTransport(server *config.McpServerConfig) string {
	if strings.TrimSpace(server.URL) != "" {
		return "http"
	}
	return "stdio"
}

func marshalMCPServerYAML(server *config.McpServerConfig) string {
	return marshalNamedMCPServerYAML("", server)
}

func marshalNamedMCPServerYAML(name string, server *config.McpServerConfig) string {
	lines := []string{}
	if name != "" {
		lines = append(lines, name+":")
	}
	prefix := ""
	if name != "" {
		prefix = "  "
	}
	if server.Command != "" {
		lines = append(lines, prefix+fmt.Sprintf("command: %s", quoteYAML(server.Command)))
	}
	if len(server.Args) > 0 {
		lines = append(lines, prefix+"args:")
		for _, arg := range server.Args {
			lines = append(lines, prefix+"  - "+quoteYAML(arg))
		}
	}
	if server.URL != "" {
		lines = append(lines, prefix+fmt.Sprintf("url: %s", quoteYAML(server.URL)))
	}
	lines = append(lines, prefix+fmt.Sprintf("enabled: %t", server.Enabled))
	if len(server.Env) > 0 {
		lines = append(lines, prefix+"env:")
		keys := make([]string, 0, len(server.Env))
		for key := range server.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, prefix+fmt.Sprintf("  %s: %s", key, quoteYAML(server.Env[key])))
		}
	}
	if server.DisabledReason != "" {
		lines = append(lines, prefix+fmt.Sprintf("disabled_reason: %s", quoteYAML(server.DisabledReason)))
	}
	return strings.Join(lines, "\n")
}

func quoteYAML(v string) string {
	data, _ := json.Marshal(v)
	return string(data)
}

