package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

type mcpStatusKind int

const (
	mcpStatusUnknown mcpStatusKind = iota
	mcpStatusConfigured
	mcpStatusReachable
	mcpStatusBroken
)

type mcpProbeStage string

const (
	mcpProbeStageSpawn      mcpProbeStage = "spawn"
	mcpProbeStageInitialize mcpProbeStage = "initialize"
	mcpProbeStageToolsList  mcpProbeStage = "tools/list"
)

type mcpProbeResult struct {
	Stage      mcpProbeStage
	Err        string
	ToolsCount int
	Latency    time.Duration
	ProbedAt   time.Time
}

type mcpStatusSummary struct {
	Kind        mcpStatusKind
	Badge       string
	Headline    string
	Detail      string
	Suggestions []string
}

type mcpProbeFinishedMsg struct {
	Name   string
	Result *mcpProbeResult
}

type mcpSaveFinishedMsg struct {
	Status    string
	Err       error
	Cfg       *config.RootConfig
	ProbeName string
	Result    *mcpProbeResult
}

type mcpBrowseFocus int

const (
	mcpBrowseFocusQuickAdd mcpBrowseFocus = iota
	mcpBrowseFocusServers
	mcpBrowseFocusActions
)

type mcpQuickAddItem struct {
	Key         string
	Label       string
	Transport   string
	Description string
}

type mcpActionItem struct {
	Key         string
	Label       string
	Description string
}

type mcpTransferItem struct {
	Key         string
	Label       string
	Description string
}

type mcpManagerModel struct {
	cfg      *config.RootConfig
	names    []string
	selected int
	width    int
	height   int
	status   string

	probes  map[string]*mcpProbeResult
	running map[string]bool

	confirmDelete bool
	browseFocus   mcpBrowseFocus
	quickAddItems []mcpQuickAddItem
	quickAddIndex int
	actionIndex   int
	transferring  bool
	transferItems []mcpTransferItem
	transferIndex int

	editing          bool
	adding           bool
	editorMode       int
	editFocus        int
	editOriginalName string
	editFields       []mcpEditField
	editCursor       map[int]int
	rawEditor        string
	rawCursor        int
}

type mcpEditField struct {
	label     string
	value     string
	kind      mcpEditFieldKind
	options   []string
	multiline bool
}

type mcpEditFieldKind int

const (
	mcpEditKindInput mcpEditFieldKind = iota
	mcpEditKindSelect
)

const (
	mcpEditorModeForm = iota
	mcpEditorModeRaw
)

const (
	mcpEditFieldName = iota
	mcpEditFieldTransport
	mcpEditFieldEnabled
	mcpEditFieldCommand
	mcpEditFieldURL
	mcpEditFieldArgs
	mcpEditFieldEnv
	mcpEditFieldDisabledReason
)

