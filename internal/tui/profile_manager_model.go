package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

var (
	colorFocus  = lipgloss.Color("#bd93f9")
	colorAccent = lipgloss.Color("#ff79c6")
	colorText   = lipgloss.Color("#f8f8f2")
	colorDim    = lipgloss.Color("#6272a4")
	colorBg     = lipgloss.Color("#282a36")

	pmAppStyle = lipgloss.NewStyle().Margin(0, 1)

	pmTitleStyle = lipgloss.NewStyle().
			Foreground(colorFocus).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorFocus).
			Padding(0, 1).
			MarginBottom(1)

	pmPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	pmFocusedPanelStyle = pmPanelStyle.Copy().
				BorderForeground(colorFocus)

	pmItemStyle         = lipgloss.NewStyle().PaddingLeft(1).Foreground(colorText)
	pmSelectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(lipgloss.Color("#ffffff")).
				Background(colorFocus).
				Bold(true)
	pmFocusedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#3a3d52")).
				Bold(true)

	pmLabelStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Width(pmLabelWidth).
			Align(lipgloss.Right).
			MarginRight(1)

	pmInputStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(lipgloss.Color("#1e1f29")).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim)

	pmFocusedInputStyle = pmInputStyle.Copy().
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#252738")).
				BorderForeground(colorFocus).
				Bold(true)

	pmBtnStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorDim).
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			MarginRight(1)

	pmActiveBtnStyle = pmBtnStyle.Copy().
				Foreground(lipgloss.Color("#ffffff")).
				Background(colorAccent).
				BorderForeground(colorAccent).
				Bold(true)

	// Left panel buttons - simpler style without border
	pmLeftBtnStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorDim).
				Padding(0, 2).
				MarginRight(1)

	pmLeftActiveBtnStyle = pmLeftBtnStyle.Copy().
				Foreground(lipgloss.Color("#ffffff")).
				Background(colorAccent).
				Bold(true)

	pmStatusBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f8f8f2")).
				Background(lipgloss.Color("#44475a")).
				Padding(0, 1).
				MarginTop(1)
	pmStatusOkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d7ffd9")).
			Bold(true)
	pmStatusErrStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffd7d7")).
				Bold(true)
	pmStatusInfoStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f8f8f2"))
	pmStatusLogStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	pmModalStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2).
			Align(lipgloss.Center)
)

const (
	pmFocusProfiles = iota
	pmFocusFields
	pmFocusActions
)

const (
	pmActAdd = iota
	pmActCopy
	pmActDel
	pmActTest
	pmActSave
)

const (
	pmBorderSize = 1
	pmPaddingV   = 0
	pmPaddingH   = 1
	pmLabelWidth = 18
	pmInputWidth = 42
)

const (
	pmFieldProfileName = iota
	pmFieldProviderType
	pmFieldOpenAIBaseURL
	pmFieldOpenAIAPIKey
	pmFieldOpenAIAPIType
	pmFieldModelsCSV
	pmFieldDefaultModel
)

type pmField struct {
	label    string
	value    string
	cursor   int
	masked   bool
	readOnly bool
}

type pmProviderOption struct {
	name string
	kind string
}

const (
	pmModalKindNone = iota
	pmModalKindAddProfile
	pmModalKindOpenAIAPIType
	pmModalKindModels
)

type pmModel struct {
	cfg *config.RootConfig

	width  int
	height int

	profileNames []string
	selected     int

	fields      []pmField
	focusArea   int
	focusField  int
	actionIndex int

	status string
	dirty  bool

	modalOpen   bool
	modalCursor int
	modalKind   int
	// When modal is opened by a mouse click, ignore the next click event
	// to avoid immediately closing the modal from the same physical click.
	modalIgnoreNextClick bool

	providerOptions []pmProviderOption
	apiTypeOptions  []string
	apiTypeSelected map[string]bool
	modelItems      []string
	modelsDraft     []string
	defaultModel    string
	modelEditMode   bool
	modelEditIndex  int
	modelEditBuffer string
	modelModalNote  string
	inputWidth      int

	leftContentX     int
	leftContentY     int
	rightContentX    int
	rightContentY    int
	leftButtonsRelY  int
	leftButtonsRelH  int
	leftButtonsRowW  int
	leftAddBtnW      int
	leftCopyBtnW     int
	rightButtonsRelY int
	rightButtonsRelH int
	rightButtonsRowW int
	rightTestBtnW    int
	fieldStartRelY   []int
	fieldEndRelY     []int
	modalX           int
	modalY           int
	modalW           int
	modalH           int
}

func ManageProfilesDashboard(cfg *config.RootConfig) error {
	m := newPMModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func newPMModel(cfg *config.RootConfig) *pmModel {
	config.Normalize(cfg)
	m := &pmModel{
		cfg: cfg,
		providerOptions: []pmProviderOption{
			{name: "OpenAI", kind: "openai"},
		},
		apiTypeOptions: []string{
			config.OpenAIAPITypeAuto,
			config.OpenAIAPITypeResponses,
			config.OpenAIAPITypeChatCompletions,
		},
		apiTypeSelected: map[string]bool{},
		focusArea:       pmFocusProfiles,
		focusField:      0,
		actionIndex:     pmActSave,
		status:          "Ready. Use [Tab]/[Shift+Tab] to move focus, [Enter] to activate.",
		inputWidth:      pmInputWidth,
	}
	m.refreshNames()
	m.selectByName(cfg.DefaultProfile)
	m.loadSelectedProfileFields()
	return m
}

func (m *pmModel) Init() tea.Cmd { return nil }

func (m *pmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case testResultMsg:
		m.handleTestResult(msg)
		return m, nil

	case fetchModelsResultMsg:
		m.handleFetchModelsResult(msg)
		return m, nil

	case tea.MouseMsg:
		if !isPrimaryClick(msg.Type) {
			return m, nil
		}
		if m.modalOpen {
			if m.modalIgnoreNextClick {
				m.modalIgnoreNextClick = false
				return m, nil
			}
			m.handleModalMouse(msg)
			return m, nil
		}
		return m, m.handleMainMouse(msg)

	case tea.KeyMsg:
		if m.modalOpen {
			return m, m.handleModalKey(msg)
		}

		if cmd, handled := m.handleMainKey(msg); handled {
			return m, cmd
		}

		m.handleFieldEdit(msg)
		return m, nil
	}
	return m, nil
}
