package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

func TestValidateMCPServerConfigRequiresTransport(t *testing.T) {
	status := summarizeMCPStatus("docs", &config.McpServerConfig{}, nil)
	if status.Kind != mcpStatusBroken {
		t.Fatalf("expected broken status, got %v", status.Kind)
	}
	if !strings.Contains(status.Detail, "missing transport") {
		t.Fatalf("expected missing transport detail, got %q", status.Detail)
	}
	if !strings.Contains(strings.Join(status.Suggestions, "\n"), "command") {
		t.Fatalf("expected command suggestion, got %v", status.Suggestions)
	}
}

func TestSummarizeMCPStatusStdioProbeSuccessIsConfigured(t *testing.T) {
	cfg := &config.McpServerConfig{Command: "npx", Args: []string{"-y", "server"}, Enabled: true}
	status := summarizeMCPStatus("docs", cfg, &mcpProbeResult{Stage: mcpProbeStageToolsList})
	if status.Kind != mcpStatusConfigured {
		t.Fatalf("expected configured, got %v", status.Kind)
	}
}

func TestSummarizeMCPStatusHTTPProbeSuccessIsReachable(t *testing.T) {
	cfg := &config.McpServerConfig{URL: "https://example.com/mcp", Enabled: true}
	status := summarizeMCPStatus("remote", cfg, &mcpProbeResult{Stage: mcpProbeStageToolsList})
	if status.Kind != mcpStatusReachable {
		t.Fatalf("expected reachable, got %v", status.Kind)
	}
}

func TestSummarizeMCPStatusProbeFailureIncludesFixes(t *testing.T) {
	cfg := &config.McpServerConfig{Command: "missing-binary", Enabled: true}
	status := summarizeMCPStatus("broken", cfg, &mcpProbeResult{Stage: mcpProbeStageSpawn, Err: "executable file not found in $PATH"})
	if status.Kind != mcpStatusBroken {
		t.Fatalf("expected broken, got %v", status.Kind)
	}
	if !strings.Contains(status.Detail, "spawn") {
		t.Fatalf("expected stage in detail, got %q", status.Detail)
	}
	if !strings.Contains(strings.Join(status.Suggestions, "\n"), "PATH") {
		t.Fatalf("expected PATH suggestion, got %v", status.Suggestions)
	}
}

func TestMCPManagerProbeResultUpdatesSelectionStatus(t *testing.T) {
	cfg := &config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"docs": {Command: "npx", Enabled: true},
	}}
	m := newMCPManagerModel(cfg)
	m.selectByName("docs")
	_, _ = m.Update(mcpProbeFinishedMsg{Name: "docs", Result: &mcpProbeResult{Stage: mcpProbeStageToolsList}})
	status := m.currentStatus()
	if status.Kind != mcpStatusConfigured {
		t.Fatalf("expected configured, got %v", status.Kind)
	}
}

func TestMCPManagerBrowseFocusStartsInQuickAdd(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	if m.browseFocus != mcpBrowseFocusQuickAdd {
		t.Fatalf("expected quick add focus, got %v", m.browseFocus)
	}
	if len(m.quickAddItems) != 2 {
		t.Fatalf("expected 2 action items, got %d", len(m.quickAddItems))
	}
	if got := m.quickAddItems[0].Label; got != "Add" {
		t.Fatalf("expected first action to be Add, got %q", got)
	}
	if got := m.quickAddItems[1].Label; got != "Transfer" {
		t.Fatalf("expected second action to be Transfer, got %q", got)
	}
}