func ManageMCPDashboard(cfg *config.RootConfig) error {
	m := newMCPManagerModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newMCPManagerModel(cfg *config.RootConfig) *mcpManagerModel {
	config.Normalize(cfg)
	m := &mcpManagerModel{
		cfg:         cfg,
		status:      "Ready.\nCreate a server, then save and probe.",
		probes:      map[string]*mcpProbeResult{},
		running:     map[string]bool{},
		browseFocus: mcpBrowseFocusQuickAdd,
		quickAddItems: []mcpQuickAddItem{
			{Key: "add", Label: "Add", Transport: "stdio", Description: "Create a new MCP server"},
			{Key: "transfer", Label: "Transfer", Description: "Import or export MCP servers"},
		},
		transferItems: []mcpTransferItem{
			{Key: "import_codex", Label: "Import from Codex", Description: "Load missing servers from Codex"},
			{Key: "import_claude", Label: "Import from Claude", Description: "Load missing user-scope servers from ~/.claude.json"},
			{Key: "export_codex", Label: "Export to Codex", Description: "Write Spark servers to Codex"},
			{Key: "export_claude", Label: "Export to Claude", Description: "Write Spark servers to ~/.claude.json"},
		},
	}
	m.refreshNames()
	return m
}

func (m *mcpManagerModel) Init() tea.Cmd { return nil }

func (m *mcpManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case mcpProbeFinishedMsg:
		delete(m.running, msg.Name)
		if msg.Result != nil {
			m.probes[msg.Name] = msg.Result
			status := summarizeMCPStatus(msg.Name, m.cfg.GetMcpServer(msg.Name), msg.Result)
			m.status = fmt.Sprintf("%s → %s", msg.Name, status.Headline)
		}
		return m, nil
	case mcpSaveFinishedMsg:
		if msg.Err != nil {
			m.status = errorStatus(msg.Err.Error())
			return m, nil
		}
		if msg.Cfg != nil {
			m.cfg = msg.Cfg
			config.Normalize(m.cfg)
			m.refreshNames()
		}
		if msg.Status != "" {
			m.status = msg.Status
		}
		if msg.ProbeName != "" && msg.Result != nil {
			m.probes[msg.ProbeName] = msg.Result
			status := summarizeMCPStatus(msg.ProbeName, m.cfg.GetMcpServer(msg.ProbeName), msg.Result)
			m.status = fmt.Sprintf("%s → %s", msg.ProbeName, status.Headline)
		}
		return m, nil
	case tea.KeyMsg:
		if m.confirmDelete {
			return m, m.handleConfirmKey(msg)
		}
		if m.transferring {
			return m, m.handleTransferKey(msg)
		}
		if m.editing {
			return m, m.handleEditorKey(msg)
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *mcpManagerModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "ctrl+c", "q":
		return tea.Quit
	case "tab":
		m.moveBrowseFocus(1)
		return nil
	case "shift+tab":
		m.moveBrowseFocus(-1)
		return nil
	case "up", "k":
		m.moveBrowseSelection(-1)
	case "down", "j":
		m.moveBrowseSelection(1)
	case "left":
		if m.browseFocus == mcpBrowseFocusActions {
			m.moveActionSelection(-1)
		}
	case "right":
		if m.browseFocus == mcpBrowseFocusActions {
			m.moveActionSelection(1)
		}
	case "enter":
		return m.activateFocusedItem()
	case "p":
		return m.probeCurrent()
	case "r":
		return m.probeAll()
	case "e":
		m.startEditCurrent()
	case "a":
		m.startAddEditor("stdio")
	case "t":
		m.openTransferMenu()
	case "d", "x":
		if m.currentName() != "" {
			m.confirmDelete = true
			m.status = fmt.Sprintf("Delete %s? Press Y to confirm or N to cancel.", m.currentName())
		}
	}
	return nil
}

func (m *mcpManagerModel) moveBrowseFocus(delta int) {
	focuses := []mcpBrowseFocus{mcpBrowseFocusQuickAdd, mcpBrowseFocusServers}
	current := 0
	for i, focus := range focuses {
		if focus == m.browseFocus {
			current = i
			break
		}
	}
	next := (current + delta) % len(focuses)
	if next < 0 {
		next += len(focuses)
	}
	m.browseFocus = focuses[next]
}

func (m *mcpManagerModel) moveBrowseSelection(delta int) {
	switch m.browseFocus {
	case mcpBrowseFocusQuickAdd:
		if len(m.quickAddItems) == 0 {
			return
		}
		m.quickAddIndex = clampIndex(m.quickAddIndex+delta, len(m.quickAddItems))
	case mcpBrowseFocusServers:
		if len(m.names) == 0 {
			return
		}
		m.selected = clampIndex(m.selected+delta, len(m.names))
	case mcpBrowseFocusActions:
		actions := m.browseActions()
		if len(actions) == 0 {
			return
		}
		m.actionIndex = clampIndex(m.actionIndex+delta, len(actions))
	}
}

func (m *mcpManagerModel) moveActionSelection(delta int) {
	actions := m.browseActions()
	if len(actions) == 0 {
		return
	}
	m.actionIndex = clampIndex(m.actionIndex+delta, len(actions))
}

func (m *mcpManagerModel) activateFocusedItem() tea.Cmd {
	switch m.browseFocus {
	case mcpBrowseFocusQuickAdd:
		if len(m.quickAddItems) == 0 {
			return nil
		}
		item := m.quickAddItems[clampIndex(m.quickAddIndex, len(m.quickAddItems))]
		switch item.Key {
		case "add":
			m.startAddEditor(item.Transport)
		case "transfer":
			m.openTransferMenu()
		}
	case mcpBrowseFocusActions:
		return m.activateCurrentAction()
	case mcpBrowseFocusServers:
		if m.currentName() != "" {
			m.status = fmt.Sprintf("Selected %s.\nReview diagnostics or press Enter on Actions to continue.", m.currentName())
		}
	}
	return nil
}

func (m *mcpManagerModel) activateCurrentAction() tea.Cmd {
	actions := m.browseActions()
	if len(actions) == 0 {
		return nil
	}
	action := actions[clampIndex(m.actionIndex, len(actions))]
	switch action.Key {
	case "probe":
		return m.probeCurrent()
	case "edit":
		m.startEditCurrent()
	case "toggle":
		return m.toggleCurrentEnabled()
	case "delete":
		if m.currentName() != "" {
			m.confirmDelete = true
			m.status = fmt.Sprintf("Delete %s? Press Y to confirm or N to cancel.", m.currentName())
		}
	case "transfer":
		m.openTransferMenu()
	}
	return nil
}

func (m *mcpManagerModel) handleTransferKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "esc":
		m.transferring = false
		m.status = infoStatus("Transfer canceled.")
		return nil
	case "up", "k":
		if len(m.transferItems) > 0 {
			m.transferIndex = clampIndex(m.transferIndex-1, len(m.transferItems))
		}
		return nil
	case "down", "j":
		if len(m.transferItems) > 0 {
			m.transferIndex = clampIndex(m.transferIndex+1, len(m.transferItems))
		}
		return nil
	case "enter":
		return m.activateTransferItem()
	}
	return nil
}

func (m *mcpManagerModel) handleEditorKey(msg tea.KeyMsg) tea.Cmd {
	if m.editorMode == mcpEditorModeForm && m.editFocus >= 0 && m.editFocus < len(m.editFields) {
		field := m.editFields[m.editFocus]
		if field.kind == mcpEditKindSelect {
			switch msg.String() {
			case "left", "h":
				m.cycleSelectedField(-1)
				return nil
			case "right", "l":
				m.cycleSelectedField(1)
				return nil
			case "enter", " ":
				m.cycleSelectedField(1)
				return nil
			}
		}
	}
	switch msg.String() {
	case "esc":
		m.editing = false
		m.status = "Edit canceled."
		return nil
	case "tab":
		if m.editorMode == mcpEditorModeRaw {
			m.editing = false
			m.status = "Exited raw editor."
			return nil
		}
		m.moveEditFocus(1)
		return nil
	case "shift+tab":
		if m.editorMode == mcpEditorModeForm {
			m.moveEditFocus(-1)
		}
		return nil
	case "f2":
		return m.saveEditor(false)
	case "f5":
		return m.saveEditor(true)
	case "up":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorVertical(-1)
			return nil
		}
		m.moveFocusedFieldCursorVertical(-1)
		return nil
	case "down":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorVertical(1)
			return nil
		}
		m.moveFocusedFieldCursorVertical(1)
		return nil
	case "left":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorHorizontal(-1)
			return nil
		}
		m.moveFocusedFieldCursorHorizontal(-1)
		return nil
	case "right":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorHorizontal(1)
			return nil
		}
		m.moveFocusedFieldCursorHorizontal(1)
		return nil
	case "home":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorToLineBoundary(false)
			return nil
		}
		m.moveFocusedFieldCursorToLineBoundary(false)
		return nil
	case "end":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorToLineBoundary(true)
			return nil
		}
		m.moveFocusedFieldCursorToLineBoundary(true)
		return nil
	case "f3":
		m.switchEditorMode(mcpEditorModeForm)
		return nil
	case "f4":
		m.switchEditorMode(mcpEditorModeRaw)
		return nil
	case "enter":
		if m.editorMode == mcpEditorModeRaw {
			m.rawEditor += "\n"
			return nil
		}
		if m.editorMode == mcpEditorModeForm && m.editFields[m.editFocus].multiline {
			m.editFields[m.editFocus].value += "\n"
			return nil
		}
	}
	if m.editorMode == mcpEditorModeRaw {
		m.handleRawInput(msg)
		return nil
	}
	m.handleFormInput(msg)
	return nil
}

