package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
)

// loadCurrentDraft populates draftFields from the currently selected server.
func saveAndMaybeProbeMCPConfigCmd(cfg *config.RootConfig, probeName string, runProbe bool) tea.Cmd {
	return func() tea.Msg {
		if err := config.Save(cfg); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		var result *mcpProbeResult
		if runProbe && probeName != "" {
			if srv := cfg.GetMcpServer(probeName); srv != nil {
				result = probeMCPServer(probeName, cloneMCPServerConfig(srv))
			}
		}
		return mcpSaveFinishedMsg{
			Status:     successStatus(fmt.Sprintf("Saved MCP configuration for %q.", probeName)),
			Cfg:        cfg,
			ProbeName:  probeName,
			Result:     result,
			OpenResult: runProbe,
		}
	}
}
func (m *mcpManagerModel) loadCurrentDraft() {
	name := m.currentName()
	server := m.cfg.GetMcpServer(name)
	m.draftFields = newMCPFormFields(name, server)
	m.fieldCursor = make(map[int]int, len(m.draftFields))
	for i, f := range m.draftFields {
		m.fieldCursor[i] = len([]rune(f.Value))
	}
	m.updateFocus()
	m.dirty = false
}

func (m *mcpManagerModel) updateFocus() tea.Cmd {
	var cmds []tea.Cmd
	visible := m.visibleFieldIndices()
	for fIdx, actualIdx := range visible {
		f := &m.draftFields[actualIdx]
		if f.Kind == mcpFieldKindInput || f.Kind == mcpFieldKindTextarea {
			if m.focusArea == mcpFocusFields && fIdx == m.focusField {
				cmd := f.Input.Focus()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else {
				f.Input.Blur()
			}
		}
	}
	return tea.Batch(cmds...)
}

func (m *mcpManagerModel) syncInputsFromModels() {
	for i := range m.draftFields {
		f := &m.draftFields[i]
		if f.Kind == mcpFieldKindInput || f.Kind == mcpFieldKindTextarea {
			if f.Input.Focused() {
				f.Value = f.Input.Value()
			}
		}
	}
}

func newMCPFormFields(name string, server *config.McpServerConfig) []mcpFormField {
	if server == nil {
		server = &config.McpServerConfig{Enabled: true}
	}
	transport := currentTransport(server)
	enabled := "false"
	if server.Enabled {
		enabled = "true"
	}
	args := strings.Join(server.Args, "\n")
	env := formatEnvLines(server.Env)

	fields := []mcpFormField{
		{Key: "name", Label: "Name", Value: name, Placeholder: "e.g. sqlite", Kind: mcpFieldKindInput, Required: true},
		{Key: "transport", Label: "Transport", Value: transport, Kind: mcpFieldKindSelect, Options: []string{"stdio", "http", "sse"}, ReadOnly: true},
		{Key: "enabled", Label: "Config Enabled", Value: enabled, Kind: mcpFieldKindSelect, Options: []string{"true", "false"}, ReadOnly: true},
		{Key: "command", Label: "Command", Value: server.Command, Placeholder: "e.g. npx", Kind: mcpFieldKindInput},
		{Key: "args", Label: "Arguments", Value: args, Placeholder: "e.g. -y @mcp/server-sqlite /data.db", Kind: mcpFieldKindTextarea},
		{Key: "url", Label: "URL", Value: server.URL, Placeholder: "e.g. https://mcp.deepwiki.com/mcp", Kind: mcpFieldKindInput},
		{Key: "env", Label: "Environment", Value: env, Placeholder: "e.g. API_KEY=xxx", Kind: mcpFieldKindTextarea},
		{Key: "reason", Label: "Disabled Reason", Value: server.DisabledReason, Placeholder: "optional note", Kind: mcpFieldKindInput},
	}
	for i := range fields {
		f := &fields[i]
		if f.Kind == mcpFieldKindInput || f.Kind == mcpFieldKindTextarea {
			f.Input = newFieldTextInput(f.Placeholder, f.Value, false, false, 30)
		}
	}
	return fields
}

func (m *mcpManagerModel) visibleFieldIndices() []int {
	transport := "stdio"
	if len(m.draftFields) > mcpFieldKeyTransport {
		transport = strings.ToLower(strings.TrimSpace(m.draftFields[mcpFieldKeyTransport].Value))
	}
	indices := []int{mcpFieldKeyName, mcpFieldKeyTransport, mcpFieldKeyEnabled}
	if transport == "http" || transport == "sse" {
		indices = append(indices, mcpFieldKeyURL, mcpFieldKeyEnv, mcpFieldKeyReason)
	} else {
		indices = append(indices, mcpFieldKeyCommand, mcpFieldKeyArgs, mcpFieldKeyEnv, mcpFieldKeyReason)
	}
	return indices
}

func (m *mcpManagerModel) saveDraft(runProbe bool) tea.Cmd {
	m.syncInputsFromModels()
	name := strings.TrimSpace(m.draftFields[mcpFieldKeyName].Value)
	if name == "" {
		m.status = errorStatus("Server name is required")
		return nil
	}
	transport := strings.ToLower(strings.TrimSpace(m.draftFields[mcpFieldKeyTransport].Value))
	enabled := parseBoolLoose(m.draftFields[mcpFieldKeyEnabled].Value)
	server := &config.McpServerConfig{
		Enabled:        enabled,
		DisabledReason: strings.TrimSpace(m.draftFields[mcpFieldKeyReason].Value),
	}
	if transport == "stdio" {
		server.Command = strings.TrimSpace(m.draftFields[mcpFieldKeyCommand].Value)
		server.Args = parseLineList(m.draftFields[mcpFieldKeyArgs].Value)
	} else {
		server.URL = strings.TrimSpace(m.draftFields[mcpFieldKeyURL].Value)
	}
	env, err := parseEnvLines(m.draftFields[mcpFieldKeyEnv].Value)
	if err != nil {
		m.status = errorStatus("Invalid environment format: " + err.Error())
		return nil
	}
	server.Env = env
	if detail, _ := validateMCPServerConfig(server); detail != "" {
		m.status = errorStatus(detail)
		return nil
	}

	origName := m.currentName()
	cfgCopy := *m.cfg
	cfgCopy.McpServers = make(map[string]*config.McpServerConfig, len(m.cfg.McpServers))
	for k, v := range m.cfg.McpServers {
		cfgCopy.McpServers[k] = cloneMCPServerConfig(v)
	}
	if origName != "" && origName != name {
		delete(cfgCopy.McpServers, origName)
	}
	cfgCopy.SetMcpServer(name, server)
	config.Normalize(&cfgCopy)

	m.cfg = &cfgCopy
	m.dirty = false
	m.refreshNames()
	m.selectByName(name)
	m.loadCurrentDraft()

	return saveAndMaybeProbeMCPConfigCmd(&cfgCopy, name, runProbe)
}

func (m *mcpManagerModel) selectByName(name string) {
	for i, n := range m.filtered {
		if n == name {
			m.selected = i
			return
		}
	}
}

func (m *mcpManagerModel) deleteCurrent() tea.Cmd {
	name := m.currentName()
	if name == "" {
		return nil
	}
	cfgCopy := *m.cfg
	cfgCopy.McpServers = make(map[string]*config.McpServerConfig, len(m.cfg.McpServers))
	for k, v := range m.cfg.McpServers {
		cfgCopy.McpServers[k] = cloneMCPServerConfig(v)
	}
	delete(cfgCopy.McpServers, name)
	delete(m.probes, name)
	config.Normalize(&cfgCopy)

	m.cfg = &cfgCopy
	m.modalKind = mcpModalNone
	m.refreshNames()
	m.loadCurrentDraft()
	m.status = successStatus(fmt.Sprintf("Deleted server %q.", name))

	return func() tea.Msg {
		if err := config.Save(&cfgCopy); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: successStatus(fmt.Sprintf("Deleted server %q.", name)), Cfg: &cfgCopy}
	}
}

