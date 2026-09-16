package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
		t.Fatalf("expected suggestions to mention command, got %v", status.Suggestions)
	}
}

func TestDiagnoseMCPFailureExplainsMissingBinary(t *testing.T) {
	server := &config.McpServerConfig{Command: "npx-missing", Enabled: true}
	probe := &mcpProbeResult{
		Stage: mcpProbeStageSpawn,
		Err:   `exec: "npx-missing": executable file not found in $PATH`,
	}
	sugs := diagnoseMCPFailure(server, probe)
	if len(sugs) == 0 {
		t.Fatal("expected suggestions for missing executable")
	}
	if !strings.Contains(sugs[0], "not found in PATH") {
		t.Fatalf("expected PATH suggestion, got %q", sugs[0])
	}
}

func TestMCPManagerProbeResultUpdatesSelectionStatus(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"docs": {Command: "npx", Enabled: true},
		},
	})

	now := time.Now()
	updated, _ := m.Update(mcpProbeFinishedMsg{
		Name: "docs",
		Result: &mcpProbeResult{
			Stage:      mcpProbeStageToolsList,
			ToolsCount: 2,
			ToolNames:  []string{"read_doc", "search_doc"},
			Latency:    120 * time.Millisecond,
			ProbedAt:   now,
		},
	})
	m = updated.(*mcpManagerModel)

	status := m.currentStatus()
	if status.Kind != mcpStatusReachable {
		t.Fatalf("expected reachable status, got %v", status.Kind)
	}
	if !strings.Contains(status.Headline, "ok (2 tools)") {
		t.Fatalf("expected tools count in headline, got %q", status.Headline)
	}
}

func TestMCPManagerHeaderSummaryCounts(t *testing.T) {
	cfg := &config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"healthy-server":  {Command: "npx", Enabled: true},
			"disabled-server": {Command: "node", Enabled: false},
			"broken-server":   {Command: "broken", Enabled: true},
			"unknown-server":  {Command: "unknown", Enabled: true},
		},
	}
	m := newMCPManagerModel(cfg)
	m.width = 140
	m.probes["healthy-server"] = &mcpProbeResult{ToolsCount: 3}
	m.probes["broken-server"] = &mcpProbeResult{Err: "executable not found"}

	summary := m.renderHeaderSummary()
	if !strings.Contains(summary, "4 servers") {
		t.Fatalf("expected 4 servers in summary, got %q", summary)
	}
	if !strings.Contains(summary, "1 healthy") {
		t.Fatalf("expected 1 healthy, got %q", summary)
	}
	if !strings.Contains(summary, "1 error") {
		t.Fatalf("expected 1 error, got %q", summary)
	}
	if !strings.Contains(summary, "1 disabled") {
		t.Fatalf("expected 1 disabled, got %q", summary)
	}
	if !strings.Contains(summary, "1 unknown") {
		t.Fatalf("expected 1 unknown, got %q", summary)
	}
}

func TestMCPManagerErrorFirstPrioritySorting(t *testing.T) {
	cfg := &config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"aaa-healthy":  {Command: "npx", Enabled: true},
			"bbb-disabled": {Command: "node", Enabled: false},
			"ccc-error":    {Command: "bad", Enabled: true},
			"ddd-unknown":  {Command: "wait", Enabled: true},
		},
	}
	m := newMCPManagerModel(cfg)
	m.probes["aaa-healthy"] = &mcpProbeResult{ToolsCount: 1}
	m.probes["ccc-error"] = &mcpProbeResult{Err: "crash"}

	m.refreshNames()

	if len(m.names) != 4 {
		t.Fatalf("expected 4 names, got %d", len(m.names))
	}
	if m.names[0] != "ccc-error" {
		t.Fatalf("expected ccc-error to be first (priority 0), got %q", m.names[0])
	}
	if m.names[1] != "ddd-unknown" {
		t.Fatalf("expected ddd-unknown to be second (priority 1), got %q", m.names[1])
	}
	if m.names[2] != "aaa-healthy" {
		t.Fatalf("expected aaa-healthy to be third (priority 2), got %q", m.names[2])
	}
	if m.names[3] != "bbb-disabled" {
		t.Fatalf("expected bbb-disabled to be last (priority 4), got %q", m.names[3])
	}
}