func (m *mcpManagerModel) handleFormInput(msg tea.KeyMsg) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := &m.editFields[m.editFocus]
	if field.kind == mcpEditKindSelect {
		return
	}
	switch msg.String() {
	case "backspace":
		field.value, m.editCursor[m.editFocus] = deleteBeforeCursor(field.value, m.editCursor[m.editFocus])
		return
	case "delete":
		field.value, m.editCursor[m.editFocus] = deleteAtCursor(field.value, m.editCursor[m.editFocus])
		return
	}
	if len(msg.Runes) == 0 {
		return
	}
	field.value, m.editCursor[m.editFocus] = insertAtCursor(field.value, m.editCursor[m.editFocus], filterPrintableRunes(msg.Runes))
}

func (m *mcpManagerModel) handleRawInput(msg tea.KeyMsg) {
	switch msg.String() {
	case "backspace":
		m.rawEditor, m.rawCursor = deleteBeforeCursor(m.rawEditor, m.rawCursor)
		return
	case "delete":
		m.rawEditor, m.rawCursor = deleteAtCursor(m.rawEditor, m.rawCursor)
		return
	}
	if len(msg.Runes) == 0 {
		return
	}
	m.rawEditor, m.rawCursor = insertAtCursor(m.rawEditor, m.rawCursor, filterPrintableRunes(msg.Runes))
}

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

func (m *mcpManagerModel) handleConfirmKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.confirmDelete = false
		return m.deleteCurrent()
	case "n", "esc":
		m.confirmDelete = false
		m.status = "Delete canceled."
	}
	return nil
}

func (m *mcpManagerModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	header := dashboardHeaderStyle.Width(m.width - 6).Render("MCP Manager")
	left := m.renderServerList()
	right := m.renderDetails()

	leftW := m.leftPaneWidth()
	rightW := m.width - leftW - 4
	if rightW < 44 {
		rightW = 44
	}

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.leftPaneStyle(leftW).Render(left),
		m.rightPaneStyle(rightW).Render(right),
	)

	statusBar := pmStatusBarStyle.Width(m.width - 4).Render(
		lipgloss.NewStyle().Align(lipgloss.Right).Foreground(colorMuted).Render(m.contextHelpText()),
	)

	return fitToViewportHeight(pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar)), m.height)
}

func (m *mcpManagerModel) leftPaneWidth() int {
	leftW := 42
	if m.width > 0 && m.width < 100 {
		leftW = 36
	}
	if m.width > 0 && leftW > m.width/2 {
		leftW = m.width / 2
	}
	if leftW < 34 {
		return 34
	}
	return leftW
}

func (m *mcpManagerModel) leftPaneItemWidth() int {
	return max(36, m.leftPaneWidth()-4)
}

func (m *mcpManagerModel) leftPaneStyle(width int) lipgloss.Style {
	style := pmPanelStyle.Width(width)
	if !m.editing && !m.transferring && m.browseFocus != mcpBrowseFocusActions {
		return pmFocusedPanelStyle.Width(width)
	}
	return style
}

func (m *mcpManagerModel) rightPaneStyle(width int) lipgloss.Style {
	style := pmPanelStyle.Width(width)
	if m.editing || m.transferring || m.browseFocus == mcpBrowseFocusActions {
		return pmFocusedPanelStyle.Width(width)
	}
	return style
}

func (m *mcpManagerModel) renderStatusBar() string {
	if m.confirmDelete {
		return m.status
	}
	statusMain, statusLog := splitStatusText(m.status)
	if statusMain == "" {
		statusMain = "Ready."
	}
	main := styleStatusMain(statusMain)
	help := lipgloss.NewStyle().Foreground(colorDim).Align(lipgloss.Right).Render(m.contextHelpText())
	if statusLog == "" {
		return lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(m.width/2).Render(main),
			lipgloss.NewStyle().Width(max(0, m.width/2-6)).Render(help),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(m.width/2).Render(main),
			lipgloss.NewStyle().Width(max(0, m.width/2-6)).Render(help),
		),
		pmStatusLogStyle.Render("↳ "+statusLog),
	)
}

func (m *mcpManagerModel) statusSummaryText() string {
	main, _ := splitStatusText(m.status)
	main = strings.TrimSpace(main)
	if main == "" || strings.EqualFold(main, "Ready.") || strings.EqualFold(main, "Ready") {
		return "No recent activity"
	}
	return main
}

func (m *mcpManagerModel) renderStatusSection(width int) []string {
	title := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Status")
	statusText := m.statusSummaryText()
	return []string{
		title,
		lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(statusText),
	}
}

func (m *mcpManagerModel) renderHelpSection(width int) []string {
	title := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Help")
	return []string{
		title,
		lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(m.contextHelpText()),
	}
}

