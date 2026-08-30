package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
)

func (m *mcpManagerModel) saveEditor(runProbe bool) tea.Cmd {
	cfg, name, err := m.buildEditedServerConfig()
	if err != nil {
		m.status = errorStatus(err.Error())
		return nil
	}
	m.editing = false
	m.cfg = cfg
	m.refreshNames()
	m.selectByName(name)
	return saveAndMaybeProbeMCPConfigCmd(cfg, name, runProbe)
}

func (m *mcpManagerModel) startAddEditor(transport string) {
	m.editing = true
	m.adding = true
	m.editorMode = mcpEditorModeForm
	m.editFocus = 0
	m.editOriginalName = ""
	m.editFields = newMCPEditFields("", &config.McpServerConfig{Enabled: true}, transport)
	m.editCursor = make(map[int]int, len(m.editFields))
	m.rawEditor = ""
	m.rawCursor = 0
	m.status = fmt.Sprintf("Add new %s server. F2 saves, F5 saves and probes.", transport)
}

func (m *mcpManagerModel) startEditCurrent() {
	name := m.currentName()
	server := m.cfg.GetMcpServer(name)
	if name == "" || server == nil {
		return
	}
	m.editing = true
	m.adding = false
	m.editorMode = mcpEditorModeForm
	m.editFocus = 0
	m.editOriginalName = name
	m.editFields = newMCPEditFields(name, server, currentTransport(server))
	m.editCursor = make(map[int]int, len(m.editFields))
	for i, field := range m.editFields {
		m.editCursor[i] = len([]rune(field.value))
	}
	m.rawEditor = marshalMCPServerYAML(server)
	m.rawCursor = len([]rune(m.rawEditor))
	m.status = fmt.Sprintf("Editing %s. F2 saves, F5 saves and probes.", name)
}

func newMCPEditFields(name string, server *config.McpServerConfig, transport string) []mcpEditField {
	if server == nil {
		server = &config.McpServerConfig{Enabled: true}
	}
	args := strings.Join(server.Args, "\n")
	env := formatEnvLines(server.Env)
	enabled := "false"
	if server.Enabled {
		enabled = "true"
	}
	return []mcpEditField{
		{label: "Name", value: name, kind: mcpEditKindInput},
		{label: "Transport", value: transport, kind: mcpEditKindSelect, options: []string{"stdio", "sse", "http"}},
		{label: "Enabled", value: enabled, kind: mcpEditKindSelect, options: []string{"true", "false"}},
		{label: "Command", value: server.Command, kind: mcpEditKindInput},
		{label: "URL", value: server.URL, kind: mcpEditKindInput},
		{label: "Args", value: args, kind: mcpEditKindInput, multiline: true},
		{label: "Env", value: env, kind: mcpEditKindInput, multiline: true},
		{label: "Disabled Reason", value: server.DisabledReason, kind: mcpEditKindInput},
	}
}

func (m *mcpManagerModel) cycleSelectedField(delta int) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := &m.editFields[m.editFocus]
	if field.kind != mcpEditKindSelect || len(field.options) == 0 {
		return
	}
	current := strings.ToLower(strings.TrimSpace(field.value))
	for i, option := range field.options {
		if option == current {
			next := (i + delta) % len(field.options)
			if next < 0 {
				next += len(field.options)
			}
			field.value = field.options[next]
			return
		}
	}
	field.value = field.options[0]
}

func (m *mcpManagerModel) cycleTransport() {
	current := strings.ToLower(strings.TrimSpace(m.editFields[mcpEditFieldTransport].value))
	options := []string{"stdio", "sse", "http"}
	for i, option := range options {
		if option == current {
			m.editFields[mcpEditFieldTransport].value = options[(i+1)%len(options)]
			return
		}
	}
	m.editFields[mcpEditFieldTransport].value = "stdio"
}

func (m *mcpManagerModel) toggleEnabledField() {
	if strings.EqualFold(strings.TrimSpace(m.editFields[mcpEditFieldEnabled].value), "true") {
		m.editFields[mcpEditFieldEnabled].value = "false"
		return
	}
	m.editFields[mcpEditFieldEnabled].value = "true"
}

func (m *mcpManagerModel) syncRawFromFields() {
	server, name, err := m.serverConfigFromForm()
	if err != nil {
		m.status = "Error: " + err.Error()
		return
	}
	m.rawEditor = marshalNamedMCPServerYAML(name, server)
}

