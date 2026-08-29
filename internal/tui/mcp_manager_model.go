package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
		browseFocus: mcpBrowseFocusServers,
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
	if len(m.names) == 0 {
		m.browseFocus = mcpBrowseFocusQuickAdd
	}
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