func (m *mcpManagerModel) toggleCurrentEnabled() tea.Cmd {
	name := m.currentName()
	if name == "" {
		return nil
	}
	server := m.cfg.GetMcpServer(name)
	if server == nil {
		return nil
	}
	enabled := !server.Enabled
	cfgCopy := *m.cfg
	cfgCopy.McpServers = make(map[string]*config.McpServerConfig, len(m.cfg.McpServers))
	for k, v := range m.cfg.McpServers {
		cfgCopy.McpServers[k] = cloneMCPServerConfig(v)
	}
	srvCopy := cloneMCPServerConfig(server)
	srvCopy.Enabled = enabled
	cfgCopy.SetMcpServer(name, srvCopy)

	m.cfg = &cfgCopy
	m.refreshNames()
	m.loadCurrentDraft()

	statusText := ternary(enabled, "Enabled", "Disabled")
	m.status = successStatus(fmt.Sprintf("Server %q %s.", name, statusText))

	return func() tea.Msg {
		if err := config.Save(&cfgCopy); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: successStatus(fmt.Sprintf("Server %q %s.", name, statusText)), Cfg: &cfgCopy}
	}
}

func (m *mcpManagerModel) probeCurrent(openResult bool) tea.Cmd {
	name := m.currentName()
	if name == "" {
		return nil
	}
	server := m.cfg.GetMcpServer(name)
	if server == nil {
		return nil
	}
	m.running[name] = true
	m.status = "Probing " + name + "..."
	serverCopy := cloneMCPServerConfig(server)
	return func() tea.Msg {
		return mcpProbeFinishedMsg{Name: name, Result: probeMCPServer(name, serverCopy), OpenResult: openResult}
	}
}