func (m *mcpManagerModel) syncFieldsFromRaw() error {
	server, name, err := parseEditedMCPServerRaw(m.rawEditor, m.editOriginalName)
	if err != nil {
		return err
	}
	m.editFields = newMCPEditFields(name, server, currentTransport(server))
	m.editCursor = make(map[int]int, len(m.editFields))
	for i, field := range m.editFields {
		m.editCursor[i] = len([]rune(field.value))
	}
	if m.adding || m.editOriginalName == "" {
		m.editOriginalName = name
	}
	visible := m.visibleEditFieldIndices()
	if len(visible) == 0 {
		m.editFocus = 0
		return nil
	}
	if !containsEditField(visible, m.editFocus) {
		m.editFocus = visible[0]
	}
	return nil
}

func (m *mcpManagerModel) switchEditorMode(mode int) {
	if mode == m.editorMode {
		return
	}
	switch mode {
	case mcpEditorModeRaw:
		m.syncRawFromFields()
		if strings.HasPrefix(m.status, "Error:") {
			return
		}
		m.editorMode = mcpEditorModeRaw
		m.rawCursor = len([]rune(m.rawEditor))
	case mcpEditorModeForm:
		if err := m.syncFieldsFromRaw(); err != nil {
			m.status = "Error: " + err.Error()
			return
		}
		m.editorMode = mcpEditorModeForm
	}
}

func (m *mcpManagerModel) visibleEditFieldIndices() []int {
	transport := ""
	if len(m.editFields) > mcpEditFieldTransport {
		transport = strings.ToLower(strings.TrimSpace(m.editFields[mcpEditFieldTransport].value))
	}
	visible := []int{
		mcpEditFieldName,
		mcpEditFieldTransport,
		mcpEditFieldEnabled,
	}
	switch transport {
	case "sse", "http":
		visible = append(visible, mcpEditFieldURL)
	default:
		visible = append(visible, mcpEditFieldCommand, mcpEditFieldArgs, mcpEditFieldEnv)
	}
	visible = append(visible, mcpEditFieldDisabledReason)
	return visible
}

func (m *mcpManagerModel) moveEditFocus(delta int) {
	visible := m.visibleEditFieldIndices()
	if len(visible) == 0 {
		return
	}
	currentPos := 0
	for i, idx := range visible {
		if idx == m.editFocus {
			currentPos = i
			break
		}
	}
	next := (currentPos + delta) % len(visible)
	if next < 0 {
		next += len(visible)
	}
	m.editFocus = visible[next]
}

func (m *mcpManagerModel) buildEditedServerConfig() (*config.RootConfig, string, error) {
	cfgCopy := *m.cfg
	cfgCopy.McpServers = make(map[string]*config.McpServerConfig, len(m.cfg.McpServers))
	for name, server := range m.cfg.McpServers {
		cfgCopy.McpServers[name] = cloneMCPServerConfig(server)
	}

	var (
		server *config.McpServerConfig
		name   string
		err    error
	)
	if m.editorMode == mcpEditorModeRaw {
		server, name, err = parseEditedMCPServerRaw(m.rawEditor, m.editOriginalName)
	} else {
		server, name, err = m.serverConfigFromForm()
	}
	if err != nil {
		return nil, "", err
	}
	if m.editOriginalName != "" && m.editOriginalName != name {
		delete(cfgCopy.McpServers, m.editOriginalName)
	}
	cfgCopy.SetMcpServer(name, server)
	config.Normalize(&cfgCopy)
	return &cfgCopy, name, nil
}

func (m *mcpManagerModel) serverConfigFromForm() (*config.McpServerConfig, string, error) {
	name := config.McpServerName(strings.TrimSpace(m.editFields[mcpEditFieldName].value))
	if name == "" {
		return nil, "", fmt.Errorf("server name is required")
	}
	transport := strings.ToLower(strings.TrimSpace(m.editFields[mcpEditFieldTransport].value))
	if transport == "" {
		transport = "stdio"
	}
	server := &config.McpServerConfig{
		Enabled:        parseBoolLoose(m.editFields[mcpEditFieldEnabled].value),
		DisabledReason: strings.TrimSpace(m.editFields[mcpEditFieldDisabledReason].value),
	}
	if transport == "stdio" {
		server.Command = strings.TrimSpace(m.editFields[mcpEditFieldCommand].value)
		server.Args = parseLineList(m.editFields[mcpEditFieldArgs].value)
	} else {
		server.URL = strings.TrimSpace(m.editFields[mcpEditFieldURL].value)
	}
	env, err := parseEnvLines(m.editFields[mcpEditFieldEnv].value)
	if err != nil {
		return nil, "", err
	}
	server.Env = env
	if detail, _ := validateMCPServerConfig(server); detail != "" {
		return nil, "", fmt.Errorf("%s", detail)
	}
	return server, name, nil
}

