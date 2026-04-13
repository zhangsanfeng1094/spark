package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type claudeConfigMap map[string]any

func DefaultClaudeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

func LoadClaudeUserMcpServers(path string) (map[string]*McpServerConfig, error) {
	root, err := loadClaudeConfigMap(path)
	if err != nil {
		return nil, err
	}

	rawServers, _ := root["mcpServers"].(map[string]any)
	if rawServers == nil {
		return map[string]*McpServerConfig{}, nil
	}

	out := make(map[string]*McpServerConfig, len(rawServers))
	for name, raw := range rawServers {
		serverMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		cfg := decodeClaudeServerMap(serverMap)
		if cfg == nil {
			continue
		}
		out[McpServerName(name)] = cfg
	}
	return out, nil
}

func SaveClaudeUserMcpServers(path string, servers map[string]*McpServerConfig) error {
	root, err := loadClaudeConfigMap(path)
	if err != nil {
		return err
	}

	encodedServers := make(map[string]any, len(servers))
	for name, server := range servers {
		if server == nil || !server.Enabled {
			continue
		}
		encodedServers[McpServerName(name)] = encodeClaudeServerMap(server)
	}
	root["mcpServers"] = encodedServers

	if path == "" {
		path, err = DefaultClaudeConfigPath()
		if err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return writeWithBackup(path, data)
}

func loadClaudeConfigMap(path string) (claudeConfigMap, error) {
	if path == "" {
		var err error
		path, err = DefaultClaudeConfigPath()
		if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return claudeConfigMap{}, nil
		}
		return nil, err
	}

	root := claudeConfigMap{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse claude config: %w", err)
	}
	return root, nil
}

func decodeClaudeServerMap(serverMap map[string]any) *McpServerConfig {
	cfg := &McpServerConfig{Enabled: true}
	if command, _ := serverMap["command"].(string); command != "" {
		cfg.Command = command
	}
	if url, _ := serverMap["url"].(string); url != "" {
		cfg.URL = url
	}
	if serverURL, _ := serverMap["serverUrl"].(string); cfg.URL == "" && serverURL != "" {
		cfg.URL = serverURL
	}
	if args, ok := serverMap["args"].([]any); ok && len(args) > 0 {
		cfg.Args = make([]string, 0, len(args))
		for _, arg := range args {
			if s, ok := arg.(string); ok && s != "" {
				cfg.Args = append(cfg.Args, s)
			}
		}
	}
	if env, ok := serverMap["env"].(map[string]any); ok && len(env) > 0 {
		cfg.Env = make(map[string]string, len(env))
		for key, value := range env {
			if s, ok := value.(string); ok {
				cfg.Env[key] = s
			}
		}
	}
	return cfg
}

func encodeClaudeServerMap(server *McpServerConfig) map[string]any {
	out := map[string]any{}
	if server == nil {
		return out
	}
	if server.URL != "" {
		out["type"] = "http"
		out["url"] = server.URL
		return out
	}
	out["type"] = "stdio"
	if server.Command != "" {
		out["command"] = server.Command
	}
	if len(server.Args) > 0 {
		out["args"] = server.Args
	}
	if len(server.Env) > 0 {
		out["env"] = server.Env
	}
	return out
}