func (m *mcpManagerModel) probeAll() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.names))
	for _, name := range m.names {
		server := m.cfg.GetMcpServer(name)
		if server == nil {
			continue
		}
		m.running[name] = true
		serverCopy := cloneMCPServerConfig(server)
		nameCopy := name
		cmds = append(cmds, func() tea.Msg {
			return mcpProbeFinishedMsg{Name: nameCopy, Result: probeMCPServer(nameCopy, serverCopy)}
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	m.status = "Probing all MCP servers..."
	return tea.Batch(cmds...)
}

func (m *mcpManagerModel) openExternalEditorForCurrent() tea.Cmd {
	name := m.currentName()
	if name == "" {
		name = "new-server"
	}
	server := m.cfg.GetMcpServer(name)
	content := marshalNamedMCPServerYAML(name, server)
	return openExternalEditor(name, content, "yaml")
}

func (m *mcpManagerModel) openExternalEditorForImport() tea.Cmd {
	initial := m.importBuffer
	if strings.TrimSpace(initial) == "" {
		initial = "# Paste or write MCP Server config in TOML, JSON, or YAML format\n# Examples:\n# [mcp_servers.my-server]\n# command = \"npx\"\n# args = [\"-y\", \"@modelcontextprotocol/server-sqlite\"]\n\n"
	}
	return openExternalEditor("import", initial, "toml")
}

func openExternalEditor(target, initialContent, ext string) tea.Cmd {
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
		return func() tea.Msg {
			return mcpExternalEditorFinishedMsg{
				Target: target,
				Err:    fmt.Errorf("failed to create temporary file: %w", err),
			}
		}
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
	cmdName := parts[0]
	cmdArgs := append(parts[1:], tmpPath)

	c := exec.Command(cmdName, cmdArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return mcpExternalEditorFinishedMsg{
			Target: target,
			Path:   tmpPath,
			Err:    err,
		}
	})
}

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

	if msg.Target == "import" {
		servers, err := parseMultipleMCPServersRaw(content, "imported-server")
		if err != nil {
			m.importBuffer = content
			m.importCursor = len([]rune(content))
			m.status = errorStatus("Import parse error: " + err.Error())
			return nil
		}
		m.modalKind = mcpModalNone
		m.importBuffer = ""
		m.importCursor = 0
		return func() tea.Msg {
			cfg, err := config.Load()
			if err != nil {
				return mcpSaveFinishedMsg{Err: err}
			}
			result := cfg.ImportMcpServers(servers)
			if err := config.Save(cfg); err != nil {
				return mcpSaveFinishedMsg{Err: err}
			}
			status := successStatus(fmt.Sprintf("Vim import: %d server(s) added.", result.Added))
			if result.Skipped > 0 {
				status += fmt.Sprintf(" %d skipped.", result.Skipped)
			}
			return mcpSaveFinishedMsg{Status: status, Cfg: cfg}
		}
	}

	server, name, err := parseEditedMCPServerRaw(content, msg.Target)
	if err != nil {
		m.status = errorStatus("Config error: " + err.Error())
		return nil
	}

	cfgCopy := *m.cfg
	cfgCopy.McpServers = make(map[string]*config.McpServerConfig, len(m.cfg.McpServers))
	for k, v := range m.cfg.McpServers {
		cfgCopy.McpServers[k] = cloneMCPServerConfig(v)
	}
	if msg.Target != "" && msg.Target != name && msg.Target != "new-server" {
		delete(cfgCopy.McpServers, msg.Target)
	}
	cfgCopy.SetMcpServer(name, server)
	config.Normalize(&cfgCopy)

	m.cfg = &cfgCopy
	m.modalKind = mcpModalNone
	m.refreshNames()
	m.selectByName(name)
	m.loadCurrentDraft()

	return saveAndMaybeProbeMCPConfigCmd(&cfgCopy, name, false)
}