func TestMCPManagerBrowseFocusCyclesWithTab(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"docs": {Command: "npx", Enabled: true},
	}})

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.browseFocus != mcpBrowseFocusServers {
		t.Fatalf("expected server list focus after tab, got %v", m.browseFocus)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.browseFocus != mcpBrowseFocusQuickAdd {
		t.Fatalf("expected actions focus after second tab, got %v", m.browseFocus)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.browseFocus != mcpBrowseFocusServers {
		t.Fatalf("expected server list focus after shift+tab, got %v", m.browseFocus)
	}
}

func TestMCPManagerQuickAddEnterStartsEditor(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.editing || !m.adding {
		t.Fatalf("expected add editor to open, editing=%t adding=%t", m.editing, m.adding)
	}
	if got := m.editFields[mcpEditFieldTransport].value; got != "stdio" {
		t.Fatalf("expected stdio transport from first quick add item, got %q", got)
	}
}

func TestMCPManagerRenderQuickAddSection(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})

	left := m.renderServerList()

	for _, want := range []string{"Actions", "Add", "Transfer", "Your Servers"} {
		if !strings.Contains(left, want) {
			t.Fatalf("expected left pane to contain %q, got %q", want, left)
		}
	}
	for _, unwanted := range []string{"Quick Add", "Create MCP Server"} {
		if strings.Contains(left, unwanted) {
			t.Fatalf("expected left pane to omit %q, got %q", unwanted, left)
		}
	}
	if strings.Contains(left, "Load missing servers from Codex") {
		t.Fatalf("expected compact action copy to avoid the old long description, got %q", left)
	}
	if got := strings.Count(left, "Import/Export"); got != 1 {
		t.Fatalf("expected transfer summary to appear once, got count %d in %q", got, left)
	}
}

func TestMCPManagerRenderServerRowIncludesTransportAndSelectionMarker(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"docs": {Command: "npx", Enabled: true},
	}})
	m.selected = 0
	m.browseFocus = mcpBrowseFocusServers

	left := m.renderServerList()

	for _, want := range []string{"docs", "stdio", "➤"} {
		if !strings.Contains(left, want) {
			t.Fatalf("expected left pane to contain %q, got %q", want, left)
		}
	}
}

func TestMCPManagerQuickAddRowsUseWiderPaneWidth(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})

	row := m.renderQuickAddItem(0, m.quickAddItems[0])

	if got := lipgloss.Width(row); got < 36 {
		t.Fatalf("expected quick-add row width to be widened, got %d in %q", got, row)
	}
}

func TestMCPManagerRenderEmptyState(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})

	right := m.renderDetails()

	for _, want := range []string{"No MCP servers yet", "Create MCP Server", "choose transport in the editor"} {
		if !strings.Contains(right, want) {
			t.Fatalf("expected empty state to contain %q, got %q", want, right)
		}
	}
}

func TestMCPManagerQuickAddShortcutOpensUnifiedCreateFlow(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	if !m.editing || !m.adding {
		t.Fatalf("expected add editor to open, editing=%t adding=%t", m.editing, m.adding)
	}
	if got := m.editFields[mcpEditFieldTransport].value; got != "stdio" {
		t.Fatalf("expected unified create flow to default transport to stdio, got %q", got)
	}
}

func TestMCPManagerQuickAddTransferEnterOpensTransferMenu(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})

	m.quickAddIndex = 1
	cmd := m.activateFocusedItem()
	if cmd != nil {
		t.Fatal("expected transfer menu open without immediate command execution")
	}
	if !m.transferring {
		t.Fatal("expected transfer menu to be active")
	}
	if len(m.transferItems) != 4 {
		t.Fatalf("expected 4 transfer items, got %d", len(m.transferItems))
	}
}

func TestMCPManagerRenderOverviewIncludesProbeMetadata(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"docs": {Command: "npx", Args: []string{"-y", "@mcp/server"}, Enabled: true},
	}})
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	m.probes["docs"] = &mcpProbeResult{Stage: mcpProbeStageToolsList, ToolsCount: 3, Latency: 1500 * time.Millisecond, ProbedAt: now}

	right := m.renderDetails()

	for _, want := range []string{"Overview", "Transport: stdio", "Args: 2 configured", "Tools detected: 3", now.Format(time.RFC3339)} {
		if !strings.Contains(right, want) {
			t.Fatalf("expected overview to contain %q, got %q", want, right)
		}
	}
}