func (m *mcpManagerModel) contextHelpText() string {
	if m.confirmDelete {
		return "Y confirm • N cancel"
	}
	if m.editing {
		if m.editorMode == mcpEditorModeRaw {
			return "F2 Save • F5 Save & Probe • F3 Form • Esc Cancel"
		}
		return "Tab Move • F4 Raw • F2 Save • F5 Save & Probe • Esc Cancel"
	}
	if m.transferring {
		return "Up/Down Select • Enter Run Transfer • Esc Cancel"
	}
	switch m.browseFocus {
	case mcpBrowseFocusQuickAdd:
		return "Enter Run Action • Tab Next Zone • A Add • T Transfer"
	case mcpBrowseFocusServers:
		return "Up/Down Select • Tab Next Zone • E Edit • P Probe • D Delete"
	case mcpBrowseFocusActions:
		return "Enter Run Action • Left/Right Change • Tab Next Zone"
	default:
		return "Q Quit"
	}
}

func (m *mcpManagerModel) renderServerList() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Underline(true).Render("Actions"),
		lipgloss.NewStyle().Foreground(colorDim).Render("Global MCP operations"),
		"",
	}
	for i, item := range m.quickAddItems {
		lines = append(lines, m.renderQuickAddItem(i, item))
	}
	lines = append(lines, "", lipgloss.NewStyle().Bold(true).Underline(true).Render("Your Servers"), lipgloss.NewStyle().Foreground(colorDim).Render("Status and transport at a glance"), "")
	if len(m.names) == 0 {
		lines = append(lines, pmItemStyle.Render("  No servers configured yet"))
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}
	for i, name := range m.names {
		lines = append(lines, m.renderServerRow(i, name))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render("Shortcuts: A add • T transfer • E edit • P probe • D delete"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderDetails() string {
	if m.editing {
		return m.renderEditor()
	}
	if m.transferring {
		return m.renderTransferMenu()
	}
	name := m.currentName()
	if name == "" {
		return m.renderEmptyState()
	}
	server := m.cfg.GetMcpServer(name)
	status := m.currentStatus()
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Overview"),
		lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(name),
		"",
		fmt.Sprintf("Status: %s %s", renderStatusBadge(status), strings.Title(status.Headline)),
		fmt.Sprintf("Transport: %s", transportLabel(server)),
		fmt.Sprintf("Enabled: %t", server != nil && server.Enabled),
	}
	if server != nil {
		if strings.TrimSpace(server.Command) != "" {
			lines = append(lines, fmt.Sprintf("Command: %s", server.Command))
		}
		if len(server.Args) > 0 {
			lines = append(lines, fmt.Sprintf("Args: %d configured", len(server.Args)))
			lines = append(lines, fmt.Sprintf("Arg Preview: %s", strings.Join(server.Args, " ")))
		}
		if strings.TrimSpace(server.URL) != "" {
			lines = append(lines, fmt.Sprintf("URL: %s", server.URL))
		}
		if len(server.Env) > 0 {
			lines = append(lines, fmt.Sprintf("Env: %d entries", len(server.Env)))
		}
		if strings.TrimSpace(server.DisabledReason) != "" {
			lines = append(lines, fmt.Sprintf("Disabled reason: %s", server.DisabledReason))
		}
	}
	if probe := m.probes[name]; probe != nil {
		lines = append(lines, fmt.Sprintf("Last probe: %s", probe.ProbedAt.Format(time.RFC3339)))
		lines = append(lines, fmt.Sprintf("Latency: %s", probe.Latency.Round(time.Millisecond)))
		if probe.ToolsCount > 0 {
			lines = append(lines, fmt.Sprintf("Tools detected: %d", probe.ToolsCount))
		}
	}
	lines = append(lines, "", lipgloss.NewStyle().Bold(true).Render("Diagnostics"))
	lines = append(lines, m.renderDiagnostics(status, m.probes[name])...)
	lines = append(lines, "")
	lines = append(lines, m.renderStatusSection(max(36, m.width-50))...)
	lines = append(lines, "")
	lines = append(lines, m.renderHelpSection(max(36, m.width-50))...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderEmptyState() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("No MCP servers yet"),
		"",
		"Manage local and remote MCP endpoints from one place.",
		"Recommended path: create a server, choose transport in the editor, then run a probe.",
		"",
		lipgloss.NewStyle().Bold(true).Render("Start Here"),
		"• Create MCP Server",
		"• Choose transport in the editor",
		"• Save and probe to verify the endpoint",
		"",
		lipgloss.NewStyle().Foreground(colorDim).Render("The Actions list on the left is the default focus."),
	}
	lines = append(lines, "")
	lines = append(lines, m.renderStatusSection(max(36, m.width-50))...)
	lines = append(lines, "")
	lines = append(lines, m.renderHelpSection(max(36, m.width-50))...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderEditor() string {
	title := "Edit Server"
	if m.adding {
		title = "Create Server"
	}
	if !m.adding && m.editOriginalName != "" {
		title += ": " + m.editOriginalName
	}
	modeForm := renderMCPModeToggle("F3 Form", m.editorMode == mcpEditorModeForm)
	modeRaw := renderMCPModeToggle("F4 Raw YAML/JSON", m.editorMode == mcpEditorModeRaw)
	if m.editorMode == mcpEditorModeRaw {
		modeForm = renderMCPModeToggle("F3 Form", false)
		modeRaw = renderMCPModeToggle("F4 Raw YAML/JSON", true)
	}
	contentWidth := max(36, m.width-50)
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(title),
		fmt.Sprintf("View: %s   %s", modeForm, modeRaw),
		lipgloss.NewStyle().Foreground(colorDim).Render(m.editorIntroText()),
		"",
	}
	if m.editorMode == mcpEditorModeForm {
		for _, i := range m.visibleEditFieldIndices() {
			field := m.editFields[i]
			value := m.renderEditorFieldValue(i, field, i == m.editFocus)
			label := fmt.Sprintf("%-16s", field.label)
			style := pmCompactInputStyle.Copy().Width(max(24, m.width-64))
			if i == m.editFocus {
				style = pmCompactFocusedInputStyle.Copy().Width(max(24, m.width-64))
			}
			divider := lipgloss.NewStyle().Foreground(colorBorder).Render("│")
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Center, pmLabelStyle.Render(label), divider, style.Render(value)))
		}
		if hint := m.editorFieldHint(); hint != "" {
			lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render(hint))
		}
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render("Tab/Shift+Tab move fields. Left/Right changes select fields."))
	} else {
		editorText := renderCursorText(m.rawEditor, m.rawCursor)
		lines = append(lines, pmFocusedInputStyle.Copy().Width(contentWidth).Height(max(12, m.height-14)).Render(editorText))
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render("Tab exits editor. Format must be valid YAML/JSON."))
	}
	actionTitle := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Actions")
	saveBtn := pmCompactPrimaryBtnStyle.Copy().MarginRight(0).Render("[F2] Save")
	probeBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("[F5] Save & Probe")
	cancelBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("[Esc] Cancel")
	actionRow1 := lipgloss.JoinHorizontal(lipgloss.Top, probeBtn, lipgloss.PlaceHorizontal(max(2, contentWidth-lipgloss.Width(probeBtn)-lipgloss.Width(saveBtn)), lipgloss.Right, saveBtn))
	actionRow2 := cancelBtn
	lines = append(lines, "", actionTitle, actionRow1, actionRow2, "")
	lines = append(lines, m.renderStatusSection(contentWidth)...)
	lines = append(lines, "")
	lines = append(lines, m.renderHelpSection(contentWidth)...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func renderMCPModeToggle(label string, selected bool) string {
	if selected {
		return "(●) " + label
	}
	return "( ) " + label
}

func (m *mcpManagerModel) renderEditorFieldValue(index int, field mcpEditField, focused bool) string {
	value := field.value
	if field.kind == mcpEditKindSelect {
		options := make([]string, 0, len(field.options))
		current := strings.TrimSpace(field.value)
		for _, option := range field.options {
			if option == current {
				options = append(options, "["+option+"]")
			} else {
				options = append(options, option)
			}
		}
		value = strings.Join(options, "  ")
	}
	if field.kind != mcpEditKindSelect && focused {
		cursor := m.editCursor[index]
		value = renderCursorText(value, cursor)
	} else if value == "" {
		value = " "
	}
	return value
}

func (m *mcpManagerModel) editorIntroText() string {
	if m.adding {
		transport := ""
		if len(m.editFields) > mcpEditFieldTransport {
			transport = strings.TrimSpace(m.editFields[mcpEditFieldTransport].value)
		}
		return fmt.Sprintf("Fill the minimum fields for a %s server, then save or probe.", transport)
	}
	return "Review the current server, then edit only the fields that need to change."
}

func (m *mcpManagerModel) editorFieldHint() string {
	switch m.editFocus {
	case mcpEditFieldCommand:
		return "Command: executable name or absolute path used to launch the stdio server."
	case mcpEditFieldArgs:
		return "Args: one argument per line."
	case mcpEditFieldEnv:
		return "Env: use KEY=value, one per line."
	case mcpEditFieldURL:
		return "URL: point directly to the MCP endpoint."
	default:
		return ""
	}
}

func containsEditField(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func filterPrintableRunes(runes []rune) []rune {
	out := make([]rune, 0, len(runes))
	for _, r := range runes {
		if r < 32 {
			continue
		}
		out = append(out, r)
	}
	return out
}

func insertAtCursor(value string, cursor int, inserted []rune) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if len(inserted) == 0 {
		return value, cursor
	}
	updated := append(append(append([]rune{}, r[:cursor]...), inserted...), r[cursor:]...)
	return string(updated), cursor + len(inserted)
}

func deleteBeforeCursor(value string, cursor int) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if cursor == 0 {
		return value, cursor
	}
	updated := append(append([]rune{}, r[:cursor-1]...), r[cursor:]...)
	return string(updated), cursor - 1
}

