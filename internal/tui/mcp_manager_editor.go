package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"spark/internal/config"
)

func parseEditedMCPServerRaw(raw string, fallbackName string) (*config.McpServerConfig, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", fmt.Errorf("raw editor is empty")
	}

	if strings.HasPrefix(raw, "{") {
		type namedServer map[string]*config.McpServerConfig
		var wrapped namedServer
		if err := json.Unmarshal([]byte(raw), &wrapped); err == nil && len(wrapped) == 1 {
			for key, value := range wrapped {
				return finalizeParsedRawServer(key, value)
			}
		}
		var direct config.McpServerConfig
		if err := json.Unmarshal([]byte(raw), &direct); err != nil {
			return nil, "", err
		}
		return finalizeParsedRawServer(fallbackName, &direct)
	}

	return parseMCPServerYAML(raw, fallbackName)
}

func parseMCPServerYAML(raw string, fallbackName string) (*config.McpServerConfig, string, error) {
	lines := strings.Split(raw, "\n")
	name := config.McpServerName(strings.TrimSpace(fallbackName))
	server := &config.McpServerConfig{}
	mode := ""

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(strings.TrimSpace(line), ":") && !strings.Contains(strings.TrimSpace(line), ": ") {
			candidate := strings.TrimSuffix(strings.TrimSpace(line), ":")
			if candidate != "args" && candidate != "env" {
				name = config.McpServerName(candidate)
				continue
			}
		}

		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "args:":
			mode = "args"
			server.Args = nil
			continue
		case trimmed == "env:":
			mode = "env"
			if server.Env == nil {
				server.Env = map[string]string{}
			}
			continue
		case strings.HasPrefix(trimmed, "- "):
			if mode != "args" {
				return nil, "", fmt.Errorf("unexpected list item: %q", trimmed)
			}
			server.Args = append(server.Args, unquoteYAML(strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))))
			continue
		case strings.HasPrefix(line, "  ") && mode == "env" && strings.Contains(trimmed, ":"):
			key, value, _ := strings.Cut(trimmed, ":")
			server.Env[strings.TrimSpace(key)] = unquoteYAML(strings.TrimSpace(value))
			continue
		}

		mode = ""
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, "", fmt.Errorf("invalid YAML line: %q", trimmed)
		}
		key = strings.TrimSpace(key)
		value = unquoteYAML(strings.TrimSpace(value))
		switch key {
		case "command":
			server.Command = value
		case "url":
			server.URL = value
		case "enabled":
			server.Enabled = parseBoolLoose(value)
		case "disabled_reason":
			server.DisabledReason = value
		default:
			return nil, "", fmt.Errorf("unsupported YAML field: %s", key)
		}
	}

	return finalizeParsedRawServer(name, server)
}

func unquoteYAML(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			if unquoted, err := strconvUnquoteCompatible(v); err == nil {
				return unquoted
			}
			return v[1 : len(v)-1]
		}
	}
	return v
}

func strconvUnquoteCompatible(v string) (string, error) {
	if strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		return v[1 : len(v)-1], nil
	}
	return strconv.Unquote(v)
}

func finalizeParsedRawServer(name string, server *config.McpServerConfig) (*config.McpServerConfig, string, error) {
	name = config.McpServerName(strings.TrimSpace(name))
	if name == "" {
		return nil, "", fmt.Errorf("server name is required in raw mode")
	}
	if server == nil {
		return nil, "", fmt.Errorf("raw config did not contain a server object")
	}
	if detail, _ := validateMCPServerConfig(server); detail != "" {
		return nil, "", fmt.Errorf("%s", detail)
	}
	return server, name, nil
}

func envPairs(env map[string]string) []string {
	pairs := os.Environ()
	for key, value := range env {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}
