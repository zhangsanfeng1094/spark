package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
)

// mcpFocusArea defines which area of the workbench currently has keyboard focus.
type mcpFocusArea int

const (
	mcpFocusList mcpFocusArea = iota
	mcpFocusFields
	mcpFocusActions
)

// mcpModalKind defines active popups/modals.
type mcpModalKind int

const (
	mcpModalNone mcpModalKind = iota
	mcpModalAdd
	mcpModalImport
	mcpModalTransfer
	mcpModalDeleteConfirm
	mcpModalProbeDetail
)

// mcpStatusKind represents the health status of an MCP server.
type mcpStatusKind int

const (
	mcpStatusUnknown mcpStatusKind = iota
	mcpStatusConfigured
	mcpStatusReachable
	mcpStatusBroken
)

// mcpFilterOption represents filtering mode.
type mcpFilterOption int

const (
	mcpFilterAll mcpFilterOption = iota
	mcpFilterHealthy
	mcpFilterError
	mcpFilterUnknown
	mcpFilterDisabled
	mcpFilterStdio
	mcpFilterHTTP
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
	ToolNames  []string
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
	Name       string
	Result     *mcpProbeResult
	OpenResult bool
}

type mcpExternalEditorFinishedMsg struct {
	Target string // "import" or server name
	Path   string
	Err    error
}

type mcpSaveFinishedMsg struct {
	Status     string
	Err        error
	Cfg        *config.RootConfig
	ProbeName  string
	Result     *mcpProbeResult
	OpenResult bool
}

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
	Required    bool
	ReadOnly    bool
	Input       textinput.Model
}

const (
	mcpFieldKeyName      = 0
	mcpFieldKeyTransport = 1
	mcpFieldKeyEnabled   = 2
	mcpFieldKeyCommand   = 3
	mcpFieldKeyArgs      = 4
	mcpFieldKeyURL       = 5
	mcpFieldKeyEnv       = 6
	mcpFieldKeyReason    = 7
)

type mcpTransferItem struct {
	Key         string
	Label       string
	Description string
}

// mcpManagerModel is the clean, robust state model for the MCP Manager workbench.
type mcpManagerModel struct {
	cfg      *config.RootConfig
	names    []string
	filtered []string
	selected int

	// Focus Management
	focusArea  mcpFocusArea
	focusField int // index in visibleFields
	actionIdx  int // 0: Probe, 1: Save, 2: Vim, 3: Delete

	// Search & Filter
	searchQuery  string
	searching    bool
	filterOption mcpFilterOption

	// Modal State
	modalKind   mcpModalKind
	modalCursor int

	// Add Modal drafts
	addName      string
	addTransport int // 0: stdio, 1: http, 2: sse

	// Import Modal draft
	importBuffer string
	importCursor int

	// Active drafts for current selected server
	draftFields []mcpFormField
	fieldCursor map[int]int
	dirty       bool

	// Probes
	probes  map[string]*mcpProbeResult
	running map[string]bool

	// UI Layout dimensions & status
	width          int
	height         int
	status         string
	leftPanelWidth int
	inputWidth     int

	// Mouse interaction coordinates and layout hitboxes
	leftContentX       int
	leftContentY       int
	leftVisibleRows    []int
	leftVisibleIdxs    []int
	leftButtonsRelY    int
	leftButtonsRelH    int
	leftButtonsRowW    int
	leftButtonsRow2Y   int
	leftButtonsRow2W   int
	leftAddBtnW        int
	leftImportBtnW     int
	leftTransferBtnW   int
	leftRefreshBtnW    int
	rightContentX      int
	rightContentY      int
	fieldStartRelY     []int
	fieldEndRelY       []int
	fieldActualIndices []int
	rightButtonsRelY   int
	rightButtonsRelH   int
	rightButtonsRowW   int
	rightProbeBtnW     int
	rightSaveBtnW      int
	rightVimBtnW       int
	rightDeleteBtnW    int
	modalX             int
	modalY             int
	modalW             int
	modalH             int
	modalOptionStartY  int

	// Transfer Items
	transferItems []mcpTransferItem
	transferIndex int
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
		cfg:         cfg,
		focusArea:   mcpFocusList,
		probes:      map[string]*mcpProbeResult{},
		running:     map[string]bool{},
		fieldCursor: map[int]int{},
		transferItems: []mcpTransferItem{
			{Key: "import_raw", Label: "Paste JSON / YAML / TOML", Description: "Import from raw snippet or file path"},
			{Key: "import_codex", Label: "Import from Codex", Description: "Load missing servers from Codex config"},
			{Key: "import_claude", Label: "Import from Claude", Description: "Load missing user servers from ~/.claude.json"},
			{Key: "export_codex", Label: "Export to Codex", Description: "Write Spark servers to Codex config"},
			{Key: "export_claude", Label: "Export to Claude", Description: "Write Spark servers to ~/.claude.json"},
		},
	}
	m.refreshNames()
	m.loadCurrentDraft()
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
		if msg.OpenResult {
			m.modalKind = mcpModalProbeDetail
		}
		return m, nil
	case mcpExternalEditorFinishedMsg:
		return m, m.handleExternalEditorFinished(msg)
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
			if msg.OpenResult {
				m.modalKind = mcpModalProbeDetail
			}
		}
		return m, nil
	case tea.MouseMsg:
		if m.modalKind != mcpModalNone {
			if isPrimaryClick(msg.Type) {
				m.handleModalMouse(msg)
			}
			return m, nil
		}
		if isPrimaryClick(msg.Type) {
			return m, m.handleMainMouse(msg)
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *mcpManagerModel) currentName() string {
	if len(m.filtered) == 0 {
		return ""
	}
	if m.selected < 0 || m.selected >= len(m.filtered) {
		m.selected = 0
	}
	return m.filtered[m.selected]
}

