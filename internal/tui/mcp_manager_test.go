package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
	"spark/internal/mcp"
)

func newTestMCPCfg() *config.RootConfig {
	cfg := &config.RootConfig{McpServers: map[string]*config.McpServerConfig{
		"codegraph": {Command: "codegraph-mcp", Enabled: true},
		"deepwiki":  {URL: "https://mcp.deepwiki.com/mcp", Enabled: true},
		"github":    {URL: "https://api.githubcopilot.com/mcp/", Enabled: false},
		"figma":     {URL: "https://mcp.figma.com/mcp", Enabled: false},
	}}
	// deepwiki: claude/one explicitly off; others inherit enabled.
	off := false
	cfg.McpServers["deepwiki"].Agents = map[string]*config.McpAgentBinding{
		"claude": {Agent: "claude", Enabled: &off},
		"one":    {Agent: "one", Enabled: &off},
	}
	return cfg
}

func TestMCPMatrixShowsPerAgentEnablement(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.height = 28
	view := m.View()

	// Agent column headers.
	for _, label := range []string{"Codex", "Claude", "One", "Grok", "Agy"} {
		if !strings.Contains(view, label) {
			t.Fatalf("expected agent column %q in matrix, got:\n%s", label, view)
		}
	}
	// All servers listed.
	for _, name := range []string{"codegraph", "deepwiki", "github", "figma"} {
		if !strings.Contains(view, name) {
			t.Fatalf("expected server row %q in list, got:\n%s", name, view)
		}
	}
}

func TestMCPMatrixCellStatuses(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())

	// codegraph enabled everywhere and tested OK → ●
	m.tests["codegraph"] = &mcp.Result{Stage: mcp.StageToolsList, ToolsCount: 3, ProbedAt: time.Now()}
	if got := m.cellStatus("codegraph", "codex").Kind; got != mcp.StatusOK {
		t.Fatalf("codegraph×codex expected OK, got %v", got)
	}

	// deepwiki claude binding disabled → ○ disabled
	if got := m.cellStatus("deepwiki", "claude").Kind; got != mcp.StatusDisabled {
		t.Fatalf("deepwiki×claude expected disabled, got %v", got)
	}
	// deepwiki codex inherits definition enabled → not checked
	if got := m.cellStatus("deepwiki", "codex").Kind; got != mcp.StatusNotChecked {
		t.Fatalf("deepwiki×codex expected NotChecked, got %v", got)
	}
}

func TestMCPListRowGlyphs(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.tests["codegraph"] = &mcp.Result{Stage: mcp.StageToolsList, ToolsCount: 3, ProbedAt: time.Now()}
	m.tests["github"] = &mcp.Result{Stage: mcp.StageInitialize, Err: `exec: "nope": executable file not found in $PATH`, ProbedAt: time.Now()}

	m.selectByName("codegraph")
	row := m.renderListRow(m.selectedIndexOf("codegraph"), 16, m.agents())
	if !strings.Contains(row, "●") {
		t.Fatalf("expected ● for tested-OK enabled server, got %q", row)
	}

	// github is definition-disabled: all cells ○.
	m.selectByName("github")
	row = m.renderListRow(m.selectedIndexOf("github"), 16, m.agents())
	if strings.Contains(row, "●") || strings.Contains(row, "?") || strings.Contains(row, "!") {
		t.Fatalf("expected all ○ for disabled server, got %q", row)
	}
}

func (m *mcpManagerModel) selectedIndexOf(name string) int {
	for i, n := range m.filtered {
		if n == name {
			return i
		}
	}
	return 0
}

func TestMCPToggleAgentBinding(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.selectByName("codegraph")

	// Toggle claude (matrix cursor default 0 = codex; move right to claude).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(*mcpManagerModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = updated.(*mcpManagerModel)

	if m.cfg.McpAgentEnabled("codegraph", "claude") {
		t.Fatal("expected codegraph×claude to be toggled off (was inherited-on)")
	}
	// codex untouched.
	if !m.cfg.McpAgentEnabled("codegraph", "codex") {
		t.Fatal("expected codegraph×codex to stay enabled")
	}

	// Toggle back on.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = updated.(*mcpManagerModel)
	if !m.cfg.McpAgentEnabled("codegraph", "claude") {
		t.Fatal("expected codegraph×claude to be toggled back on")
	}
}

func TestMCPDetailPageShowsDefinitionBindingsAndTest(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.height = 30
	m.selectByName("deepwiki")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*mcpManagerModel)
	if m.page != mcpPageDetail {
		t.Fatalf("expected detail page, got %v", m.page)
	}

	view := m.View()
	for _, want := range []string{"Definition", "Agent bindings", "Last test", "Not checked", "remote · https://mcp.deepwiki.com/mcp"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected detail page to contain %q, got:\n%s", want, view)
		}
	}
	// Per-agent states: codex enabled-not-checked, claude disabled.
	if !strings.Contains(view, "?") {
		t.Fatal("expected ? glyph for enabled-not-checked agent")
	}
}

func TestMCPDetailTestResultErrorActionable(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.height = 30
	m.selectByName("codegraph")
	_ = m.View()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*mcpManagerModel)
	updated, _ = m.Update(mcpTestFinishedMsg{
		Name:   "codegraph",
		Result: &mcp.Result{Stage: mcp.StageSpawn, Err: `exec: "codegraph-mcp": executable file not found in $PATH`, ProbedAt: time.Now()},
		Open:   true,
	})
	m = updated.(*mcpManagerModel)

	view := m.View()
	if !strings.Contains(view, "Command not found") {
		t.Fatalf("expected actionable 'Command not found' headline, got:\n%s", view)
	}
	if !strings.Contains(view, "PATH") {
		t.Fatalf("expected PATH suggestion, got:\n%s", view)
	}
}

