package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
	"spark/internal/mcp"
)

// MCP Manager: single server list + Agent Matrix on the main screen, with a
// detail page per server and per-agent binding editors reached from there.
// Transport/config forms never live on the main screen.

type mcpPage int

const (
	mcpPageList mcpPage = iota
	mcpPageDetail
	mcpPageBinding
)

type mcpModalKind int

const (
	mcpModalNone       mcpModalKind = iota
	mcpModalAdd                     // Add flow: Paste config / Local command / Remote URL / Import existing
	mcpModalPaste                   // paste buffer (auto-detect JSON/YAML/TOML)
	mcpModalImportPeer              // import from codex/claude existing configs
	mcpModalDeleteConfirm
	mcpModalTestDetail // detailed test result (tools list)
)

type mcpFieldKind int

const (
	mcpFieldKindInput mcpFieldKind = iota
	mcpFieldKindSelect
	mcpFieldKindTextarea
)

type mcpFormField struct {
	Key         string
	Label       string
	Value       string
	Placeholder string
	Kind        mcpFieldKind
	Options     []string
	Input       textinput.Model
}

// Detail page sections focus.
type mcpDetailFocus int

const (
	mcpDetailFocusBindings mcpDetailFocus = iota // agent binding rows
	mcpDetailFocusActions                        // Test / Edit definition / Delete
)

// Binding page field indices.
const (
	bindFieldEnabled = 0
	bindFieldCommand = 1
	bindFieldArgs    = 2
	bindFieldURL     = 3
	bindFieldEnv     = 4
	bindFieldStartup = 5
	bindFieldTool    = 6
	bindFieldEnableT = 7
	bindFieldDisablT = 8
	bindFieldCount   = 9
)

type mcpTestFinishedMsg struct {
	Name   string
	Result *mcp.Result
	Open   bool
}

type mcpSaveFinishedMsg struct {
	Status string
	Err    error
	Cfg    *config.RootConfig
}

type mcpExternalEditorFinishedMsg struct {
	Target string // "paste" | "definition" | server name
	Path   string
	Err    error
}

type mcpManagerModel struct {
	cfg *config.RootConfig

	// List page
	names    []string
	filtered []string
	selected int

	// Search
	searchQuery string
	searching   bool

	// Pages
	page         mcpPage
	detailFocus  mcpDetailFocus
	detailCursor int // index into agents on detail page (bindings) or actions
	bindAgent    string
	bindFields   []mcpFormField
	bindCursor   int

	// Modals
	modalKind   mcpModalKind
	modalCursor int
	addChoice   int
	pasteBuffer string
	pasteCursor int
	importPeer  int

	// Definition editor (detail page) — common fields of the definition.
	defFields []mcpFormField
	defCursor int

	// Tests
	tests   map[string]*mcp.Result
	testing map[string]bool
	testAll bool

	// Matrix cursor: which agent column Space toggles (index into agents()).
	matrixCursor int

	width  int
	height int
	status string
}

func ManageMCPDashboard(cfg *config.RootConfig) error {
	m := newMCPManagerModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newMCPManagerModel(cfg *config.RootConfig) *mcpManagerModel {
	if cfg == nil {
		cfg = &config.RootConfig{}
	}
	config.Normalize(cfg)
	m := &mcpManagerModel{
		cfg:     cfg,
		tests:   map[string]*mcp.Result{},
		testing: map[string]bool{},
	}
	m.refreshNames()
	return m
}

func (m *mcpManagerModel) Init() tea.Cmd { return nil }

func (m *mcpManagerModel) agents() []string { return config.McpAgents() }

func (m *mcpManagerModel) agentLabel(agent string) string {
	switch agent {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude"
	case "one":
		return "One"
	case "grok":
		return "Grok"
	case "agy":
		return "Agy"
	default:
		return agent
	}
}

// currentName returns the selected server name on the list page (or the
// server whose detail/binding page is open).
func (m *mcpManagerModel) currentName() string {
	if len(m.filtered) == 0 {
		return ""
	}
	if m.selected < 0 || m.selected >= len(m.filtered) {
		m.selected = 0
	}
	return m.filtered[m.selected]
}

func (m *mcpManagerModel) currentServer() *config.McpServerConfig {
	return m.cfg.GetMcpServer(m.currentName())
}

// cellStatus computes the matrix cell state for one server×agent.
func (m *mcpManagerModel) cellStatus(name, agent string) mcp.Status {
	if !m.cfg.McpAgentEnabled(name, agent) {
		return mcp.Status{Kind: mcp.StatusDisabled}
	}
	eff := m.cfg.McpEffectiveServer(name, agent)
	return mcp.Summarize(eff, m.tests[name])
}

// matrixIssues counts server×agent cells that are enabled but failing.
func (m *mcpManagerModel) matrixIssues() int {
	issues := 0
	for name := range m.cfg.McpServers {
		for _, agent := range m.agents() {
			status := m.cellStatus(name, agent)
			if status.Kind == mcp.StatusError {
				issues++
			}
		}
	}
	return issues
}

// headerSummary renders "MCP · N servers · M issues".
func (m *mcpManagerModel) headerSummary() string {
	total := len(m.cfg.McpServers)
	issues := m.matrixIssues()
	parts := []string{fmt.Sprintf("%d servers", total)}
	if issues > 0 {
		parts = append(parts, fmt.Sprintf("%d issues", issues))
	} else {
		parts = append(parts, "no issues")
	}
	return strings.Join(parts, " · ")
}

func (m *mcpManagerModel) refreshNames() {
	m.names = make([]string, 0, len(m.cfg.McpServers))
	for name := range m.cfg.McpServers {
		m.names = append(m.names, name)
	}
	// Sort: servers with issues first, then enabled-anywhere, then name.
	sort.SliceStable(m.names, func(i, j int) bool {
		ri, rj := mcpServerRank(m.names[i], m), mcpServerRank(m.names[j], m)
		if ri != rj {
			return ri < rj
		}
		return m.names[i] < m.names[j]
	})
	m.refreshFiltered()
}

func mcpServerRank(name string, m *mcpManagerModel) int {
	issueRank := 2
	for _, agent := range m.agents() {
		switch m.cellStatus(name, agent).Kind {
		case mcp.StatusError:
			return 0
		case mcp.StatusNotChecked:
			issueRank = 1
		}
	}
	if issueRank < 2 {
		return issueRank
	}
	if m.cfg.McpEnabledAgentCount(name) == 0 {
		return 3
	}
	return 2
}

func (m *mcpManagerModel) refreshFiltered() {
	prev := m.currentName()
	if m.searchQuery == "" {
		m.filtered = append([]string{}, m.names...)
	} else {
		m.filtered = nil
		q := strings.ToLower(strings.TrimSpace(m.searchQuery))
		for _, name := range m.names {
			server := m.cfg.GetMcpServer(name)
			match := strings.Contains(strings.ToLower(name), q)
			if !match && server != nil {
				match = strings.Contains(strings.ToLower(server.Command), q) ||
					strings.Contains(strings.ToLower(server.URL), q)
			}
			if match {
				m.filtered = append(m.filtered, name)
			}
		}
	}
	if len(m.filtered) == 0 {
		m.selected = 0
	} else if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}
	// Re-sorting can move the previously selected server; keep the cursor on it.
	if prev != "" {
		for i, n := range m.filtered {
			if n == prev {
				m.selected = i
				break
			}
		}
	}
}

