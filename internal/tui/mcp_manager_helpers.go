package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"spark/internal/config"
)

func sortMCPNames(names []string, cfg *config.RootConfig, probes map[string]*mcpProbeResult) {
	sort.SliceStable(names, func(i, j int) bool {
		si := cfg.GetMcpServer(names[i])
		sj := cfg.GetMcpServer(names[j])
		pi := probes[names[i]]
		pj := probes[names[j]]
		return serverPriority(names[i], si, pi) < serverPriority(names[j], sj, pj)
	})
}

func serverPriority(name string, server *config.McpServerConfig, probe *mcpProbeResult) int {
	if server == nil || !server.Enabled {
		return 4 // Disabled last
	}
	summary := summarizeMCPStatus(name, server, probe)
	switch summary.Kind {
	case mcpStatusBroken:
		return 0 // Error first
	case mcpStatusUnknown:
		return 1 // Unknown second
	case mcpStatusConfigured, mcpStatusReachable:
		return 2 // Healthy third
	default:
		return 3
	}
}

func isHTTPMCPServer(server *config.McpServerConfig) bool {
	if server == nil {
		return false
	}
	return strings.TrimSpace(server.URL) != ""
}

func transportLabel(server *config.McpServerConfig) string {
	if server == nil {
		return "stdio"
	}
	if isHTTPMCPServer(server) {
		if strings.HasPrefix(strings.ToLower(server.URL), "http://") || strings.HasPrefix(strings.ToLower(server.URL), "https://") {
			if strings.Contains(strings.ToLower(server.URL), "/sse") {
				return "sse"
			}
			return "http/sse"
		}
		return "http"
	}
	return "stdio"
}

func currentTransport(server *config.McpServerConfig) string {
	if server == nil {
		return "stdio"
	}
	if isHTTPMCPServer(server) {
		if strings.Contains(strings.ToLower(server.URL), "/sse") {
			return "sse"
		}
		return "http"
	}
	return "stdio"
}

func formatEnvLines(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", k, env[k]))
	}
	return strings.Join(lines, "\n")
}

func parseEnvLines(raw string) (map[string]string, error) {
	lines := strings.Split(raw, "\n")
	env := make(map[string]string)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid environment variable line: %q", line)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			return nil, fmt.Errorf("empty environment variable key")
		}
		env[k] = v
	}
	return env, nil
}

func parseLineList(raw string) []string {
	lines := strings.Split(raw, "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func summarizeMCPStatus(name string, server *config.McpServerConfig, probe *mcpProbeResult) mcpStatusSummary {
	if server == nil {
		return mcpStatusSummary{
			Kind:        mcpStatusBroken,
			Badge:       "✕",
			Headline:    "missing config",
			Detail:      "server not defined in configuration",
			Suggestions: []string{"Add server configuration or remove entry"},
		}
	}
	if server.Command == "" && server.URL == "" {
		return mcpStatusSummary{
			Kind:        mcpStatusBroken,
			Badge:       "✕",
			Headline:    "invalid config",
			Detail:      "missing transport: command or url is required",
			Suggestions: []string{"Specify command for stdio or url for HTTP/SSE"},
		}
	}
	if !server.Enabled {
		return mcpStatusSummary{
			Kind:        mcpStatusUnknown,
			Badge:       "○",
			Headline:    "disabled",
			Detail:      ternary(server.DisabledReason != "", server.DisabledReason, "manually disabled"),
			Suggestions: []string{"Press Space to enable server"},
		}
	}
	if probe == nil {
		return mcpStatusSummary{
			Kind:        mcpStatusUnknown,
			Badge:       "?",
			Headline:    "not probed yet",
			Detail:      "probe has not been run in this session",
			Suggestions: []string{"Press P to test connection"},
		}
	}
	if probe.Err != "" {
		return mcpStatusSummary{
			Kind:        mcpStatusBroken,
			Badge:       "✕",
			Headline:    "probe failed",
			Detail:      probe.Err,
			Suggestions: diagnoseMCPFailure(server, probe),
		}
	}
	return mcpStatusSummary{
		Kind:        mcpStatusReachable,
		Badge:       "✓",
		Headline:    fmt.Sprintf("ok (%d tools)", probe.ToolsCount),
		Detail:      fmt.Sprintf("latency: %s", probe.Latency.Round(time.Millisecond)),
		Suggestions: []string{"Server is healthy and ready"},
	}
}

func diagnoseMCPFailure(server *config.McpServerConfig, probe *mcpProbeResult) []string {
	if probe == nil || probe.Err == "" {
		return nil
	}
	errStr := strings.ToLower(probe.Err)
	var sugs []string
	if strings.Contains(errStr, "executable file not found") || strings.Contains(errStr, "command not found") {
		sugs = append(sugs, fmt.Sprintf("Executable %q not found in PATH", server.Command))
	}
	if strings.Contains(errStr, "connection refused") {
		sugs = append(sugs, "Remote endpoint refused connection; ensure the server is running")
	}
	if strings.Contains(errStr, "timeout") {
		sugs = append(sugs, "Server took too long to respond to initialize")
	}
	if len(sugs) == 0 {
		sugs = append(sugs, "Check server logs or command line arguments")
	}
	return sugs
}

func statusIcon(status mcpStatusSummary) string {
	switch status.Kind {
	case mcpStatusReachable, mcpStatusConfigured:
		return "✓"
	case mcpStatusBroken:
		return "✕"
	default:
		return status.Badge
	}
}

func validateMCPServerConfig(server *config.McpServerConfig) (string, string) {
	if server == nil {
		return "server config cannot be empty", "Provide command or URL"
	}
	if strings.TrimSpace(server.Command) == "" && strings.TrimSpace(server.URL) == "" {
		return "either command or url is required", "Specify a command for local execution or a URL for HTTP"
	}
	return "", ""
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
	return &clone
}

func envPairs(env map[string]string) []string {
	var pairs []string
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	return pairs
}

func parseBoolLoose(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "true" || v == "1" || v == "yes" || v == "on"
}