func TestMCPManagerRenderDiagnosticsConclusionFirst(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"broken": {Command: "missing-binary", Enabled: true},
	}})
	m.probes["broken"] = &mcpProbeResult{Stage: mcpProbeStageSpawn, Err: "executable file not found in $PATH"}

	right := m.renderDetails()

	for _, want := range []string{"Current State:", "Why:", "Next Action:", "Failure Stage: spawn"} {
		if !strings.Contains(right, want) {
			t.Fatalf("expected diagnostics to contain %q, got %q", want, right)
		}
	}
}

func TestMCPManagerDisabledStatusExplainsIntent(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"docs": {Command: "npx", Enabled: false, DisabledReason: "disabled by spark"},
	}})

	right := m.renderDetails()

	if !strings.Contains(right, "Disabled reason: disabled by spark") {
		t.Fatalf("expected disabled reason in details, got %q", right)
	}
}

func TestMCPManagerRenderActionsAndStatusBarContext(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"docs": {Command: "npx", Enabled: true},
	}})
	m.width = 120

	right := m.renderDetails()
	for _, unwanted := range []string{"Actions", "Transfer"} {
		if strings.Contains(right, unwanted) {
			t.Fatalf("expected details to omit %q, got %q", unwanted, right)
		}
	}

	if got := m.contextHelpText(); !strings.Contains(got, "Enter Run Action") {
		t.Fatalf("expected actions help text, got %q", got)
	}

	m.startEditCurrent()
	if got := m.contextHelpText(); !strings.Contains(got, "Ctrl+S Save") {
		t.Fatalf("expected edit help text, got %q", got)
	}
}

func TestMCPManagerTransferMenuRendersClaudeAndCodexOptions(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.openTransferMenu()

	right := m.renderDetails()

	for _, want := range []string{
		"Transfer MCP Servers",
		"Import from Codex",
		"Import from Claude",
		"Export to Codex",
		"Export to Claude",
	} {
		if !strings.Contains(right, want) {
			t.Fatalf("expected transfer panel to contain %q, got %q", want, right)
		}
	}
}

func TestMCPManagerStatusBarStylesSuccessAndErrorStates(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.width = 120

	m.status = "✓ Imported 2 MCP server(s) from Claude."
	if got := m.renderStatusBar(); !strings.Contains(got, "✓ Imported 2 MCP server(s) from Claude.") {
		t.Fatalf("expected success status in status bar, got %q", got)
	}

	m.status = "✗ Failed to sync MCP servers to Claude."
	if got := m.renderStatusBar(); !strings.Contains(got, "✗ Failed to sync MCP servers to Claude.") {
		t.Fatalf("expected error status in status bar, got %q", got)
	}
}

func TestMCPManagerEditorFlowPreservesBrowseUntilExplicitEdit(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"docs": {Command: "npx", Enabled: true},
	}})

	if m.editing {
		t.Fatal("expected browse mode by default")
	}

	right := m.renderDetails()
	if !strings.Contains(right, "Overview") {
		t.Fatalf("expected overview in browse mode, got %q", right)
	}

	m.startEditCurrent()
	editor := m.renderDetails()
	for _, want := range []string{"Edit Server: docs", "[ Ctrl+S Save ]", "[ Ctrl+P Save & Probe ]"} {
		if !strings.Contains(editor, want) {
			t.Fatalf("expected editor to contain %q, got %q", want, editor)
		}
	}
	if !strings.Contains(editor, "[ Ctrl+S Save ]") || !strings.Contains(editor, "[ Ctrl+P Save & Probe ]") {
		t.Fatalf("expected editor footer actions, got %q", editor)
	}
}

func TestMCPManagerStartAddEditorDefaultsToForm(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	if !m.editing {
		t.Fatal("expected editing mode")
	}
	if m.editorMode != mcpEditorModeForm {
		t.Fatalf("expected form mode, got %v", m.editorMode)
	}
	if got := m.editFields[mcpEditFieldTransport].value; got != "stdio" {
		t.Fatalf("expected stdio transport, got %q", got)
	}
}

