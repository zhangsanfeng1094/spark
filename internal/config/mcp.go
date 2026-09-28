package config

import (
	"fmt"
	"sort"
	"strings"
)

// McpAgentBinding is an agent-specific binding layered on top of a shared MCP
// server definition. The definition owns transport-level facts; the binding
// decides whether a given agent gets the server and may override how that
// agent starts it.
type McpAgentBinding struct {
	Agent   string `json:"agent,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"` // nil = inherit definition default

	// Transport overrides. Empty values inherit the definition.
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// Codex-specific advanced settings (binding-level). Values set here win
	// over the legacy definition-level fields below.
	StartupTimeout *int                      `json:"startup_timeout_sec,omitempty"`
	ToolTimeout    *int                      `json:"tool_timeout_sec,omitempty"`
	EnabledTools   []string                  `json:"enabled_tools,omitempty"`
	DisabledTools  []string                  `json:"disabled_tools,omitempty"`
	Scopes         []string                  `json:"scopes,omitempty"`
	OAuthResource  *string                   `json:"oauth_resource,omitempty"`
	Tools          map[string]map[string]any `json:"tools,omitempty"`
}

// McpServerConfig represents a single MCP server definition (shared across
// agents). Transport fields are the public definition; agent bindings live in
// Agents keyed by canonical agent name (codex/claude/one/grok/agy).
//
// The Codex-specific fields below are legacy storage kept for backward
// compatibility: they are treated as codex-binding values and must not gain
// new global semantics. New writes should target the codex binding.
type McpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`       // legacy global default inherited by agents without an explicit binding
	URL     string            `json:"url,omitempty"` // For HTTP transport

	// Agent bindings keyed by canonical agent name.
	Agents map[string]*McpAgentBinding `json:"agents,omitempty"`

	// Legacy Codex-specific fields (storage compat; semantics live on the codex binding).
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

// mcpAgentsFirstStage lists the agents supported by MCP bindings in phase one,
// in display order.
var mcpAgentsFirstStage = []string{"codex", "claude", "one", "grok", "agy"}

// McpAgents returns the canonical agent names supported by MCP bindings.
func McpAgents() []string {
	return append([]string(nil), mcpAgentsFirstStage...)
}

// McpCanonicalAgent normalizes an agent name to its canonical binding key
// (e.g. grok-build → grok). Unknown names are lowercased as-is so future
// agents keep working without config changes.
func McpCanonicalAgent(agent string) string {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "grok-build", "grok":
		return "grok"
	default:
		return strings.ToLower(strings.TrimSpace(agent))
	}
}

// normalizeMcpServer normalizes one server definition and performs the
// legacy→binding migration in memory (never drops data):
//
//  1. Agent binding keys are canonicalized.
//  2. Legacy Codex-specific fields (startup_timeout, tool_timeout, enabled/
//     disabled tools, scopes, oauth_resource, tools) are copied into the codex
//     binding's advanced section when that binding does not set them. The
//     legacy storage fields stay untouched so older Spark builds keep working;
//     the product semantics (UI + injection) only read the binding from now on.
func normalizeMcpServer(server *McpServerConfig) {
	if server == nil {
		return
	}
	if server.Agents == nil && hasLegacyCodexAdvanced(server) {
		server.Agents = map[string]*McpAgentBinding{}
	}
	if server.Agents != nil {
		canonical := make(map[string]*McpAgentBinding, len(server.Agents))
		for agent, binding := range server.Agents {
			if binding == nil {
				continue
			}
			canonical[McpCanonicalAgent(agent)] = binding
		}
		server.Agents = canonical
	}
	if server.Agents == nil {
		return
	}
	codex, ok := server.Agents["codex"]
	if !ok || codex == nil {
		if !hasLegacyCodexAdvanced(server) {
			return
		}
		codex = &McpAgentBinding{}
		server.Agents["codex"] = codex
	}
	if codex.StartupTimeout == nil {
		codex.StartupTimeout = server.StartupTimeout
	}
	if codex.ToolTimeout == nil {
		codex.ToolTimeout = server.ToolTimeout
	}
	if len(codex.EnabledTools) == 0 {
		codex.EnabledTools = server.EnabledTools
	}
	if len(codex.DisabledTools) == 0 {
		codex.DisabledTools = server.DisabledTools
	}
	if len(codex.Scopes) == 0 {
		codex.Scopes = server.Scopes
	}
	if codex.OAuthResource == nil {
		codex.OAuthResource = server.OAuthResource
	}
	if len(codex.Tools) == 0 {
		codex.Tools = server.Tools
	}
}

// hasLegacyCodexAdvanced reports whether the definition carries legacy
// Codex-specific advanced fields that should be visible on the codex binding.
func hasLegacyCodexAdvanced(server *McpServerConfig) bool {
	return server.StartupTimeout != nil ||
		server.ToolTimeout != nil ||
		len(server.EnabledTools) > 0 ||
		len(server.DisabledTools) > 0 ||
		len(server.Scopes) > 0 ||
		server.OAuthResource != nil ||
		len(server.Tools) > 0
}

// McpServerName returns a normalized server name.
func McpServerName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// GetMcpServer retrieves an MCP server configuration by name.
func (c *RootConfig) GetMcpServer(name string) *McpServerConfig {
	if c == nil || c.McpServers == nil {
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

// McpBinding returns the agent binding for a server, or nil when the agent
// inherits the definition default.
func (c *RootConfig) McpBinding(name, agent string) *McpAgentBinding {
	server := c.GetMcpServer(name)
	if server == nil {
		return nil
	}
	return server.Agents[McpCanonicalAgent(agent)]
}

// SetMcpBinding writes an agent binding for a server.
func (c *RootConfig) SetMcpBinding(name, agent string, binding *McpAgentBinding) {
	server := c.GetMcpServer(name)
	if server == nil {
		return
	}
	if server.Agents == nil {
		server.Agents = make(map[string]*McpAgentBinding)
	}
	agent = McpCanonicalAgent(agent)
	if binding == nil {
		delete(server.Agents, agent)
		return
	}
	binding.Agent = agent
	server.Agents[agent] = binding
}

// RemoveMcpBinding drops an agent binding so the agent inherits the
// definition default again.
func (c *RootConfig) RemoveMcpBinding(name, agent string) {
	c.SetMcpBinding(name, agent, nil)
}

// McpAgentEnabled reports whether a server is enabled for one agent. A binding
// with an explicit Enabled wins; otherwise the legacy definition-level Enabled
// is inherited (backward compatibility for pre-binding configs).
func (c *RootConfig) McpAgentEnabled(name, agent string) bool {
	server := c.GetMcpServer(name)
	if server == nil {
		return false
	}
	if binding := server.Agents[McpCanonicalAgent(agent)]; binding != nil && binding.Enabled != nil {
		return *binding.Enabled
	}
	return server.Enabled
}

// ToggleMcpAgent sets the explicit enabled state for one agent binding.
func (c *RootConfig) ToggleMcpAgent(name, agent string, enabled bool) {
	server := c.GetMcpServer(name)
	if server == nil {
		return
	}
	agent = McpCanonicalAgent(agent)
	if server.Agents == nil {
		server.Agents = make(map[string]*McpAgentBinding)
	}
	binding := server.Agents[agent]
	if binding == nil {
		binding = &McpAgentBinding{}
		server.Agents[agent] = binding
	}
	value := enabled
	binding.Enabled = &value
}

// McpBindingAgents returns the sorted list of agents that have an explicit
// binding for a server.
func (c *RootConfig) McpBindingAgents(name string) []string {
	server := c.GetMcpServer(name)
	if server == nil || len(server.Agents) == 0 {
		return nil
	}
	agents := make([]string, 0, len(server.Agents))
	for agent := range server.Agents {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	return agents
}

// McpEffectiveServer builds the per-agent view of a server: a copy of the
// definition with the agent binding's overrides applied and Enabled resolved
// for that agent. Returns nil when the server does not exist.
func (c *RootConfig) McpEffectiveServer(name, agent string) *McpServerConfig {
	server := c.GetMcpServer(name)
	if server == nil {
		return nil
	}
	eff := CloneMcpServerConfig(server)
	eff.Enabled = c.McpAgentEnabled(name, agent)
	binding := c.McpBinding(name, agent)
	if binding == nil {
		return eff
	}
	if binding.Command != "" {
		eff.Command = binding.Command
		eff.Args = append([]string(nil), binding.Args...)
	} else if len(binding.Args) > 0 {
		eff.Args = append([]string(nil), binding.Args...)
	}
	if binding.URL != "" {
		eff.URL = binding.URL
	}
	if len(binding.Env) > 0 {
		merged := make(map[string]string, len(server.Env)+len(binding.Env))
		for k, v := range server.Env {
			merged[k] = v
		}
		for k, v := range binding.Env {
			merged[k] = v
		}
		eff.Env = merged
	}
	if binding.StartupTimeout != nil {
		eff.StartupTimeout = binding.StartupTimeout
	}
	if binding.ToolTimeout != nil {
		eff.ToolTimeout = binding.ToolTimeout
	}
	if len(binding.EnabledTools) > 0 {
		eff.EnabledTools = binding.EnabledTools
	}
	if len(binding.DisabledTools) > 0 {
		eff.DisabledTools = binding.DisabledTools
	}
	if len(binding.Scopes) > 0 {
		eff.Scopes = binding.Scopes
	}
	if binding.OAuthResource != nil {
		eff.OAuthResource = binding.OAuthResource
	}
	if len(binding.Tools) > 0 {
		eff.Tools = binding.Tools
	}
	return eff
}

// McpServersForAgent returns effective server configs (binding overrides
// applied) for every server enabled for the given agent, keyed by name.
func (c *RootConfig) McpServersForAgent(agent string) map[string]*McpServerConfig {
	out := make(map[string]*McpServerConfig)
	if c == nil {
		return out
	}
	for name := range c.McpServers {
		eff := c.McpEffectiveServer(name, agent)
		if eff != nil && eff.Enabled {
			out[name] = eff
		}
	}
	return out
}

// McpEnabledAgentCount returns how many first-stage agents have this server
// enabled. Used for dashboard summaries.
func (c *RootConfig) McpEnabledAgentCount(name string) int {
	count := 0
	for _, agent := range mcpAgentsFirstStage {
		if c.McpAgentEnabled(name, agent) {
			count++
		}
	}
	return count
}

// CountEnabledMcpServers returns the number of servers enabled for at least
// one first-stage agent.
func CountEnabledMcpServers(servers map[string]*McpServerConfig) int {
	count := 0
	for _, server := range servers {
		if server == nil {
			continue
		}
		enabled := server.Enabled
		if !enabled {
			for _, agent := range mcpAgentsFirstStage {
				if binding := server.Agents[agent]; binding != nil && binding.Enabled != nil && *binding.Enabled {
					enabled = true
					break
				}
			}
		}
		if enabled {
			count++
		}
	}
	return count
}

// EnableMcpServer enables an MCP server (definition-level default).
func (c *RootConfig) EnableMcpServer(name string) error {
	cfg := c.GetMcpServer(name)
	if cfg == nil {
		return fmt.Errorf("MCP server not found: %s", name)
	}
	cfg.Enabled = true
	cfg.DisabledReason = ""
	return nil
}

// DisableMcpServer disables an MCP server (definition-level default) with an
// optional reason.
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

// CloneMcpServerConfig deep-copies a server definition including bindings.
func CloneMcpServerConfig(server *McpServerConfig) *McpServerConfig {
	if server == nil {
		return nil
	}
	clone := *server
	clone.Args = append([]string(nil), server.Args...)
	if server.Env != nil {
		clone.Env = make(map[string]string, len(server.Env))
		for k, v := range server.Env {
			clone.Env[k] = v
		}
	}
	if server.Agents != nil {
		clone.Agents = make(map[string]*McpAgentBinding, len(server.Agents))
		for agent, binding := range server.Agents {
			clone.Agents[agent] = CloneMcpAgentBinding(binding)
		}
	}
	clone.EnabledTools = append([]string(nil), server.EnabledTools...)
	clone.DisabledTools = append([]string(nil), server.DisabledTools...)
	clone.Scopes = append([]string(nil), server.Scopes...)
	if server.Tools != nil {
		clone.Tools = make(map[string]map[string]any, len(server.Tools))
		for tool, cfg := range server.Tools {
			if cfg == nil {
				clone.Tools[tool] = nil
				continue
			}
			copied := make(map[string]any, len(cfg))
			for k, v := range cfg {
				copied[k] = v
			}
			clone.Tools[tool] = copied
		}
	}
	return &clone
}

// CloneMcpAgentBinding deep-copies an agent binding.
func CloneMcpAgentBinding(binding *McpAgentBinding) *McpAgentBinding {
	if binding == nil {
		return nil
	}
	clone := *binding
	clone.Args = append([]string(nil), binding.Args...)
	if binding.Env != nil {
		clone.Env = make(map[string]string, len(binding.Env))
		for k, v := range binding.Env {
			clone.Env[k] = v
		}
	}
	clone.EnabledTools = append([]string(nil), binding.EnabledTools...)
	clone.DisabledTools = append([]string(nil), binding.DisabledTools...)
	clone.Scopes = append([]string(nil), binding.Scopes...)
	if binding.Tools != nil {
		clone.Tools = make(map[string]map[string]any, len(binding.Tools))
		for tool, cfg := range binding.Tools {
			if cfg == nil {
				clone.Tools[tool] = nil
				continue
			}
			copied := make(map[string]any, len(cfg))
			for k, v := range cfg {
				copied[k] = v
			}
			clone.Tools[tool] = copied
		}
	}
	return &clone
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