func (m *mcpManagerModel) currentStatus() mcpStatusSummary {
	name := m.currentName()
	if name == "" {
		return mcpStatusSummary{Kind: mcpStatusUnknown, Headline: "No server", Detail: "No server selected"}
	}
	return summarizeMCPStatus(name, m.cfg.GetMcpServer(name), m.probes[name])
}

func (m *mcpManagerModel) refreshNames() {
	m.names = make([]string, 0, len(m.cfg.McpServers))
	for name := range m.cfg.McpServers {
		m.names = append(m.names, name)
	}
	sortMCPNames(m.names, m.cfg, m.probes)
	m.refreshFiltered()
}

func (m *mcpManagerModel) refreshFiltered() {
	if m.searchQuery == "" && m.filterOption == mcpFilterAll {
		m.filtered = append([]string{}, m.names...)
	} else {
		m.filtered = nil
		q := strings.ToLower(strings.TrimSpace(m.searchQuery))
		for _, name := range m.names {
			server := m.cfg.GetMcpServer(name)
			if q != "" {
				matches := strings.Contains(strings.ToLower(name), q)
				if server != nil {
					if strings.Contains(strings.ToLower(server.Command), q) || strings.Contains(strings.ToLower(server.URL), q) {
						matches = true
					}
				}
				if !matches {
					continue
				}
			}
			if m.filterOption != mcpFilterAll {
				status := summarizeMCPStatus(name, server, m.probes[name])
				switch m.filterOption {
				case mcpFilterHealthy:
					if status.Kind != mcpStatusReachable && status.Kind != mcpStatusConfigured {
						continue
					}
				case mcpFilterError:
					if status.Kind != mcpStatusBroken {
						continue
					}
				case mcpFilterUnknown:
					if status.Kind != mcpStatusUnknown {
						continue
					}
				case mcpFilterDisabled:
					if server != nil && server.Enabled {
						continue
					}
				case mcpFilterStdio:
					if isHTTPMCPServer(server) {
						continue
					}
				case mcpFilterHTTP:
					if !isHTTPMCPServer(server) {
						continue
					}
				}
			}
			m.filtered = append(m.filtered, name)
		}
	}
	if len(m.filtered) == 0 {
		m.selected = 0
	} else if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}
}
