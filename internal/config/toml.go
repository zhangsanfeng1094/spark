package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type codexConfigMap map[string]any

func DefaultCodexConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

func LoadCodexMcpServers(path string) (map[string]*McpServerConfig, error) {
	root, err := loadCodexConfigMap(path)
	if err != nil {
		return nil, err
	}

	rawServers, _ := root["mcp_servers"].(map[string]any)
	if rawServers == nil {
		return map[string]*McpServerConfig{}, nil
	}

	out := make(map[string]*McpServerConfig, len(rawServers))
	for name, raw := range rawServers {
		serverMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		cfg, err := decodeServerMap(serverMap)
		if err != nil {
			return nil, fmt.Errorf("decode mcp server %q: %w", name, err)
		}
		out[McpServerName(name)] = cfg
	}
	return out, nil
}

func SaveCodexMcpServers(path string, servers map[string]*McpServerConfig) error {
	root, err := loadCodexConfigMap(path)
	if err != nil {
		return err
	}

	encodedServers := make(map[string]any, len(servers))
	for name, server := range servers {
		if server == nil || !server.Enabled {
			continue
		}
		encodedServers[McpServerName(name)] = encodeServerMap(server)
	}
	root["mcp_servers"] = encodedServers

	if path == "" {
		path, err = DefaultCodexConfigPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(root); err != nil {
		return err
	}
	return writeWithBackup(path, buf.Bytes())
}

func loadCodexConfigMap(path string) (codexConfigMap, error) {
	if path == "" {
		var err error
		path, err = DefaultCodexConfigPath()
		if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return codexConfigMap{}, nil
		}
		return nil, err
	}

	root := codexConfigMap{}
	if _, err := toml.Decode(string(data), &root); err != nil {
		return nil, fmt.Errorf("parse codex config: %w", err)
	}
	return root, nil
}

func decodeServerMap(serverMap map[string]any) (*McpServerConfig, error) {
	var cfg McpServerConfig
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(serverMap); err != nil {
		return nil, err
	}
	if _, err := toml.Decode(buf.String(), &cfg); err != nil {
		return nil, err
	}
	if _, ok := serverMap["enabled"]; !ok {
		cfg.Enabled = true
	}
	return &cfg, nil
}

func encodeServerMap(server *McpServerConfig) map[string]any {
	out := map[string]any{}
	if server.Command != "" {
		out["command"] = server.Command
	}
	if len(server.Args) > 0 {
		out["args"] = server.Args
	}
	if len(server.Env) > 0 {
		out["env"] = server.Env
	}
	if server.URL != "" {
		out["url"] = server.URL
	}
	if server.Required {
		out["required"] = server.Required
	}
	if server.DisabledReason != "" {
		out["disabled_reason"] = server.DisabledReason
	}
	if server.StartupTimeout != nil {
		out["startup_timeout_sec"] = *server.StartupTimeout
	}
	if server.ToolTimeout != nil {
		out["tool_timeout_sec"] = *server.ToolTimeout
	}
	if len(server.EnabledTools) > 0 {
		out["enabled_tools"] = server.EnabledTools
	}
	if len(server.DisabledTools) > 0 {
		out["disabled_tools"] = server.DisabledTools
	}
	if len(server.Scopes) > 0 {
		out["scopes"] = server.Scopes
	}
	if server.OAuthResource != nil && *server.OAuthResource != "" {
		out["oauth_resource"] = *server.OAuthResource
	}
	if len(server.Tools) > 0 {
		out["tools"] = server.Tools
	}
	return out
}