func TestMCPDetailShowsToolCountWhenTested(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.height = 30
	m.selectByName("deepwiki")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*mcpManagerModel)

	updated, _ = m.Update(mcpTestFinishedMsg{
		Name:   "deepwiki",
		Result: &mcp.Result{Stage: mcp.StageToolsList, ToolsCount: 7, ToolNames: []string{"read_doc", "search"}, ProbedAt: time.Now()},
	})
	m = updated.(*mcpManagerModel)

	view := m.View()
	if !strings.Contains(view, "7 tool(s)") {
		t.Fatalf("expected tool count on detail page, got:\n%s", view)
	}
	// No fabricated tool counts elsewhere: github has no test result.
	if strings.Contains(view, "github") && strings.Count(view, "tool(s)") != 1 {
		t.Fatalf("expected exactly one tool-count mention, got:\n%s", view)
	}
}

func TestMCPBindingEditorSavesOverrides(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.height = 30
	m.selectByName("deepwiki")

	// Enter detail, move to claude (index 1), enter binding editor.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*mcpManagerModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(*mcpManagerModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*mcpManagerModel)

	if m.page != mcpPageBinding || m.bindAgent != "claude" {
		t.Fatalf("expected claude binding editor, page=%v agent=%q", m.page, m.bindAgent)
	}
	view := m.View()
	for _, want := range []string{"Enabled", "Command", "URL", "Environment"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected binding editor field %q, got:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "Codex") && !strings.Contains(view, "Codex advanced") {
		// Codex advanced fields exist on the form regardless of agent.
		t.Fatalf("expected codex advanced fields present in form, got:\n%s", view)
	}

	// Save without changes returns to detail.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(*mcpManagerModel)
	if m.page != mcpPageDetail {
		t.Fatalf("expected return to detail after save, got %v", m.page)
	}
}

func TestMCPAddModalFourChoices(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{McpServers: map[string]*config.McpServerConfig{}})
	m.width = 100
	m.height = 26

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(*mcpManagerModel)
	if m.modalKind != mcpModalAdd {
		t.Fatalf("expected add modal, got %v", m.modalKind)
	}
	view := m.View()
	for _, want := range []string{"Paste config", "Local command", "Remote URL", "Import existing"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected add option %q, got:\n%s", want, view)
		}
	}
	// No format knowledge on the first layer.
	if !strings.Contains(view, "auto-detected") {
		t.Fatalf("expected format auto-detection hint, got:\n%s", view)
	}

	// Paste choice opens paste modal.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*mcpManagerModel)
	if m.modalKind != mcpModalPaste {
		t.Fatalf("expected paste modal, got %v", m.modalKind)
	}
}

func TestMCPTransferRemovedFromMainUI(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.height = 28
	view := m.View()

	if strings.Contains(view, "Transfer") {
		t.Fatalf("Transfer must not appear on the main screen, got:\n%s", view)
	}
	if strings.Contains(view, "Command") && strings.Contains(view, "Arguments") {
		t.Fatalf("config form fields must not be on the main screen, got:\n%s", view)
	}
}

func TestMCPSearchFiltersList(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.searchQuery = "deep"
	m.refreshFiltered()

	if len(m.filtered) != 1 || m.filtered[0] != "deepwiki" {
		t.Fatalf("expected only deepwiki, got %v", m.filtered)
	}
}

func TestMCPFooterShowsActions(t *testing.T) {
	m := newMCPManagerModel(newTestMCPCfg())
	m.width = 110
	m.height = 28
	view := m.View()
	for _, want := range []string{"Enter details", "toggle agent binding", "T test", "A add", "I import", "/ search"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected footer hint %q, got:\n%s", want, view)
		}
	}
}

func TestMCPSnapshotsRender(t *testing.T) {
	cfg := newTestMCPCfg()

	overview, err := RenderMCPManagerSnapshot(cfg, 110, 28, "")
	if err != nil {
		t.Fatalf("overview snapshot: %v", err)
	}
	if !strings.Contains(overview, "Codex") || !strings.Contains(overview, "codegraph") {
		t.Fatalf("overview snapshot missing matrix, got:\n%s", overview)
	}

	detail, err := RenderMCPManagerSnapshot(cfg, 110, 30, "detail")
	if err != nil {
		t.Fatalf("detail snapshot: %v", err)
	}
	if !strings.Contains(detail, "Agent bindings") {
		t.Fatalf("detail snapshot missing bindings, got:\n%s", detail)
	}

	binding, err := RenderMCPManagerSnapshot(cfg, 110, 30, "binding-codex")
	if err != nil {
		t.Fatalf("binding snapshot: %v", err)
	}
	if !strings.Contains(binding, "Command") {
		t.Fatalf("binding snapshot missing fields, got:\n%s", binding)
	}
}

func TestMCPNotCheckedNotUnknownWording(t *testing.T) {
	status := mcp.Summarize(&config.McpServerConfig{Command: "x", Enabled: true}, nil)
	if status.Headline != "Not checked" {
		t.Fatalf("expected 'Not checked', got %q", status.Headline)
	}
}
