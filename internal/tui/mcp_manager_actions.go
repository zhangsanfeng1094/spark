package tui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
)

// toggleCurrentAgentBinding toggles the agent under the matrix cursor for the
// selected server.
func (m *mcpManagerModel) toggleCurrentAgentBinding() tea.Cmd {
	name := m.currentName()
	if name == "" {
		return nil
	}
	agents := m.agents()
	agent := agents[clampIndex(m.matrixCursor, len(agents))]
	return m.toggleAgentBinding(name, agent)
}

func (m *mcpManagerModel) toggleAgentBinding(name, agent string) tea.Cmd {
	if m.cfg.GetMcpServer(name) == nil {
		return nil
	}
	enabled := !m.cfg.McpAgentEnabled(name, agent)
	m.cfg.ToggleMcpAgent(name, agent, enabled)
	status := successStatus(fmt.Sprintf("%s: %s %s for %s.", name, m.agentLabel(agent), ternary(enabled, "enabled", "disabled"), name))
	m.status = status
	m.refreshNames()
	return m.saveCfgCmd(status)
}

// openBindingEditor builds the per-agent binding form.
func (m *mcpManagerModel) openBindingEditor(agent string) tea.Cmd {
	name := m.currentName()
	if name == "" {
		return nil
	}
	m.bindAgent = config.McpCanonicalAgent(agent)
	m.bindFields = newBindingFields(m.cfg.McpBinding(name, m.bindAgent))
	m.bindCursor = 0
	m.page = mcpPageBinding
	m.status = fmt.Sprintf("Editing %s binding for %s. Ctrl+S save.", m.agentLabel(m.bindAgent), name)
	return nil
}

// newBindingFields builds the binding form. The enabled field defaults to the
// current effective state (binding or inherited definition default).
func newBindingFields(binding *config.McpAgentBinding) []mcpFormField {
	if binding == nil {
		binding = &config.McpAgentBinding{}
	}
	env := formatEnvLines(binding.Env)
	fields := []mcpFormField{
		{Key: "enabled", Label: "Enabled", Value: "inherit", Kind: mcpFieldKindSelect, Options: []string{"inherit", "true", "false"}},
		{Key: "command", Label: "Command", Value: binding.Command, Placeholder: "inherit from definition", Kind: mcpFieldKindInput},
		{Key: "args", Label: "Arguments", Value: strings.Join(binding.Args, "\n"), Placeholder: "one per line", Kind: mcpFieldKindTextarea},
		{Key: "url", Label: "URL", Value: binding.URL, Placeholder: "inherit from definition", Kind: mcpFieldKindInput},
		{Key: "env", Label: "Environment", Value: env, Placeholder: "KEY=value lines", Kind: mcpFieldKindTextarea},
		{Key: "startup_timeout", Label: "Startup timeout", Value: intPtrString(binding.StartupTimeout), Placeholder: "seconds (Codex)", Kind: mcpFieldKindInput},
		{Key: "tool_timeout", Label: "Tool timeout", Value: intPtrString(binding.ToolTimeout), Placeholder: "seconds (Codex)", Kind: mcpFieldKindInput},
		{Key: "enabled_tools", Label: "Enabled tools", Value: strings.Join(binding.EnabledTools, "\n"), Placeholder: "one per line (Codex)", Kind: mcpFieldKindTextarea},
		{Key: "disabled_tools", Label: "Disabled tools", Value: strings.Join(binding.DisabledTools, "\n"), Placeholder: "one per line (Codex)", Kind: mcpFieldKindTextarea},
	}
	for i := range fields {
		f := &fields[i]
		if f.Kind == mcpFieldKindInput || f.Kind == mcpFieldKindTextarea {
			f.Input = newFieldTextInput(f.Placeholder, f.Value, false, false, 30)
		}
	}
	if binding.Enabled != nil && *binding.Enabled {
		fields[bindFieldEnabled].Value = "true"
	} else if binding.Enabled != nil {
		fields[bindFieldEnabled].Value = "false"
	}
	return fields
}