func (m *mcpManagerModel) confirmAddFromModal() {
	name := strings.TrimSpace(m.addName)
	if name == "" {
		name = fmt.Sprintf("mcp-server-%d", len(m.names)+1)
	}
	transports := []string{"stdio", "http", "sse"}
	transport := transports[clampIndex(m.addTransport, len(transports))]

	server := &config.McpServerConfig{
		Enabled: true,
	}
	if transport == "stdio" {
		server.Command = "npx"
	} else {
		server.URL = "https://mcp.example.com/mcp"
	}

	m.cfg.SetMcpServer(config.McpServerName(name), server)
	config.Normalize(m.cfg)
	m.modalKind = mcpModalNone
	m.refreshNames()
	m.selectByName(name)
	m.loadCurrentDraft()
	m.focusArea = mcpFocusFields
	m.focusField = 0
	m.status = successStatus(fmt.Sprintf("Created %s server %q. Fill fields or press [V] to open in Vim.", strings.ToUpper(transport), name))
}

func (m *mcpManagerModel) saveImportModal() tea.Cmd {
	servers, err := parseMultipleMCPServersRaw(m.importBuffer, "imported-server")
	if err != nil {
		m.status = errorStatus("Import failed: " + err.Error())
		return nil
	}
	m.modalKind = mcpModalNone
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
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

func (m *mcpManagerModel) activateTransferItem() tea.Cmd {
	if len(m.transferItems) == 0 {
		return nil
	}
	item := m.transferItems[clampIndex(m.transferIndex, len(m.transferItems))]
	m.modalKind = mcpModalNone
	switch item.Key {
	case "import_raw":
		m.modalKind = mcpModalImport
		m.importBuffer = ""
		m.importCursor = 0
		return nil
	case "import_codex":
		return m.importFromCodex()
	case "import_claude":
		return m.importFromClaude()
	case "export_codex":
		return m.syncToCodex()
	case "export_claude":
		return m.syncToClaude()
	}
	return nil
}

func (m *mcpManagerModel) importFromCodex() tea.Cmd {
	m.status = "Importing from Codex..."
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		servers, err := config.LoadCodexMcpServers("")
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		result := cfg.ImportMcpServers(servers)
		if err := config.Save(cfg); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		summary := fmt.Sprintf("Imported %d server(s) from Codex.", result.Added)
		if result.Skipped > 0 {
			summary += fmt.Sprintf(" %d skipped.", result.Skipped)
		}
		return mcpSaveFinishedMsg{Status: successStatus(summary), Cfg: cfg}
	}
}

func (m *mcpManagerModel) importFromClaude() tea.Cmd {
	m.status = "Importing from Claude..."
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		servers, err := config.LoadClaudeUserMcpServers("")
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		result := cfg.ImportMcpServers(servers)
		if err := config.Save(cfg); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		summary := fmt.Sprintf("Imported %d server(s) from Claude.", result.Added)
		if result.Skipped > 0 {
			summary += fmt.Sprintf(" %d skipped.", result.Skipped)
		}
		return mcpSaveFinishedMsg{Status: successStatus(summary), Cfg: cfg}
	}
}

func (m *mcpManagerModel) syncToCodex() tea.Cmd {
	m.status = "Exporting to Codex..."
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		if err := config.SaveCodexMcpServers("", cfg.McpServers); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: successStatus("Exported servers to Codex."), Cfg: cfg}
	}
}

func (m *mcpManagerModel) syncToClaude() tea.Cmd {
	m.status = "Exporting to Claude..."
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		if err := config.SaveClaudeUserMcpServers("", cfg.McpServers); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: successStatus("Exported servers to Claude."), Cfg: cfg}
	}
}