func deleteAtCursor(value string, cursor int) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if cursor >= len(r) {
		return value, cursor
	}
	updated := append(append([]rune{}, r[:cursor]...), r[cursor+1:]...)
	return string(updated), cursor
}

func clampIndexInclusive(value, max int) int {
	if value < 0 {
		return 0
	}
	if value > max {
		return max
	}
	return value
}

func renderCursorText(value string, cursor int) string {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	cursorToken := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("|")
	if len(r) == 0 {
		return cursorToken
	}
	return string(r[:cursor]) + cursorToken + string(r[cursor:])
}

func cursorLineColumn(r []rune, cursor int) (lineStart, lineEnd, column int) {
	cursor = clampIndexInclusive(cursor, len(r))
	lineStart = cursor
	for lineStart > 0 && r[lineStart-1] != '\n' {
		lineStart--
	}
	lineEnd = cursor
	for lineEnd < len(r) && r[lineEnd] != '\n' {
		lineEnd++
	}
	column = cursor - lineStart
	return lineStart, lineEnd, column
}

func moveCursorHorizontal(value string, cursor, delta int) int {
	r := []rune(value)
	return clampIndexInclusive(cursor+delta, len(r))
}

func moveCursorToLineBoundary(value string, cursor int, end bool) int {
	r := []rune(value)
	lineStart, lineEnd, _ := cursorLineColumn(r, cursor)
	if end {
		return lineEnd
	}
	return lineStart
}