func TestMCPManagerSearchFiltering(t *testing.T) {
	cfg := &config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"alpha-node":   {Command: "npx", Enabled: true},
			"beta-python":  {Command: "python3", Enabled: true},
			"gamma-remote": {URL: "https://example.com", Enabled: true},
		},
	}
	m := newMCPManagerModel(cfg)
	m.searchQuery = "python"
	m.refreshFiltered()

	if len(m.filtered) != 1 || m.filtered[0] != "beta-python" {
		t.Fatalf("expected only beta-python, got %v", m.filtered)
	}

	m.searchQuery = "example.com"
	m.refreshFiltered()
	if len(m.filtered) != 1 || m.filtered[0] != "gamma-remote" {
		t.Fatalf("expected gamma-remote by url, got %v", m.filtered)
	}
}

func TestMCPManagerFilterCycle(t *testing.T) {
	cfg := &config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"healthy-server": {Command: "npx", Enabled: true},
			"broken-server":  {Command: "bad", Enabled: true},
			"remote-server":  {URL: "https://remote.com", Enabled: true},
		},
	}
	m := newMCPManagerModel(cfg)
	m.probes["healthy-server"] = &mcpProbeResult{ToolsCount: 1}
	m.probes["broken-server"] = &mcpProbeResult{Err: "fail"}

	m.filterOption = mcpFilterError
	m.refreshFiltered()
	if len(m.filtered) != 1 || m.filtered[0] != "broken-server" {
		t.Fatalf("expected only broken-server for error filter, got %v", m.filtered)
	}

	m.filterOption = mcpFilterHTTP
	m.refreshFiltered()
	if len(m.filtered) != 1 || m.filtered[0] != "remote-server" {
		t.Fatalf("expected only remote-server for HTTP filter, got %v", m.filtered)
	}
}

func TestMCPManagerRenderServerRowSingleLineFormat(t *testing.T) {
	cfg := &config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"deepwiki": {URL: "https://mcp.deepwiki.com/mcp", Enabled: true},
		},
	}
	m := newMCPManagerModel(cfg)
	rendered := m.renderServerRow(0, "deepwiki", 40)

	if strings.Contains(rendered, "\n") {
		t.Fatalf("expected single line row render, got multi-line: %q", rendered)
	}
	if !strings.Contains(rendered, "deepwiki") || !strings.Contains(strings.ToLower(rendered), "http") || !strings.Contains(strings.ToLower(rendered), "on") {
		t.Fatalf("expected formatted server row, got %q", rendered)
	}
}

func TestMCPManagerRenderEmptyState(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	view := m.renderEmptyState()

	if !strings.Contains(view, "No MCP servers configured") {
		t.Fatalf("expected empty state message, got %q", view)
	}
	if !strings.Contains(view, "Click [Add]") {
		t.Fatalf("expected quick start hints, got %q", view)
	}
}

func TestMCPManagerDirectKeyActionsAndStatusBarContext(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"docs": {Command: "npx", Enabled: true},
		},
	})
	m.width = 120
	m.height = 30
	_ = m.View()

	// 1. Toggle with Space
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if m.cfg.McpServers["docs"].Enabled {
		t.Fatal("expected docs server to be toggled off")
	}

	// 2. Open Add Modal via Mouse Click on Add button
	addBtnY := m.leftContentY + m.leftButtonsRelY
	m.Update(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.leftContentX + 2,
		Y:    addBtnY,
	})
	if m.modalKind != mcpModalAdd {
		t.Fatalf("expected add modal kind, got %v", m.modalKind)
	}

	// 3. Close Modal with Esc
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.modalKind != mcpModalNone {
		t.Fatalf("expected modal closed, got %v", m.modalKind)
	}

	// 4. Tab rotates focus to fields
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focusArea != mcpFocusFields {
		t.Fatalf("expected focus on fields, got %v", m.focusArea)
	}
}

func TestMCPManagerTransferMenuRendersClaudeAndCodexOptions(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.width = 120
	m.openTransferModal()

	modalView := m.renderTransferModal()

	for _, want := range []string{
		"Transfer MCP Servers",
		"Paste JSON / YAML / TOML",
		"Import from Codex",
		"Import from Claude",
		"Export to Codex",
		"Export to Claude",
	} {
		if !strings.Contains(modalView, want) {
			t.Fatalf("expected transfer panel to contain %q, got %q", want, modalView)
		}
	}
}