func TestMCPManagerSaveFormCreatesServer(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editFields[mcpEditFieldName].value = "git-tools"
	m.editFields[mcpEditFieldCommand].value = "npx"
	m.editFields[mcpEditFieldArgs].value = "-y\n@mcp/sqlite-server"
	m.editFields[mcpEditFieldEnv].value = "DB_PATH=/tmp/data.db"

	cfg, name, err := m.buildEditedServerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	server := cfg.GetMcpServer(name)
	if server == nil {
		t.Fatal("expected saved server")
	}
	if server.Command != "npx" {
		t.Fatalf("expected command npx, got %q", server.Command)
	}
	if len(server.Args) != 2 || server.Args[1] != "@mcp/sqlite-server" {
		t.Fatalf("unexpected args: %v", server.Args)
	}
	if server.Env["DB_PATH"] != "/tmp/data.db" {
		t.Fatalf("unexpected env: %v", server.Env)
	}
}

func TestMCPManagerSaveRawYAMLUpdatesServer(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"git-tools": {Command: "old", Enabled: true},
	}})
	m.startEditCurrent()
	m.editorMode = mcpEditorModeRaw
	m.rawEditor = "command: npx\nargs:\n  - -y\n  - '@mcp/sqlite-server'\nenabled: true\nenv:\n  DB_PATH: /var/lib/data.db\n"

	cfg, name, err := m.buildEditedServerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	server := cfg.GetMcpServer(name)
	if server == nil {
		t.Fatal("expected saved server")
	}
	if server.Command != "npx" {
		t.Fatalf("expected command npx, got %q", server.Command)
	}
	if len(server.Args) != 2 {
		t.Fatalf("unexpected args: %v", server.Args)
	}
	if server.Env["DB_PATH"] != "/var/lib/data.db" {
		t.Fatalf("unexpected env: %v", server.Env)
	}
}

func TestMCPManagerSelectFieldIgnoresFreeText(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editFocus = mcpEditFieldTransport

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	if got := m.editFields[mcpEditFieldTransport].value; got != "stdio" {
		t.Fatalf("expected transport select to ignore free text, got %q", got)
	}
}

func TestMCPManagerSelectFieldCyclesWithArrowKeys(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editFocus = mcpEditFieldTransport

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.editFields[mcpEditFieldTransport].value; got != "sse" {
		t.Fatalf("expected transport to cycle right to sse, got %q", got)
	}

	m.editFocus = mcpEditFieldEnabled
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.editFields[mcpEditFieldEnabled].value; got != "false" {
		t.Fatalf("expected enabled to toggle right to false, got %q", got)
	}
}

func TestMCPManagerTabMovesFieldsWhileArrowKeysMoveTextCursor(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editFocus = mcpEditFieldName
	m.editFields[mcpEditFieldName].value = "figma"
	m.editCursor[mcpEditFieldName] = len([]rune("figma"))

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})

	if got := m.editFields[mcpEditFieldName].value; got != "figXma" {
		t.Fatalf("expected insert at cursor, got %q", got)
	}
	if m.editFocus != mcpEditFieldName {
		t.Fatalf("expected left/right not to change focus, got %d", m.editFocus)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.editFocus != mcpEditFieldTransport {
		t.Fatalf("expected tab to move focus, got %d", m.editFocus)
	}
}

func TestMCPManagerTextFieldSupportsDeleteAndHomeEnd(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editFocus = mcpEditFieldName
	m.editFields[mcpEditFieldName].value = "figma"
	m.editCursor[mcpEditFieldName] = len([]rune("figma"))

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	if got := m.editFields[mcpEditFieldName].value; got != "igma" {
		t.Fatalf("expected delete at cursor to remove first rune, got %q", got)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.editFields[mcpEditFieldName].value; got != "igm" {
		t.Fatalf("expected backspace before cursor to remove last rune, got %q", got)
	}
}

func TestMCPManagerMultilineFieldSupportsVerticalCursorMovement(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editFocus = mcpEditFieldArgs
	m.editFields[mcpEditFieldArgs].value = "abc\ndefg"
	m.editCursor[mcpEditFieldArgs] = len([]rune("abc\ndefg"))

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})

	if got := m.editFields[mcpEditFieldArgs].value; got != "abcX\ndefg" {
		t.Fatalf("expected up to move cursor to prior line before insert, got %q", got)
	}
}