func moveCursorVertical(value string, cursor, delta int) int {
	r := []rune(value)
	lineStart, _, column := cursorLineColumn(r, cursor)
	if delta < 0 {
		if lineStart == 0 {
			return cursor
		}
		prevEnd := lineStart - 1
		prevStart := prevEnd
		for prevStart > 0 && r[prevStart-1] != '\n' {
			prevStart--
		}
		return min(prevStart+column, prevEnd)
	}
	lineEnd := moveCursorToLineBoundary(value, cursor, true)
	if lineEnd >= len(r) {
		return cursor
	}
	nextStart := lineEnd + 1
	nextEnd := nextStart
	for nextEnd < len(r) && r[nextEnd] != '\n' {
		nextEnd++
	}
	return min(nextStart+column, nextEnd)
}

func (m *mcpManagerModel) moveFocusedFieldCursorHorizontal(delta int) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := m.editFields[m.editFocus]
	if field.kind == mcpEditKindSelect {
		return
	}
	m.editCursor[m.editFocus] = moveCursorHorizontal(field.value, m.editCursor[m.editFocus], delta)
}

func (m *mcpManagerModel) moveFocusedFieldCursorVertical(delta int) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := m.editFields[m.editFocus]
	if field.kind == mcpEditKindSelect {
		return
	}
	m.editCursor[m.editFocus] = moveCursorVertical(field.value, m.editCursor[m.editFocus], delta)
}

func (m *mcpManagerModel) moveFocusedFieldCursorToLineBoundary(end bool) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := m.editFields[m.editFocus]
	if field.kind == mcpEditKindSelect {
		return
	}
	m.editCursor[m.editFocus] = moveCursorToLineBoundary(field.value, m.editCursor[m.editFocus], end)
}

func (m *mcpManagerModel) moveRawCursorHorizontal(delta int) {
	m.rawCursor = moveCursorHorizontal(m.rawEditor, m.rawCursor, delta)
}

func (m *mcpManagerModel) moveRawCursorVertical(delta int) {
	m.rawCursor = moveCursorVertical(m.rawEditor, m.rawCursor, delta)
}

func (m *mcpManagerModel) moveRawCursorToLineBoundary(end bool) {
	m.rawCursor = moveCursorToLineBoundary(m.rawEditor, m.rawCursor, end)
}