func TestMCPManagerImportJSONYAMLAndTOMLModal(t *testing.T) {
	// 1. Test JSON with mcpServers wrapper
	jsonSnippet := `{"mcpServers": {"sqlite-node": {"command": "npx", "args": ["-y", "@mcp/server-sqlite"]}}}`
	servers, err := parseMultipleMCPServersRaw(jsonSnippet, "test")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(servers) != 1 || servers["sqlite-node"] == nil {
		t.Fatalf("expected parsed sqlite-node, got %v", servers)
	}
	if servers["sqlite-node"].Command != "npx" {
		t.Fatalf("expected command npx, got %q", servers["sqlite-node"].Command)
	}

	// 2. Test YAML direct multi-server snippet
	yamlSnippet := `
figma:
  url: "https://mcp.figma.com/mcp"
  enabled: true
git-tools:
  command: "npx"
  args: ["-y", "git-mcp"]
`
	serversYaml, err := parseMultipleMCPServersRaw(yamlSnippet, "test")
	if err != nil {
		t.Fatalf("unexpected yaml parse error: %v", err)
	}
	if len(serversYaml) != 2 {
		t.Fatalf("expected 2 parsed servers, got %d", len(serversYaml))
	}
	if serversYaml["figma"].URL != "https://mcp.figma.com/mcp" {
		t.Fatalf("expected figma URL, got %q", serversYaml["figma"].URL)
	}

	// 3. Test TOML format (Codex config format: [mcp_servers.name] and [mcpServers.name])
	tomlSnippet := `
[mcp_servers.context-mode]
command = "uvx"
args = ["context-mode-mcp"]
enabled = true

[mcp_servers.remote-sse]
url = "https://mcp.example.com/sse"
enabled = true
`
	serversToml, err := parseMultipleMCPServersRaw(tomlSnippet, "test")
	if err != nil {
		t.Fatalf("unexpected toml parse error: %v", err)
	}
	if len(serversToml) != 2 {
		t.Fatalf("expected 2 parsed servers from TOML, got %d (%v)", len(serversToml), serversToml)
	}
	if serversToml["context-mode"].Command != "uvx" {
		t.Fatalf("expected context-mode command uvx, got %q", serversToml["context-mode"].Command)
	}
	if serversToml["remote-sse"].URL != "https://mcp.example.com/sse" {
		t.Fatalf("expected remote-sse url, got %q", serversToml["remote-sse"].URL)
	}

	// 4. Test Mouse Click opening Import Modal
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.width = 100
	m.height = 30
	_ = m.View()
	importBtnY := m.leftContentY + m.leftButtonsRelY
	m.Update(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.leftContentX + m.leftAddBtnW + 2,
		Y:    importBtnY,
	})
	if m.modalKind != mcpModalImport {
		t.Fatalf("expected screen to be import modal, got %v", m.modalKind)
	}
	modalView := m.renderImportModal()
	if !strings.Contains(modalView, "Import MCP Servers") || !strings.Contains(modalView, "Import") || !strings.Contains(modalView, "Open in Editor") {
		t.Fatalf("expected import modal view, got %q", modalView)
	}

	// 5. Test File Path Import parsing
	tmpFile, err := os.CreateTemp("", "spark-mcp-test-*.toml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	_, _ = tmpFile.WriteString(tomlSnippet)
	_ = tmpFile.Close()

	serversFromFile, err := parseMultipleMCPServersRaw(tmpFile.Name(), "test")
	if err != nil {
		t.Fatalf("failed to parse servers from file path: %v", err)
	}
	if len(serversFromFile) != 2 || serversFromFile["context-mode"] == nil {
		t.Fatalf("expected parsed servers from file path, got %v", serversFromFile)
	}
}

func TestMCPManagerSaveFormCreatesServer(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.draftFields = newMCPFormFields("git-tools", &config.McpServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@mcp/sqlite-server"},
		Env:     map[string]string{"DB_PATH": "/tmp/data.db"},
		Enabled: true,
	})

	m.saveDraft(false)

	server := m.cfg.GetMcpServer("git-tools")
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