func TestMCPManagerRawEditorSupportsCursorMovement(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editorMode = mcpEditorModeRaw
	m.rawEditor = "abc\ndef"
	m.rawCursor = len([]rune("abc\ndef"))

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})

	if got := m.rawEditor; got != "abcX\ndef" {
		t.Fatalf("expected raw editor insertion at moved cursor, got %q", got)
	}
}

func TestMCPManagerOnlyFocusedFieldRendersCursor(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editFields[mcpEditFieldName].value = "demo"
	m.editFields[mcpEditFieldCommand].value = "auggie"
	m.editCursor[mcpEditFieldName] = 2
	m.editCursor[mcpEditFieldCommand] = 3
	m.editFocus = mcpEditFieldName

	view := m.renderDetails()

	if !strings.Contains(view, "de|mo") {
		t.Fatalf("expected focused field to render cursor, got %q", view)
	}
	if strings.Contains(view, "aug|gie") {
		t.Fatalf("expected unfocused field to omit cursor, got %q", view)
	}
}

func TestMCPManagerSwitchFormToRawSyncsCurrentFields(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editFields[mcpEditFieldName].value = "git-tools"
	m.editFields[mcpEditFieldCommand].value = "npx"
	m.editFields[mcpEditFieldArgs].value = "-y\n@mcp/sqlite-server"

	m.switchEditorMode(mcpEditorModeRaw)

	if m.editorMode != mcpEditorModeRaw {
		t.Fatalf("expected raw mode, got %v", m.editorMode)
	}
	if !strings.Contains(m.rawEditor, "git-tools:") {
		t.Fatalf("expected raw editor to include server name, got %q", m.rawEditor)
	}
	if !strings.Contains(m.rawEditor, `command: "npx"`) {
		t.Fatalf("expected raw editor to include command, got %q", m.rawEditor)
	}
}

func TestMCPManagerSwitchRawToFormParsesRawEditor(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")
	m.editorMode = mcpEditorModeRaw
	m.rawEditor = "git-tools:\n  url: \"https://example.com/mcp\"\n  enabled: false\n"

	m.switchEditorMode(mcpEditorModeForm)

	if m.editorMode != mcpEditorModeForm {
		t.Fatalf("expected form mode, got %v", m.editorMode)
	}
	if got := m.editFields[mcpEditFieldName].value; got != "git-tools" {
		t.Fatalf("expected parsed name, got %q", got)
	}
	if got := m.editFields[mcpEditFieldTransport].value; got != "http" {
		t.Fatalf("expected parsed transport http, got %q", got)
	}
	if got := m.editFields[mcpEditFieldEnabled].value; got != "false" {
		t.Fatalf("expected parsed enabled false, got %q", got)
	}
}

func TestMCPManagerVisibleFieldsFollowTransport(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("stdio")

	visible := m.visibleEditFieldIndices()
	if !containsInt(visible, mcpEditFieldCommand) || !containsInt(visible, mcpEditFieldArgs) || !containsInt(visible, mcpEditFieldEnv) {
		t.Fatalf("expected stdio fields visible, got %v", visible)
	}
	if containsInt(visible, mcpEditFieldURL) {
		t.Fatalf("expected url hidden for stdio, got %v", visible)
	}

	m.editFields[mcpEditFieldTransport].value = "http"
	visible = m.visibleEditFieldIndices()
	if !containsInt(visible, mcpEditFieldURL) {
		t.Fatalf("expected url visible for http, got %v", visible)
	}
	if containsInt(visible, mcpEditFieldCommand) || containsInt(visible, mcpEditFieldArgs) || containsInt(visible, mcpEditFieldEnv) {
		t.Fatalf("expected stdio-only fields hidden for http, got %v", visible)
	}
}

func TestMCPManagerFieldNavigationSkipsHiddenFields(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.startAddEditor("http")
	m.editFocus = mcpEditFieldTransport

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.editFocus != mcpEditFieldEnabled {
		t.Fatalf("expected focus to move to enabled, got %d", m.editFocus)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.editFocus != mcpEditFieldURL {
		t.Fatalf("expected focus to skip hidden stdio fields and move to url, got %d", m.editFocus)
	}
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