func intPtrString(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

// saveBinding writes the binding form back to config.
func (m *mcpManagerModel) saveBinding() tea.Cmd {
	name := m.currentName()
	if name == "" || m.bindAgent == "" {
		return nil
	}

	binding := &config.McpAgentBinding{}
	switch m.bindFields[bindFieldEnabled].Value {
	case "true", "false":
		v := m.bindFields[bindFieldEnabled].Value == "true"
		binding.Enabled = &v
	}

	binding.Command = strings.TrimSpace(m.bindFields[bindFieldCommand].Value)
	binding.Args = parseLineList(m.bindFields[bindFieldArgs].Value)
	binding.URL = strings.TrimSpace(m.bindFields[bindFieldURL].Value)

	env, err := parseEnvLines(m.bindFields[bindFieldEnv].Value)
	if err != nil {
		m.status = errorStatus("Invalid environment format: " + err.Error())
		return nil
	}
	binding.Env = env

	if v := parseOptionalInt(m.bindFields[bindFieldStartup].Value); v != nil {
		binding.StartupTimeout = v
	}
	if v := parseOptionalInt(m.bindFields[bindFieldTool].Value); v != nil {
		binding.ToolTimeout = v
	}
	binding.EnabledTools = parseLineList(m.bindFields[bindFieldEnableT].Value)
	binding.DisabledTools = parseLineList(m.bindFields[bindFieldDisablT].Value)

	m.cfg.SetMcpBinding(name, m.bindAgent, binding)
	m.page = mcpPageDetail
	m.refreshNames()
	status := successStatus(fmt.Sprintf("Saved %s binding for %s.", m.agentLabel(m.bindAgent), name))
	m.status = status
	return m.saveCfgCmd(status)
}

func parseOptionalInt(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var v int
	if _, err := fmt.Sscanf(raw, "%d", &v); err != nil {
		return nil
	}
	return &v
}

// deleteCurrent removes the server definition and all bindings.
func (m *mcpManagerModel) deleteCurrent() tea.Cmd {
	name := m.currentName()
	if name == "" {
		return nil
	}
	cfgCopy := *m.cfg
	cfgCopy.McpServers = make(map[string]*config.McpServerConfig, len(m.cfg.McpServers))
	for k, v := range m.cfg.McpServers {
		cfgCopy.McpServers[k] = config.CloneMcpServerConfig(v)
	}
	delete(cfgCopy.McpServers, name)
	delete(m.tests, name)

	m.cfg = &cfgCopy
	m.modalKind = mcpModalNone
	m.page = mcpPageList
	m.refreshNames()
	status := successStatus(fmt.Sprintf("Deleted server %q.", name))
	m.status = status
	return func() tea.Msg {
		if err := config.Save(&cfgCopy); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: status, Cfg: &cfgCopy}
	}
}

// activateAddChoice dispatches the Add flow choice.
func (m *mcpManagerModel) activateAddChoice() tea.Cmd {
	switch m.addChoice {
	case 0: // Paste config
		m.modalKind = mcpModalPaste
		m.pasteBuffer = ""
		m.pasteCursor = 0
		m.status = "Paste config, then Ctrl+S to import. Esc cancels."
		return nil
	case 1: // Local command
		return m.createSkeletonServer("stdio")
	case 2: // Remote URL
		return m.createSkeletonServer("http")
	case 3: // Import existing
		m.modalKind = mcpModalImportPeer
		m.importPeer = 0
		return nil
	}
	return nil
}

// createSkeletonServer adds a stub server and opens the raw editor for it.
func (m *mcpManagerModel) createSkeletonServer(transport string) tea.Cmd {
	name := m.nextServerName(ternary(transport == "stdio", "local", "remote"))
	server := &config.McpServerConfig{Enabled: false}
	if transport == "stdio" {
		server.Command = "npx"
	} else {
		server.URL = "https://mcp.example.com/mcp"
	}
	m.cfg.SetMcpServer(name, server)
	m.modalKind = mcpModalNone
	m.refreshNames()
	m.selectByName(name)
	m.status = successStatus(fmt.Sprintf("Created draft %q — edit raw config, then enable per agent.", name))
	cmd := m.saveCfgCmd(m.status)
	_ = cmd
	return tea.Batch(cmd, func() tea.Msg { return nil })
}

