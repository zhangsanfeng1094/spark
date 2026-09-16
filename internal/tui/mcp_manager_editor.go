package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
	"spark/internal/config"
)

// parseMultipleMCPServersRaw handles TOML, JSON, YAML, and local file paths.
func parseMultipleMCPServersRaw(raw string, fallbackName string) (map[string]*config.McpServerConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("import content is empty")
	}

	// 0. If raw is a local file path, read its content
	if content, err := tryReadFileContent(raw); err == nil && len(strings.TrimSpace(content)) > 0 {
		raw = strings.TrimSpace(content)
	}

	// 1. Try TOML decoding
	var tomlMap map[string]any
	if _, err := toml.Decode(raw, &tomlMap); err == nil && len(tomlMap) > 0 {
		var serversMap any
		if v, ok := tomlMap["mcp_servers"]; ok {
			serversMap = v
		} else if v, ok := tomlMap["mcpServers"]; ok {
			serversMap = v
		}

		if serversMap != nil {
			if sMap, ok := serversMap.(map[string]any); ok {
				if parsed, err := convertGenericMapToMCPServers(sMap); err == nil && len(parsed) > 0 {
					return parsed, nil
				}
			}
		}

		if _, hasCmd := tomlMap["command"]; hasCmd || hasKey(tomlMap, "url") {
			if srv, err := decodeMapToSingleMCPServer(tomlMap, fallbackName); err == nil {
				return map[string]*config.McpServerConfig{fallbackName: srv}, nil
			}
		}

		if parsed, err := convertGenericMapToMCPServers(tomlMap); err == nil && len(parsed) > 0 {
			return parsed, nil
		}
	}

	// 2. Try JSON / YAML decoding
	var topLevel map[string]any
	if err := yaml.Unmarshal([]byte(raw), &topLevel); err == nil && len(topLevel) > 0 {
		var serversMap any
		if v, ok := topLevel["mcpServers"]; ok {
			serversMap = v
		} else if v, ok := topLevel["mcp_servers"]; ok {
			serversMap = v
		}

		if serversMap != nil {
			if sMap, ok := serversMap.(map[string]any); ok {
				return convertGenericMapToMCPServers(sMap)
			}
		}

		if _, hasCmd := topLevel["command"]; hasCmd || hasKey(topLevel, "url") {
			srv, name, err := parseEditedMCPServerRaw(raw, fallbackName)
			if err != nil {
				return nil, err
			}
			return map[string]*config.McpServerConfig{name: srv}, nil
		}

		results, err := convertGenericMapToMCPServers(topLevel)
		if err == nil && len(results) > 0 {
			return results, nil
		}
	}

	// 3. Fallback to single server parser
	srv, name, err := parseEditedMCPServerRaw(raw, fallbackName)
	if err != nil {
		return nil, err
	}
	return map[string]*config.McpServerConfig{name: srv}, nil
}

func decodeMapToSingleMCPServer(m map[string]any, fallbackName string) (*config.McpServerConfig, error) {
	rawBytes, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	srv, _, err := parseEditedMCPServerRaw(string(rawBytes), fallbackName)
	return srv, err
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func convertGenericMapToMCPServers(input map[string]any) (map[string]*config.McpServerConfig, error) {
	out := make(map[string]*config.McpServerConfig)
	for name, val := range input {
		cleanName := strings.TrimSpace(name)
		if cleanName == "" {
			continue
		}
		rawBytes, err := yaml.Marshal(val)
		if err != nil {
			continue
		}
		srv, parsedName, err := parseEditedMCPServerRaw(string(rawBytes), cleanName)
		if err != nil {
			continue
		}
		if parsedName != "" {
			cleanName = parsedName
		}
		out[cleanName] = srv
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid MCP servers found in import content")
	}
	return out, nil
}

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
	server := &config.McpServerConfig{Enabled: true}
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
		return nil, "", fmt.Errorf("server name is required")
	}
	if server == nil {
		return nil, "", fmt.Errorf("raw config did not contain a server object")
	}
	if detail, _ := validateMCPServerConfig(server); detail != "" {
		return nil, "", fmt.Errorf("%s", detail)
	}
	return server, name, nil
}

func tryReadFileContent(pathCandidate string) (string, error) {
	trimmed := strings.TrimSpace(pathCandidate)
	if strings.Contains(trimmed, "\n") {
		return "", fmt.Errorf("not a file path")
	}
	if strings.HasPrefix(trimmed, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))
		}
	}
	info, err := os.Stat(trimmed)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("file not found")
	}
	data, err := os.ReadFile(trimmed)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func marshalNamedMCPServerYAML(name string, server *config.McpServerConfig) string {
	if server == nil {
		return fmt.Sprintf("%s:\n  enabled: true\n", name)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s:\n", name))
	if server.URL != "" {
		b.WriteString(fmt.Sprintf("  url: %q\n", server.URL))
	}
	if server.Command != "" {
		b.WriteString(fmt.Sprintf("  command: %q\n", server.Command))
	}
	if len(server.Args) > 0 {
		b.WriteString("  args:\n")
		for _, arg := range server.Args {
			b.WriteString(fmt.Sprintf("    - %q\n", arg))
		}
	}
	if len(server.Env) > 0 {
		b.WriteString("  env:\n")
		for k, v := range server.Env {
			b.WriteString(fmt.Sprintf("    %s: %q\n", k, v))
		}
	}
	b.WriteString(fmt.Sprintf("  enabled: %t\n", server.Enabled))
	if server.DisabledReason != "" {
		b.WriteString(fmt.Sprintf("  disabled_reason: %q\n", server.DisabledReason))
	}
	return b.String()
}
