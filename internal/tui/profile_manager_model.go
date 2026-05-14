package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

var (
	colorFocus        = lipgloss.Color("#b58cff")
	colorAccent       = lipgloss.Color("#c8aefc")
	colorText         = lipgloss.Color("#e8e8e8")
	colorTextSoft     = lipgloss.Color("#d7dae2")
	colorLabel        = lipgloss.Color("#a0a6b3")
	colorMuted        = lipgloss.Color("#7a7a8a")
	colorDim          = colorMuted
	colorSuccess      = lipgloss.Color("#5fd38d")
	colorError        = lipgloss.Color("#ff6b6b")
	colorWarning      = lipgloss.Color("#f4c95d")
	colorBg           = lipgloss.Color("#1e1f22")
	colorPanelBg      = lipgloss.Color("#25262b")
	colorBorder       = lipgloss.Color("#4b4f5c")
	colorFieldBg      = lipgloss.Color("#202127")
	colorFieldBgFocus = lipgloss.Color("#23242a")

	pmAppStyle = lipgloss.NewStyle().Margin(0, 1)

	pmTitleStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Background(colorBg).
			Padding(0, 1).
			MarginBottom(1)

	pmPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Background(colorPanelBg).
			Padding(0, 1)

	pmFocusedPanelStyle = pmPanelStyle.Copy().
				BorderForeground(lipgloss.Color("#6b6380"))

	pmItemStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			Foreground(colorTextSoft)
	pmSelectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(colorText).
				Bold(true)
	pmFocusedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(colorText).
				Background(colorFocus).
				Bold(true)
	pmSelectedMutedItemStyle = lipgloss.NewStyle().
					PaddingLeft(1).
					Foreground(lipgloss.Color("#98d7cf")).
					Bold(true)

	pmBadgeStyle = lipgloss.NewStyle().
			Foreground(colorFocus).
			Bold(true).
			Padding(0, 1)

	pmLabelStyle = lipgloss.NewStyle().
			Foreground(colorLabel).
			Width(pmLabelWidth).
			Align(lipgloss.Right).
			MarginRight(1)

	pmFocusedLabelStyle = pmLabelStyle.Copy().
				Foreground(colorText)

	pmInputStyle = lipgloss.NewStyle().
			Foreground(colorTextSoft).
			Background(colorFieldBg).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#363842"))

	pmFocusedInputStyle = pmInputStyle.Copy().
				Foreground(colorText).
				Background(colorFieldBgFocus).
				BorderForeground(colorFocus).
				Bold(true)
	pmCompactInputStyle = lipgloss.NewStyle().
				Foreground(colorTextSoft).
				Background(colorFieldBg).
				Padding(0, 1)
	pmCompactReadOnlyInputStyle = pmCompactInputStyle.Copy().
					Foreground(colorMuted).
					Background(colorPanelBg)
	pmCompactFocusedInputStyle = pmCompactInputStyle.Copy().
					Foreground(colorText).
					Background(colorFieldBgFocus).
					Bold(true)

	pmBtnStyle = lipgloss.NewStyle().
			Foreground(colorTextSoft).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3c3f49")).
			MarginRight(1)

	pmPrimaryBtnStyle = pmBtnStyle.Copy().
				Foreground(colorText).
				BorderForeground(colorFocus).
				Bold(true)

	pmActiveBtnStyle = pmPrimaryBtnStyle.Copy().
				Foreground(colorText).
				BorderForeground(colorFocus).
				Bold(true)

	pmLeftBtnStyle = lipgloss.NewStyle().
			Foreground(colorTextSoft).
			Padding(0, 1).
			MarginRight(1)

	pmLeftActiveBtnStyle = pmLeftBtnStyle.Copy().
				Foreground(colorFocus).
				Bold(true)
	pmCompactBtnStyle = lipgloss.NewStyle().
				Foreground(colorTextSoft).
				Background(colorFieldBg).
				Padding(0, 1)
	pmCompactPrimaryBtnStyle = pmCompactBtnStyle.Copy().
					Foreground(colorText).
					Background(colorFocus).
					Bold(true)
	pmCompactActiveBtnStyle = pmCompactPrimaryBtnStyle.Copy()

	pmStatusBarStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorBg).
				Padding(0, 1).
				MarginTop(1)
	pmStatusOkStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)
	pmStatusErrStyle = lipgloss.NewStyle().
				Foreground(colorError).
				Bold(true)
	pmStatusInfoStyle = lipgloss.NewStyle().
				Foreground(colorText)
	pmStatusLogStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	pmModalStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorFocus).
			Background(colorPanelBg).
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
	pmActDefault
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
	pmModalKindProviderType
	pmModalKindOpenAIAPIType
	pmModalKindModels
)

const pmModelsModalMaxVisible = 10

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

	lastTestSummary string
	lastTestOK      bool

	modalOpen   bool
	modalCursor int
	modalKind   int
	// When modal is opened by a mouse click, ignore the next click event
	// to avoid immediately closing the modal from the same physical click.
	modalIgnoreNextClick bool

	providerOptions        []pmProviderOption
	apiTypeOptions         []string
	apiTypeSelected        map[string]bool
	modelItems             []string
	modelsDraft            []string
	defaultModel           string
	modelEditMode          bool
	modelEditIndex         int
	modelEditBuffer        string
	modelModalNote         string
	modelSearchQuery       string
	modelSearchFocused     bool
	modelModalScroll       int
	modelModalVisibleCount int
	inputWidth             int

	leftContentX     int
	leftContentY     int
	leftVisibleRows  []int
	leftVisibleIdxs  []int
	rightContentX    int
	rightContentY    int
	leftButtonsRelY  int
	leftButtonsRelH  int
	leftButtonsRowW  int
	leftButtonsRow2Y int
	leftButtonsRow2W int
	leftAddBtnW      int
	leftCopyBtnW     int
	leftDefaultBtnW  int
	rightButtonsRelY int
	rightButtonsRelH int
	rightButtonsRowW int
	rightTestBtnW    int
	rightButtonsGapW int
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
			{name: "Gemini", kind: "gemini"},
		},
		apiTypeOptions: []string{
			config.OpenAIAPITypeAuto,
			config.OpenAIAPITypeResponses,
			config.OpenAIAPITypeChatCompletions,
			config.OpenAIAPITypeGeminiGenerateContent,
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
		if m.modalOpen && m.handleModalWheel(msg) {
			return m, nil
		}
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
