package config

import (
	"fmt"
	"sort"
	"strings"
)

// McpServerConfig represents a single MCP server configuration.
type McpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
	URL     string            `json:"url,omitempty"` // For HTTP transport

	// Additional fields from Codex MCP config
	Required       bool                      `json:"required,omitempty"`
	DisabledReason string                    `json:"disabled_reason,omitempty"`
	StartupTimeout *int                      `json:"startup_timeout_sec,omitempty"`
	ToolTimeout    *int                      `json:"tool_timeout_sec,omitempty"`
	EnabledTools   []string                  `json:"enabled_tools,omitempty"`
	DisabledTools  []string                  `json:"disabled_tools,omitempty"`
	Scopes         []string                  `json:"scopes,omitempty"`
	OAuthResource  *string                   `json:"oauth_resource,omitempty"`
	Tools          map[string]map[string]any `json:"tools,omitempty"`
}

type McpImportResult struct {
	Added   int
	Skipped int
}

// McpServerName returns a normalized server name.
func McpServerName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// GetMcpServer retrieves an MCP server configuration by name.
func (c *RootConfig) GetMcpServer(name string) *McpServerConfig {
	if c.McpServers == nil {
		return nil
	}
	return c.McpServers[McpServerName(name)]
}

// SetMcpServer adds or updates an MCP server configuration.
func (c *RootConfig) SetMcpServer(name string, cfg *McpServerConfig) {
	if c.McpServers == nil {
		c.McpServers = make(map[string]*McpServerConfig)
	}
	c.McpServers[McpServerName(name)] = cfg
}

// RemoveMcpServer removes an MCP server configuration.
func (c *RootConfig) RemoveMcpServer(name string) bool {
	name = McpServerName(name)
	if c.McpServers == nil {
		return false
	}
	if _, exists := c.McpServers[name]; !exists {
		return false
	}
	delete(c.McpServers, name)
	return true
}

// ListMcpServers returns a sorted list of MCP server names.
func (c *RootConfig) ListMcpServers() []string {
	if c.McpServers == nil {
		return nil
	}
	names := make([]string, 0, len(c.McpServers))
	for name := range c.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CountEnabledMcpServers returns the number of enabled MCP servers.
func CountEnabledMcpServers(servers map[string]*McpServerConfig) int {
	count := 0
	for _, server := range servers {
		if server != nil && server.Enabled {
			count++
		}
	}
	return count
}

// EnableMcpServer enables an MCP server.
func (c *RootConfig) EnableMcpServer(name string) error {
	cfg := c.GetMcpServer(name)
	if cfg == nil {
		return fmt.Errorf("MCP server not found: %s", name)
	}
	cfg.Enabled = true
	cfg.DisabledReason = ""
	return nil
}

// DisableMcpServer disables an MCP server with an optional reason.
func (c *RootConfig) DisableMcpServer(name, reason string) error {
	cfg := c.GetMcpServer(name)
	if cfg == nil {
		return fmt.Errorf("MCP server not found: %s", name)
	}
	cfg.Enabled = false
	if reason != "" {
		cfg.DisabledReason = reason
	}
	return nil
}

// MergeMcpServers merges MCP servers from another source into this config.
// Existing servers with the same name will be overwritten.
func (c *RootConfig) MergeMcpServers(servers map[string]*McpServerConfig) {
	if c.McpServers == nil {
		c.McpServers = make(map[string]*McpServerConfig)
	}
	for name, cfg := range servers {
		c.McpServers[McpServerName(name)] = cfg
	}
}

// ImportMcpServers adds missing MCP servers without overwriting existing entries.
func (c *RootConfig) ImportMcpServers(servers map[string]*McpServerConfig) McpImportResult {
	if c.McpServers == nil {
		c.McpServers = make(map[string]*McpServerConfig)
	}
	result := McpImportResult{}
	for name, cfg := range servers {
		if cfg == nil {
			continue
		}
		normalized := McpServerName(name)
		if _, exists := c.McpServers[normalized]; exists {
			result.Skipped++
			continue
		}
		c.McpServers[normalized] = cfg
		result.Added++
	}
	return result
}

// NewStdioMcpServer creates a new MCP server with stdio transport.
func NewStdioMcpServer(command string, args []string) *McpServerConfig {
	return &McpServerConfig{
		Command: command,
		Args:    args,
		Enabled: true,
	}
}

// NewHttpMcpServer creates a new MCP server with HTTP transport.
func NewHttpMcpServer(url string) *McpServerConfig {
	return &McpServerConfig{
		URL:     url,
		Enabled: true,
	}
}