func (m *mcpManagerModel) selectByName(name string) {
	for i, n := range m.filtered {
		if n == name {
			m.selected = i
			return
		}
	}
}

// matrixCellGlyph returns the glyph shown in the agent matrix for a status.
func matrixCellGlyph(status mcp.Status) string {
	switch status.Kind {
	case mcp.StatusOK:
		return "●" // enabled & tested OK
	case mcp.StatusError:
		return "!" // enabled but failing
	case mcp.StatusNotChecked:
		return "?" // enabled but never tested
	default: // disabled
		return "○"
	}
}

// renderCell returns the styled glyph for one server×agent matrix cell.
func (m *mcpManagerModel) renderCell(name, agent string) string {
	status := m.cellStatus(name, agent)
	glyph := matrixCellGlyph(status)
	switch status.Kind {
	case mcp.StatusOK:
		return lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render(glyph)
	case mcp.StatusError:
		return lipgloss.NewStyle().Foreground(colorError).Bold(true).Render(glyph)
	case mcp.StatusNotChecked:
		return lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render(glyph)
	default:
		return lipgloss.NewStyle().Foreground(colorDim).Render(glyph)
	}
}

func (m *mcpManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case mcpTestFinishedMsg:
		delete(m.testing, msg.Name)
		if msg.Result != nil {
			m.tests[msg.Name] = msg.Result
			status := mcp.Summarize(m.cfg.GetMcpServer(msg.Name), msg.Result)
			m.status = fmt.Sprintf("%s → %s", msg.Name, status.Headline)
		}
		if msg.Open {
			m.modalKind = mcpModalTestDetail
		}
		if m.testAll && len(m.testing) == 0 {
			m.testAll = false
			m.refreshNames()
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
		return m, nil
	case mcpExternalEditorFinishedMsg:
		return m, m.handleExternalEditorFinished(msg)
	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// saveCfgCmd persists the model's config copy.
func (m *mcpManagerModel) saveCfgCmd(status string) tea.Cmd {
	cfgCopy := *m.cfg
	cfgCopy.McpServers = make(map[string]*config.McpServerConfig, len(m.cfg.McpServers))
	for k, v := range m.cfg.McpServers {
		cfgCopy.McpServers[k] = config.CloneMcpServerConfig(v)
	}
	return func() tea.Msg {
		if err := config.Save(&cfgCopy); err != nil {
			return mcpSaveFinishedMsg{Err: err}
		}
		return mcpSaveFinishedMsg{Status: status, Cfg: &cfgCopy}
	}
}

// testCurrentCmd runs a server-level test.
func (m *mcpManagerModel) testServerCmd(name string, open bool) tea.Cmd {
	server := m.cfg.GetMcpServer(name)
	if server == nil {
		return nil
	}
	m.testing[name] = true
	m.status = "Testing " + name + "..."
	def := config.CloneMcpServerConfig(server)
	return func() tea.Msg {
		return mcpTestFinishedMsg{Name: name, Result: mcp.Test(def), Open: open}
	}
}

// testAllCmd tests every configured server.
func (m *mcpManagerModel) testAllCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, name := range m.names {
		if m.testing[name] {
			continue
		}
		cmds = append(cmds, m.testServerCmd(name, false))
	}
	if len(cmds) == 0 {
		return nil
	}
	m.testAll = true
	m.status = "Testing all MCP servers..."
	return tea.Batch(cmds...)
}

// timestamp formatting helper for detail pages.
func fmtTestAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	age := time.Since(t).Round(time.Second)
	return age.String() + " ago"
}