func (m *mcpManagerModel) nextServerName(prefix string) string {
	base := fmt.Sprintf("%s-mcp", prefix)
	name := base
	for i := 2; ; i++ {
		if m.cfg.GetMcpServer(name) == nil {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// savePasteImport imports servers from the paste buffer / file path.
func (m *mcpManagerModel) savePasteImport() tea.Cmd {
	servers, err := parseMultipleMCPServersRaw(m.pasteBuffer, "imported-server")
	if err != nil {
		m.status = errorStatus("Import failed: " + err.Error())
		return nil
	}
	m.modalKind = mcpModalNone
	buffer := m.pasteBuffer
	m.pasteBuffer = ""
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		_ = buffer
		result := cfg.ImportMcpServers(servers)
		if err := config.Save(cfg); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		status := successStatus(fmt.Sprintf("Imported %d MCP server(s).", result.Added))
		if result.Skipped > 0 {
			status += fmt.Sprintf(" Skipped %d existing.", result.Skipped)
		}
		return mcpSaveFinishedMsg{Status: status, Cfg: cfg}
	}
}

// importFromPeer pulls servers from an existing agent config on disk.
func (m *mcpManagerModel) importFromPeer(peer string) tea.Cmd {
	m.modalKind = mcpModalNone
	m.status = "Importing from " + peer + "..."
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		var servers map[string]*config.McpServerConfig
		switch peer {
		case "codex":
			servers, err = config.LoadCodexMcpServers("")
		case "claude":
			servers, err = config.LoadClaudeUserMcpServers("")
		default:
			err = fmt.Errorf("unsupported import source: %s", peer)
		}
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		result := cfg.ImportMcpServers(servers)
		if err := config.Save(cfg); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		label := strings.ToUpper(peer[:1]) + peer[1:]
		status := successStatus(fmt.Sprintf("Imported %d server(s) from %s.", result.Added, label))
		if result.Skipped > 0 {
			status += fmt.Sprintf(" %d skipped.", result.Skipped)
		}
		return mcpSaveFinishedMsg{Status: status, Cfg: cfg}
	}
}

// openExternalEditor opens $EDITOR on a temp file for paste/definition editing.
func (m *mcpManagerModel) openExternalEditor(target, initialContent, ext string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if strings.TrimSpace(editor) == "" {
		editor = os.Getenv("VISUAL")
	}
	if strings.TrimSpace(editor) == "" {
		if p, err := exec.LookPath("vim"); err == nil && p != "" {
			editor = "vim"
		} else if p, err := exec.LookPath("nano"); err == nil && p != "" {
			editor = "nano"
		} else {
			editor = "vi"
		}
	}

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("spark-mcp-*.%s", ext))
	if err != nil {
		m.status = errorStatus("failed to create temporary file: " + err.Error())
		return nil
	}
	tmpPath := tmpFile.Name()
	if strings.TrimSpace(initialContent) != "" {
		_, _ = tmpFile.WriteString(initialContent)
	}
	_ = tmpFile.Close()

	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"vim"}
	}
	c := exec.Command(parts[0], append(parts[1:], tmpPath)...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return mcpExternalEditorFinishedMsg{Target: target, Path: tmpPath, Err: err}
	})
}

// handleExternalEditorFinished imports the edited content.
func (m *mcpManagerModel) handleExternalEditorFinished(msg mcpExternalEditorFinishedMsg) tea.Cmd {
	defer func() {
		if msg.Path != "" {
			_ = os.Remove(msg.Path)
		}
	}()

	if msg.Err != nil {
		m.status = errorStatus("Editor error: " + msg.Err.Error())
		return nil
	}
	data, err := os.ReadFile(msg.Path)
	if err != nil {
		m.status = errorStatus("Failed to read edited file: " + err.Error())
		return nil
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		m.status = infoStatus("Editor closed without saving.")
		return nil
	}

	if msg.Target == "paste" {
		m.pasteBuffer = content
		m.pasteCursor = len([]rune(content))
		m.modalKind = mcpModalPaste
		return m.savePasteImport()
	}

	// Definition editing for the current server.
	server, name, err := parseEditedMCPServerRaw(content, m.currentName())
	if err != nil {
		m.status = errorStatus("Config error: " + err.Error())
		return nil
	}
	origName := m.currentName()
	cfgCopy := *m.cfg
	cfgCopy.McpServers = make(map[string]*config.McpServerConfig, len(m.cfg.McpServers))
	for k, v := range m.cfg.McpServers {
		cfgCopy.McpServers[k] = config.CloneMcpServerConfig(v)
	}
	if origName != "" && origName != name {
		if prev := cfgCopy.McpServers[origName]; prev != nil {
			// Preserve bindings across a rename.
			server.Agents = prev.Agents
			delete(cfgCopy.McpServers, origName)
		}
	}
	cfgCopy.SetMcpServer(name, server)
	config.Normalize(&cfgCopy)

	m.cfg = &cfgCopy
	m.refreshNames()
	m.selectByName(name)
	status := successStatus(fmt.Sprintf("Saved definition for %q.", name))
	m.status = status
	return func() tea.Msg {
		if err := config.Save(&cfgCopy); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: status, Cfg: &cfgCopy}
	}
}

// sortedAgentBindingNames lists agents with explicit bindings (diagnostics).
func sortedAgentBindingNames(server *config.McpServerConfig) []string {
	if server == nil || len(server.Agents) == 0 {
		return nil
	}
	names := make([]string, 0, len(server.Agents))
	for agent := range server.Agents {
		names = append(names, agent)
	}
	sort.Strings(names)
	return names
}