func TestMCPManagerComponentViewRendering(t *testing.T) {
	cfg := &config.RootConfig{
		McpServers: map[string]*config.McpServerConfig{
			"deepwiki": {
				URL:     "https://mcp.deepwiki.com/mcp",
				Enabled: true,
			},
			"augment-context": {
				Command: "npx",
				Enabled: false,
			},
			"context-mode": {
				Command: "npx",
				Enabled: false,
			},
			"figma": {
				URL:     "https://figma.com/mcp",
				Enabled: false,
			},
			"n8n": {
				Command: "npx",
				Enabled: false,
			},
		},
	}
	m := newMCPManagerModel(cfg)
	m.width = 110
	m.height = 28

	view := m.View()
	t.Log("\n" + view)

	// 1. Verify headers & sections
	if !strings.Contains(view, "MCP Manager") {
		t.Fatalf("expected header title, got: %s", view)
	}
	if !strings.Contains(view, "Configuration") {
		t.Fatalf("expected section header 'Configuration', got: %s", view)
	}
	if !strings.Contains(view, "Actions") {
		t.Fatalf("expected section header 'Actions', got: %s", view)
	}
	if !strings.Contains(view, "Diagnostics") {
		t.Fatalf("expected section header 'Diagnostics', got: %s", view)
	}

	// 2. Verify left pane bottom buttons
	if !strings.Contains(view, "Add") || !strings.Contains(view, "Import") {
		t.Fatalf("expected clean left pane buttons, got: %s", view)
	}
	if !strings.Contains(view, "Transfer") || !strings.Contains(view, "Refresh") {
		t.Fatalf("expected clean left pane transfer/refresh buttons, got: %s", view)
	}

	// 3. Verify segmented pills rendering
	if !strings.Contains(view, "http") || !strings.Contains(view, "stdio") {
		t.Fatalf("expected transport options, got: %s", view)
	}

	// 4. Test field navigation and textinput typing
	m.focusArea = mcpFocusFields
	m.focusField = 0 // Name field
	m.updateFocus()

	// Type a character into name
	m.handleFieldsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-v2")})
	if !strings.Contains(m.draftFields[mcpFieldKeyName].Value, "-v2") {
		t.Fatalf("expected updated draft value from textinput component, got %q", m.draftFields[mcpFieldKeyName].Value)
	}

	// 5. Test cycling select field (Transport)
	m.focusField = 1 // Transport field
	m.updateFocus()
	origTransport := m.draftFields[mcpFieldKeyTransport].Value
	m.handleFieldsKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.draftFields[mcpFieldKeyTransport].Value == origTransport {
		t.Fatalf("expected transport to cycle from %q", origTransport)
	}

	// 6. Test Mouse Interactions (Profile-style)
	// 6.1 Click on server row in left pane
	firstRowY := m.leftContentY + m.leftVisibleRows[0]
	m.Update(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    m.leftContentX + 3,
		Y:    firstRowY,
	})
	if m.selected != 0 || m.focusArea != mcpFocusList {
		t.Fatalf("expected left row click to select server 0 and focus list, got selected=%d focusArea=%d", m.selected, m.focusArea)
	}

	// 6.2 Click on input field in right panel
	nameFieldY := m.rightContentY + m.fieldStartRelY[0]
	inputStartX := m.rightContentX + pmLabelWidth + 2
	m.Update(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    inputStartX + 5,
		Y:    nameFieldY,
	})
	if m.focusArea != mcpFocusFields || m.focusField != 0 {
		t.Fatalf("expected click on field to focus fields and field 0, got focusArea=%d focusField=%d", m.focusArea, m.focusField)
	}

	// 6.3 Click on segmented pill field (e.g. Transport field 1)
	transportFieldY := m.rightContentY + m.fieldStartRelY[1]
	oldTrans := m.draftFields[mcpFieldKeyTransport].Value
	m.Update(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    inputStartX + 5,
		Y:    transportFieldY,
	})
	if m.draftFields[mcpFieldKeyTransport].Value == oldTrans {
		t.Fatalf("expected clicking segmented pill field to cycle value from %q", oldTrans)
	}

	// 6.4 Click outside modal to close modal
	m.modalKind = mcpModalAdd
	m.modalX = 20
	m.modalY = 5
	m.modalW = 50
	m.modalH = 15
	m.Update(tea.MouseMsg{
		Type: tea.MouseRelease,
		X:    0,
		Y:    0,
	})
	if m.modalKind != mcpModalNone {
		t.Fatalf("expected click outside modal to dismiss modal, got %v", m.modalKind)
	}
}
