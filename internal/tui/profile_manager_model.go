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

	pmAppStyle = lipgloss.NewStyle().Margin(1, 1)

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

	pmLabelStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Width(pmLabelWidth).
			Align(lipgloss.Right).
			MarginRight(1)

	pmInputStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(lipgloss.Color("#1e1f29")).
			Padding(0, 1).
			Width(pmInputWidth)

	pmFocusedInputStyle = pmInputStyle.Copy().
				Underline(true).
				UnderlineSpaces(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#252738"))

	pmBtnStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorDim).
			Padding(0, 2).
			MarginRight(1)

	pmActiveBtnStyle = pmBtnStyle.Copy().
				Foreground(lipgloss.Color("#ffffff")).
				Background(colorAccent).
				Bold(true)

	pmStatusBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f8f8f2")).
				Background(lipgloss.Color("#44475a")).
				Padding(0, 1).
				MarginTop(1)

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

	providerOptions []pmProviderOption

	leftContentX     int
	leftContentY     int
	rightContentX    int
	rightContentY    int
	leftButtonsRelY  int
	rightButtonsRelY int
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
	configMustNormalize(cfg)
	m := &pmModel{
		cfg: cfg,
		providerOptions: []pmProviderOption{
			{name: "OpenAI", kind: "openai"},
			{name: "Anthropic (Claude)", kind: "anthropic"},
			{name: "Ollama (Local)", kind: "ollama"},
			{name: "OpenAI Compatible", kind: "compatible"},
		},
		focusArea:   pmFocusProfiles,
		focusField:  0,
		actionIndex: pmActSave,
		status:      "Ready. Press [Tab] to navigate, [Enter] to select.",
	}
	m.refreshNames()
	m.selectByName(cfg.DefaultProfile)
	m.loadSelectedProfileFields()
	return m
}

func configMustNormalize(cfg *config.RootConfig) {
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*config.Profile{}
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	if _, ok := cfg.Profiles[cfg.DefaultProfile]; !ok {
		cfg.Profiles[cfg.DefaultProfile] = &config.Profile{OpenAIBaseURL: "https://api.openai.com/v1"}
	}
	if cfg.Integrations == nil {
		cfg.Integrations = map[string]*config.IntegrationConfig{}
	}
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

	case tea.MouseMsg:
		if msg.Type != tea.MouseRelease {
			return m, nil
		}
		if m.modalOpen {
			m.handleModalMouse(msg)
		} else {
			m.handleMainMouse(msg)
		}
		return m, nil

	case tea.KeyMsg:
		if m.modalOpen {
			m.handleModalKey(msg)
			return m, nil
		}

		if cmd, handled := m.handleMainKey(msg); handled {
			return m, cmd
		}

		m.handleFieldEdit(msg)
		return m, nil
	}
	return m, nil
}