func (m *mcpManagerModel) openTransferMenu() {
	m.transferring = true
	m.transferIndex = clampIndex(m.transferIndex, len(m.transferItems))
	m.status = infoStatus("Choose a transfer action.")
}

func (m *mcpManagerModel) activateTransferItem() tea.Cmd {
	if len(m.transferItems) == 0 {
		return nil
	}
	item := m.transferItems[clampIndex(m.transferIndex, len(m.transferItems))]
	m.transferring = false
	switch item.Key {
	case "import_codex":
		return m.importFromCodex()
	case "import_claude":
		return m.importFromClaude()
	case "export_codex":
		return m.syncToCodex()
	case "export_claude":
		return m.syncToClaude()
	default:
		return nil
	}
}

func (m *mcpManagerModel) probeCurrent() tea.Cmd {
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
		return mcpProbeFinishedMsg{Name: name, Result: probeMCPServer(name, serverCopy)}
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
	if enabled {
		server.Enabled = true
		server.DisabledReason = ""
	} else {
		server.Enabled = false
		server.DisabledReason = "disabled by spark"
	}
	cfgCopy := m.cfg
	return saveMCPConfigCmd(cfgCopy, fmt.Sprintf("%s %s.", name, ternary(enabled, "enabled", "disabled")))
}

func (m *mcpManagerModel) deleteCurrent() tea.Cmd {
	name := m.currentName()
	if name == "" {
		return nil
	}
	delete(m.probes, name)
	delete(m.running, name)
	m.cfg.RemoveMcpServer(name)
	return saveMCPConfigCmd(m.cfg, fmt.Sprintf("Removed %s.", name))
}

func (m *mcpManagerModel) importFromCodex() tea.Cmd {
	m.status = infoStatus("Importing MCP servers from Codex...")
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
		status := successStatus(fmt.Sprintf("Imported %d MCP server(s) from Codex.", result.Added))
		if result.Skipped > 0 {
			status += fmt.Sprintf(" Skipped %d existing server(s).", result.Skipped)
		}
		return mcpSaveFinishedMsg{Status: status, Cfg: cfg}
	}
}

func (m *mcpManagerModel) importFromClaude() tea.Cmd {
	m.status = infoStatus("Importing MCP servers from Claude...")
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
		status := successStatus(fmt.Sprintf("Imported %d MCP server(s) from Claude.", result.Added))
		if result.Skipped > 0 {
			status += fmt.Sprintf(" Skipped %d existing server(s).", result.Skipped)
		}
		return mcpSaveFinishedMsg{Status: status, Cfg: cfg}
	}
}

func (m *mcpManagerModel) syncToCodex() tea.Cmd {
	m.status = infoStatus("Syncing MCP servers to Codex...")
	return func() tea.Msg {
		if err := config.SaveCodexMcpServers("", m.cfg.McpServers); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: successStatus(fmt.Sprintf("Synced %d MCP server(s) to Codex.", config.CountEnabledMcpServers(m.cfg.McpServers))), Cfg: cfg}
	}
}

func (m *mcpManagerModel) syncToClaude() tea.Cmd {
	m.status = infoStatus("Syncing MCP servers to Claude...")
	return func() tea.Msg {
		if err := config.SaveClaudeUserMcpServers("", m.cfg.McpServers); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		cfg, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: successStatus(fmt.Sprintf("Synced %d MCP server(s) to Claude.", config.CountEnabledMcpServers(m.cfg.McpServers))), Cfg: cfg}
	}
}

func saveMCPConfigCmd(cfg *config.RootConfig, success string) tea.Cmd {
	return func() tea.Msg {
		if err := config.Save(cfg); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		reloaded, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: successStatus(success), Cfg: reloaded}
	}
}

func saveAndMaybeProbeMCPConfigCmd(cfg *config.RootConfig, name string, runProbe bool) tea.Cmd {
	return func() tea.Msg {
		if err := config.Save(cfg); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		reloaded, err := config.Load()
		if err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		msg := mcpSaveFinishedMsg{
			Status: successStatus(fmt.Sprintf("Saved %s.", name)),
			Cfg:    reloaded,
		}
		if runProbe {
			msg.Status = infoStatus(fmt.Sprintf("Saved %s and probing...", name))
			msg.ProbeName = name
			msg.Result = probeMCPServer(name, cloneMCPServerConfig(reloaded.GetMcpServer(name)))
		}
		return msg
	}
}