func (m *mcpManagerModel) refreshNames() {
	m.names = m.cfg.ListMcpServers()
	if len(m.names) == 0 {
		m.selected = 0
		return
	}
	if m.selected >= len(m.names) {
		m.selected = len(m.names) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func (m *mcpManagerModel) currentName() string {
	if m.selected < 0 || m.selected >= len(m.names) {
		return ""
	}
	return m.names[m.selected]
}

func (m *mcpManagerModel) selectByName(name string) {
	for i, candidate := range m.names {
		if candidate == name {
			m.selected = i
			return
		}
	}
}

func (m *mcpManagerModel) currentStatus() mcpStatusSummary {
	return summarizeMCPStatus(m.currentName(), m.cfg.GetMcpServer(m.currentName()), m.probes[m.currentName()])
}

func (m *mcpManagerModel) browseActions() []mcpActionItem {
	actions := []mcpActionItem{}
	if m.currentName() != "" {
		toggleLabel := "Disable"
		toggleDesc := "Temporarily disable this server"
		if server := m.cfg.GetMcpServer(m.currentName()); server != nil && !server.Enabled {
			toggleLabel = "Enable"
			toggleDesc = "Re-enable this server"
		}
		actions = append(actions,
			mcpActionItem{Key: "probe", Label: "Probe", Description: "Run initialize and tools/list"},
			mcpActionItem{Key: "edit", Label: "Edit", Description: "Open the focused server form"},
			mcpActionItem{Key: "toggle", Label: toggleLabel, Description: toggleDesc},
			mcpActionItem{Key: "delete", Label: "Delete", Description: "Remove this server from config"},
		)
	}
	actions = append(actions,
		mcpActionItem{Key: "transfer", Label: "Transfer", Description: "Import or export MCP servers"},
	)
	if len(actions) == 0 {
		return actions
	}
	m.actionIndex = clampIndex(m.actionIndex, len(actions))
	return actions
}

func (m *mcpManagerModel) renderQuickAddItem(i int, item mcpQuickAddItem) string {
	title := item.Label
	summary := m.quickAddSummary(item)
	style := pmItemStyle.Copy().Width(m.leftPaneItemWidth())
	prefix := "  "
	if i == m.quickAddIndex {
		if m.browseFocus == mcpBrowseFocusQuickAdd {
			style = pmFocusedItemStyle.Copy().Width(m.leftPaneItemWidth())
			prefix = "➤ "
		} else {
			style = pmSelectedItemStyle.Copy().Width(m.leftPaneItemWidth())
			prefix = "◆ "
		}
	}
	return style.Render(prefix + title + "\n" + strings.Repeat(" ", len(prefix)) + "  " + summary)
}

func (m *mcpManagerModel) quickAddSummary(item mcpQuickAddItem) string {
	switch item.Key {
	case "add":
		return "New server"
	case "transfer":
		return "Import/Export"
	default:
		return item.Description
	}
}

func (m *mcpManagerModel) renderServerRow(i int, name string) string {
	server := m.cfg.GetMcpServer(name)
	status := summarizeMCPStatus(name, server, m.probes[name])
	detail := transportLabel(server)
	if server != nil && !server.Enabled {
		detail += " • disabled"
	}
	if m.running[name] {
		detail += " • probing"
	}
	line1 := fmt.Sprintf("%s %s", renderStatusBadge(status), name)
	style := pmItemStyle.Copy().Width(m.leftPaneItemWidth())
	prefix := "  "
	if i == m.selected {
		if m.browseFocus == mcpBrowseFocusServers {
			style = pmFocusedItemStyle.Copy().Width(m.leftPaneItemWidth())
			prefix = "➤ "
		} else {
			style = pmSelectedItemStyle.Copy().Width(m.leftPaneItemWidth())
			prefix = "◆ "
		}
	}
	return style.Render(prefix + line1 + "\n" + strings.Repeat(" ", len(prefix)) + "  " + detail)
}

func (m *mcpManagerModel) renderDiagnostics(status mcpStatusSummary, probe *mcpProbeResult) []string {
	lines := []string{
		fmt.Sprintf("Current State: %s %s", renderStatusBadge(status), strings.Title(status.Headline)),
		fmt.Sprintf("Why: %s", status.Detail),
	}
	nextAction := "No action needed."
	if len(status.Suggestions) > 0 {
		nextAction = status.Suggestions[0]
	}
	if status.Kind == mcpStatusUnknown {
		nextAction = "Run Probe to verify initialize and tools/list."
	}
	if probe != nil && probe.Err != "" {
		lines = append(lines, fmt.Sprintf("Failure Stage: %s", probe.Stage))
	}
	lines = append(lines, fmt.Sprintf("Next Action: %s", nextAction))
	if len(status.Suggestions) > 1 {
		lines = append(lines, "Suggested fixes:")
		for _, suggestion := range status.Suggestions[1:min(3, len(status.Suggestions))] {
			lines = append(lines, "• "+suggestion)
		}
	}
	return lines
}

func (m *mcpManagerModel) renderActionItem(i int, action mcpActionItem) string {
	label := fmt.Sprintf("%s  %s", action.Label, lipgloss.NewStyle().Foreground(colorDim).Render(action.Description))
	style := pmItemStyle.Copy().Width(max(24, m.width-48))
	prefix := "  "
	if m.browseFocus == mcpBrowseFocusActions && i == m.actionIndex {
		style = pmSelectedItemStyle.Copy().Width(max(24, m.width-48))
		prefix = "➤ "
	}
	return style.Render(prefix + label)
}

func (m *mcpManagerModel) renderTransferMenu() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Transfer MCP Servers"),
		"",
		"Choose where to import from or export to.",
		"",
	}
	for i, item := range m.transferItems {
		lines = append(lines, m.renderTransferItem(i, item))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render("Import skips same-name Spark servers. Export overwrites same-name target servers."))
	lines = append(lines, "")
	lines = append(lines, m.renderStatusSection(max(36, m.width-50))...)
	lines = append(lines, "")
	lines = append(lines, m.renderHelpSection(max(36, m.width-50))...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderTransferItem(i int, item mcpTransferItem) string {
	label := fmt.Sprintf("%s  %s", item.Label, lipgloss.NewStyle().Foreground(colorDim).Render(item.Description))
	style := pmItemStyle.Copy().Width(max(24, m.width-48))
	prefix := "  "
	if i == m.transferIndex {
		style = pmSelectedItemStyle.Copy().Width(max(24, m.width-48))
		prefix = "➤ "
	}
	return style.Render(prefix + label)
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

func renderStatusBadge(status mcpStatusSummary) string {
	switch status.Kind {
	case mcpStatusConfigured, mcpStatusReachable:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Bold(true).Render(status.Badge)
	case mcpStatusBroken:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Bold(true).Render(status.Badge)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c")).Bold(true).Render(status.Badge)
	}
}

func clampIndex(value, length int) int {
	if length <= 0 {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value >= length {
		return length - 1
	}
	return value
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

func summarizeMCPStatus(name string, server *config.McpServerConfig, probe *mcpProbeResult) mcpStatusSummary {
	if server == nil {
		return mcpStatusSummary{
			Kind:     mcpStatusBroken,
			Badge:    "✕",
			Headline: "broken",
			Detail:   "server not found in current config",
		}
	}

	if detail, suggestions := validateMCPServerConfig(server); detail != "" {
		return mcpStatusSummary{
			Kind:        mcpStatusBroken,
			Badge:       "✕",
			Headline:    "broken",
			Detail:      detail,
			Suggestions: suggestions,
		}
	}

	if probe == nil {
		return mcpStatusSummary{
			Kind:        mcpStatusUnknown,
			Badge:       "?",
			Headline:    "unknown",
			Detail:      "not probed yet",
			Suggestions: []string{"Press P to run spawn/connect → initialize → tools/list and refresh diagnostics."},
		}
	}

	if probe.Err != "" {
		return mcpStatusSummary{
			Kind:        mcpStatusBroken,
			Badge:       "✕",
			Headline:    "broken",
			Detail:      fmt.Sprintf("%s failed: %s", probe.Stage, probe.Err),
			Suggestions: diagnoseMCPFailure(server, probe),
		}
	}

	if isHTTPMCPServer(server) {
		return mcpStatusSummary{
			Kind:        mcpStatusReachable,
			Badge:       "●",
			Headline:    "reachable",
			Detail:      fmt.Sprintf("connected successfully and listed %d tool(s)", probe.ToolsCount),
			Suggestions: []string{"No fix needed. Probe only checks handshake reachability and tools/list, then closes immediately."},
		}
	}

	return mcpStatusSummary{
		Kind:        mcpStatusConfigured,
		Badge:       "✓",
		Headline:    "configured",
		Detail:      fmt.Sprintf("stdio handshake completed and listed %d tool(s)", probe.ToolsCount),
		Suggestions: []string{"No fix needed. Spark only probes the server and kills it immediately after tools/list."},
	}
}

func validateMCPServerConfig(server *config.McpServerConfig) (string, []string) {
	hasCommand := strings.TrimSpace(server.Command) != ""
	hasURL := strings.TrimSpace(server.URL) != ""
	switch {
	case hasCommand && hasURL:
		return "invalid config: both command and url are set", []string{
			"Keep exactly one transport. Use command+args for stdio or url for HTTP/SSE.",
		}
	case !hasCommand && !hasURL:
		return "invalid config: missing transport", []string{
			"Set a command for stdio servers or a url for HTTP/SSE servers.",
		}
	case hasCommand && strings.TrimSpace(server.Command) == "":
		return "invalid config: command is empty", []string{
			"Set the executable name or absolute path in command.",
		}
	}
	return "", nil
}

func diagnoseMCPFailure(server *config.McpServerConfig, probe *mcpProbeResult) []string {
	var suggestions []string
	errText := strings.ToLower(probe.Err)
	if strings.Contains(errText, "executable file not found") || strings.Contains(errText, "file not found") {
		suggestions = append(suggestions, "Install the server binary and verify the command is available in PATH.")
	}
	if strings.Contains(errText, "connection refused") {
		suggestions = append(suggestions, "Start the remote MCP service and verify the URL/port are correct.")
	}
	if strings.Contains(errText, "deadline exceeded") || strings.Contains(errText, "timeout") {
		suggestions = append(suggestions, "Increase startup time on the server side or verify the endpoint responds to initialize quickly.")
	}
	if strings.Contains(errText, "401") || strings.Contains(errText, "403") || strings.Contains(errText, "unauthorized") {
		suggestions = append(suggestions, "Check auth headers, tokens, or reverse proxy rules for this MCP endpoint.")
	}
	if isHTTPMCPServer(server) {
		suggestions = append(suggestions, "Verify the URL points to the MCP endpoint, not a human-facing docs page.")
	} else {
		suggestions = append(suggestions, "Run the configured command manually to confirm it starts and speaks MCP over stdio.")
	}
	if len(suggestions) == 0 {
		suggestions = append(suggestions, "Inspect the raw error, then verify transport settings, credentials, and server startup behavior.")
	}
	return uniqueStrings(suggestions)
}

func transportLabel(server *config.McpServerConfig) string {
	if isHTTPMCPServer(server) {
		return "http/sse"
	}
	return "stdio"
}

func isHTTPMCPServer(server *config.McpServerConfig) bool {
	return server != nil && strings.TrimSpace(server.URL) != ""
}

func cloneMCPServerConfig(server *config.McpServerConfig) *config.McpServerConfig {
	if server == nil {
		return nil
	}
	clone := *server
	if len(server.Args) > 0 {
		clone.Args = append([]string{}, server.Args...)
	}
	if len(server.Env) > 0 {
		clone.Env = make(map[string]string, len(server.Env))
		for k, v := range server.Env {
			clone.Env[k] = v
		}
	}
	if len(server.EnabledTools) > 0 {
		clone.EnabledTools = append([]string{}, server.EnabledTools...)
	}
	if len(server.DisabledTools) > 0 {
		clone.DisabledTools = append([]string{}, server.DisabledTools...)
	}
	if len(server.Scopes) > 0 {
		clone.Scopes = append([]string{}, server.Scopes...)
	}
	if len(server.Tools) > 0 {
		clone.Tools = make(map[string]map[string]any, len(server.Tools))
		for k, v := range server.Tools {
			if v == nil {
				clone.Tools[k] = nil
				continue
			}
			nested := make(map[string]any, len(v))
			for nestedKey, nestedValue := range v {
				nested[nestedKey] = nestedValue
			}
			clone.Tools[k] = nested
		}
	}
	if server.OAuthResource != nil {
		value := *server.OAuthResource
		clone.OAuthResource = &value
	}
	return &clone
}

func successStatus(message string) string {
	return "✓ " + strings.TrimSpace(message)
}

func errorStatus(message string) string {
	return "✗ " + strings.TrimSpace(message)
}

func infoStatus(message string) string {
	return "… " + strings.TrimSpace(message)
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func parseBoolLoose(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseLineList(v string) []string {
	lines := strings.Split(v, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseEnvLines(v string) (map[string]string, error) {
	lines := strings.Split(v, "\n")
	out := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid env line: %q", line)
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out, nil
}

func formatEnvLines(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+env[key])
	}
	return strings.Join(lines, "\n")
}

func currentTransport(server *config.McpServerConfig) string {
	if strings.TrimSpace(server.URL) != "" {
		return "http"
	}
	return "stdio"
}

func marshalMCPServerYAML(server *config.McpServerConfig) string {
	return marshalNamedMCPServerYAML("", server)
}

func marshalNamedMCPServerYAML(name string, server *config.McpServerConfig) string {
	lines := []string{}
	if name != "" {
		lines = append(lines, name+":")
	}
	prefix := ""
	if name != "" {
		prefix = "  "
	}
	if server.Command != "" {
		lines = append(lines, prefix+fmt.Sprintf("command: %s", quoteYAML(server.Command)))
	}
	if len(server.Args) > 0 {
		lines = append(lines, prefix+"args:")
		for _, arg := range server.Args {
			lines = append(lines, prefix+"  - "+quoteYAML(arg))
		}
	}
	if server.URL != "" {
		lines = append(lines, prefix+fmt.Sprintf("url: %s", quoteYAML(server.URL)))
	}
	lines = append(lines, prefix+fmt.Sprintf("enabled: %t", server.Enabled))
	if len(server.Env) > 0 {
		lines = append(lines, prefix+"env:")
		keys := make([]string, 0, len(server.Env))
		for key := range server.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, prefix+fmt.Sprintf("  %s: %s", key, quoteYAML(server.Env[key])))
		}
	}
	if server.DisabledReason != "" {
		lines = append(lines, prefix+fmt.Sprintf("disabled_reason: %s", quoteYAML(server.DisabledReason)))
	}
	return strings.Join(lines, "\n")
}

func quoteYAML(v string) string {
	data, _ := json.Marshal(v)
	return string(data)
}
