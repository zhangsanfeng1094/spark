package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

var (
	pmAppStyle = lipgloss.NewStyle().Margin(0, 1)

	pmTitleStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1).
			MarginBottom(1)

	pmPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	pmFocusedPanelStyle = pmPanelStyle.Copy().
				BorderForeground(colorBorderFocus)

	pmItemStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			Foreground(colorTextSoft)
	pmSelectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(colorText).
				Bold(true)
	pmFocusedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(colorFocus).
				Bold(true)
	pmSelectedMutedItemStyle = lipgloss.NewStyle().
					PaddingLeft(1).
					Foreground(colorAccent).
					Bold(true)

	pmBadgeStyle = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	pmLabelStyle = lipgloss.NewStyle().
			Foreground(colorLabel).
			Width(pmLabelWidth).
			Align(lipgloss.Right).
			MarginRight(1)

	pmFocusedLabelStyle = pmLabelStyle.Copy().
				Foreground(colorText)

	pmInputStyle = lipgloss.NewStyle().
			Foreground(colorTextSoft).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	pmFocusedInputStyle = pmInputStyle.Copy().
				Foreground(colorText).
				BorderForeground(colorFocus).
				Bold(true)
	pmCompactInputStyle = lipgloss.NewStyle().
				Foreground(colorTextSoft).
				Padding(0, 1)
	pmCompactReadOnlyInputStyle = lipgloss.NewStyle().
					Foreground(colorTextSoft).
					Padding(0, 1)
	pmCompactFocusedInputStyle = pmCompactInputStyle.Copy().
					Foreground(colorFocus).
					Bold(true)

	pmBtnStyle = lipgloss.NewStyle().
			Foreground(colorTextSoft).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
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
				Padding(0, 1)
	pmCompactPrimaryBtnStyle = pmCompactBtnStyle.Copy().
					Foreground(colorFocus).
					Bold(true)
	pmCompactActiveBtnStyle = pmCompactPrimaryBtnStyle.Copy()

	pmStatusBarStyle = lipgloss.NewStyle().
				Foreground(colorText).
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
	pmFieldModelListURL
	pmFieldModelsCSV
	pmFieldThinkingMode
	pmFieldThinkingEffort
	pmFieldThinkingBudget
)

type pmField struct {
	label       string
	value       string
	placeholder string
	cursor      int
	masked      bool
	readOnly    bool
	required    bool
	input       textinput.Model
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
	pmModalKindThinkingMode
	pmModalKindThinkingEffort
)

const pmModelsModalMaxVisible = 10

type pmStatusKind int

const (
	pmStatusNeutral pmStatusKind = iota
	pmStatusInfo
	pmStatusSuccess
	pmStatusWarning
	pmStatusError
	pmStatusTestRunning
	pmStatusTestSuccess
	pmStatusTestError
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

	status     string
	statusKind pmStatusKind
	statusSeq  uint64
	dirty      bool

	lastTestSummary string
	lastTestOK      bool
	runningTestSeq  uint64

	confirmDelete bool
	confirmQuit   bool

	modalOpen   bool
	modalCursor int
	modalKind   int
	// When modal is opened by a mouse click, ignore the next click event
	// to avoid immediately closing the modal from the same physical click.
	modalIgnoreNextClick bool

	providerOptions        []pmProviderOption
	apiTypeOptions         []string
	thinkingOptions        []string
	apiTypeSelected        map[string]bool
	modelItems             []string
	modelsDraft            []string
	defaultModel           string
	modelEditMode          bool
	modelEditIndex         int
	modelEditBuffer        string
	modelEditInput         textinput.Model
	modelModalNote         string
	modelSearchQuery       string
	modelSearchInput       textinput.Model
	modelSearchFocused     bool
	modelModalScroll       int
	modelModalVisibleCount int
	inputWidth             int
	leftPanelWidth         int

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
	fieldStartRelY     []int
	fieldEndRelY       []int
	modalX             int
	modalY             int
	modalW             int
	modalH             int
	modalOptionStartY  int
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
			{name: "Anthropic", kind: "anthropic"},
			{name: "Gemini", kind: "gemini"},
			{name: "Command Code", kind: "commandcode"},
		},
		apiTypeOptions: []string{
			config.OpenAIAPITypeResponses,
			config.OpenAIAPITypeChatCompletions,
			config.OpenAIAPITypeGeminiGenerateContent,
			config.OpenAIAPITypeAnthropicMessages,
		},
		thinkingOptions: []string{"client", "auto", "force", "off", "minimal", "low", "medium", "high", "xhigh", "max"},
		apiTypeSelected: map[string]bool{},
		focusArea:       pmFocusProfiles,
		focusField:      0,
		actionIndex:     pmActSave,
		status:          "Ready. Use [Tab]/[Shift+Tab] to move focus, [Enter] to activate.",
		statusKind:      pmStatusNeutral,
		inputWidth:      pmInputWidth,
	}
	m.refreshNames()
	m.selectByName(cfg.DefaultProfile)
	m.loadSelectedProfileFields()
	return m
}

func (m *pmModel) Init() tea.Cmd { return textinput.Blink }

func (m *pmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case cursor.BlinkMsg:
		if m.modalOpen && m.modalKind == pmModalKindModels {
			var cmd tea.Cmd
			if m.modelEditMode {
				m.modelEditInput, cmd = m.modelEditInput.Update(msg)
			} else if m.modelSearchFocused {
				m.modelSearchInput, cmd = m.modelSearchInput.Update(msg)
			}
			return m, cmd
		}
		if m.focusArea == pmFocusFields && m.focusField >= 0 && m.focusField < len(m.fields) {
			var cmd tea.Cmd
			m.fields[m.focusField].input, cmd = m.fields[m.focusField].input.Update(msg)
			return m, cmd
		}
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
		if m.confirmDelete {
			return m, m.handleConfirmDeleteKey(msg)
		}
		if m.modalOpen {
			return m, m.handleModalKey(msg)
		}

		if cmd, handled := m.handleMainKey(msg); handled {
			return m, cmd
		}

		cmd := m.handleFieldEdit(msg)
		return m, cmd
	}
	return m, nil
}
