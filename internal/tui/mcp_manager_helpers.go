package tui

import (
	"fmt"
	"sort"
	"strings"

	"spark/internal/config"
)

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
		if strings.Contains(strings.ToLower(server.URL), "/sse") {
			return "sse"
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

func validateMCPServerConfig(server *config.McpServerConfig) (string, string) {
	if server == nil {
		return "server config cannot be empty", "Provide command or URL"
	}
	if strings.TrimSpace(server.Command) == "" && strings.TrimSpace(server.URL) == "" {
		return "either command or url is required", "Specify a command for local execution or a URL for HTTP"
	}
	return "", ""
}

func parseBoolLoose(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "true" || v == "1" || v == "yes" || v == "on"
}
