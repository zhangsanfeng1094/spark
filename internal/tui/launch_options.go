package tui

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

var ErrCanceled = errors.New("aborted")

type LaunchSelection struct {
	Integration string
	Profile     string
	Model       string
}

const (
	launchOptionsColumnIntegration = iota
	launchOptionsColumnProfile
	launchOptionsColumnModel
	launchOptionsColumnCount
)

type launchOptionsModel struct {
	cfg               *config.RootConfig
	integrations      []string
	profiles          []string
	models            []string
	activeColumn      int
	integrationCursor int
	profileCursor     int
	modelCursor       int
	width             int
	height            int
	canceled          bool
	selected          LaunchSelection
}

func SelectLaunchOptions(integrationNames []string, cfg *config.RootConfig, defaultIntegration string) (LaunchSelection, error) {
	if len(integrationNames) == 0 {
		return LaunchSelection{}, fmt.Errorf("no integrations available")
	}
	m := newLaunchOptionsModel(integrationNames, cfg, defaultIntegration)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithMouseCellMotion())
	out, err := p.Run()
	if err != nil {
		return LaunchSelection{}, err
	}
	result := out.(*launchOptionsModel)
	if result.canceled {
		return LaunchSelection{}, ErrCanceled
	}
	return result.selected, nil
}

func newLaunchOptionsModel(integrationNames []string, cfg *config.RootConfig, defaultIntegration string) *launchOptionsModel {
	m := &launchOptionsModel{
		cfg:          cfg,
		integrations: append([]string(nil), integrationNames...),
		profiles:     launchProfileNames(cfg),
	}
	m.integrationCursor = indexOfFold(m.integrations, defaultIntegration)
	if m.integrationCursor < 0 {
		m.integrationCursor = 0
	}
	m.profileCursor = indexOf(m.profiles, defaultProfileName(cfg))
	if m.profileCursor < 0 {
		m.profileCursor = 0
	}
	m.refreshModels()
	m.selected = m.currentSelection()
	return m
}

func (m *launchOptionsModel) Init() tea.Cmd { return nil }

func (m *launchOptionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyEnter:
			m.selected = m.currentSelection()
			return m, tea.Quit
		case tea.KeyTab:
			m.moveColumn(1)
		case tea.KeyShiftTab:
			m.moveColumn(-1)
		case tea.KeyLeft:
			m.moveColumn(-1)
		case tea.KeyRight:
			m.moveColumn(1)
		case tea.KeyUp:
			m.moveActiveCursor(-1)
		case tea.KeyDown:
			m.moveActiveCursor(1)
		case tea.KeyRunes:
			switch strings.ToLower(string(msg.Runes)) {
			case "q":
				m.canceled = true
				return m, tea.Quit
			case "h":
				m.moveColumn(-1)
			case "l":
				m.moveColumn(1)
			case "k":
				m.moveActiveCursor(-1)
			case "j":
				m.moveActiveCursor(1)
			}
		}
	}
	return m, nil
}

func (m *launchOptionsModel) View() string {
	if m.width == 0 {
		return "loading..."
	}
	width := m.width
	if width < 78 {
		width = 78
	}
	header := dashboardHeaderStyle.Width(width - 6).Render("Launch options")
	columnWidth := (width - 14) / 3
	if columnWidth < 20 {
		columnWidth = 20
	}
	help := pmStatusBarStyle.Width(width - 6).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Foreground(colorText).Render("Ready"),
			lipgloss.NewStyle().Width(width-24).Align(lipgloss.Right).Foreground(colorMuted).Render("Tab Column | Up/Down Move | Enter Launch | Esc Back"),
		),
	)
	visibleSlots := launchOptionsVisibleSlots(m.height, lipgloss.Height(header), lipgloss.Height(help))
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderColumn("Integration", m.integrations, m.integrationCursor, m.activeColumn == launchOptionsColumnIntegration, columnWidth, visibleSlots),
		m.renderColumn("Profile", m.profiles, m.profileCursor, m.activeColumn == launchOptionsColumnProfile, columnWidth, visibleSlots),
		m.renderColumn("Model", m.models, m.modelCursor, m.activeColumn == launchOptionsColumnModel, columnWidth, visibleSlots),
	)
	return fitToViewportHeight(pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, help)), m.height)
}

func (m *launchOptionsModel) renderColumn(title string, items []string, cursor int, focused bool, width, visibleSlots int) string {
	lines := []string{
		dashboardSectionTitleStyle.Render(title),
		"",
	}
	if len(items) == 0 {
		lines = append(lines, dashboardMutedTextStyle.Width(width-4).Render("  not set"))
	} else {
		start, end := 0, len(items)
		showUp, showDown := false, false
		if visibleSlots > 0 {
			if visibleSlots < 3 {
				start = moveCursor(cursor, len(items), 0)
				end = start + 1
			} else {
				start, end, showUp, showDown = profileWindow(len(items), cursor, visibleSlots)
			}
		}
		if showUp {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Width(width-4).Render("  ^ more"))
		}
		for i := start; i < end; i++ {
			item := items[i]
			line := "  " + item
			style := pmItemStyle.Width(width - 4)
			if i == cursor {
				line = "> " + item
				if focused {
					style = pmFocusedItemStyle.Width(width - 4)
				} else {
					style = pmSelectedMutedItemStyle.Width(width - 4)
				}
			}
			lines = append(lines, style.Render(line))
		}
		if showDown {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Width(width-4).Render("  v more"))
		}
	}
	panel := pmPanelStyle.Width(width)
	if focused {
		panel = pmFocusedPanelStyle.Width(width)
	}
	return panel.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func launchOptionsVisibleSlots(height, headerHeight, helpHeight int) int {
	if height <= 0 {
		return 0
	}
	const columnOverhead = 4 // panel borders plus title and spacer rows
	slots := height - headerHeight - helpHeight - columnOverhead
	if slots < 1 {
		return 1
	}
	return slots
}

func (m *launchOptionsModel) moveColumn(delta int) {
	m.activeColumn = (m.activeColumn + delta + launchOptionsColumnCount) % launchOptionsColumnCount
}

func (m *launchOptionsModel) moveActiveCursor(delta int) {
	switch m.activeColumn {
	case launchOptionsColumnIntegration:
		m.integrationCursor = moveCursor(m.integrationCursor, len(m.integrations), delta)
	case launchOptionsColumnProfile:
		prev := m.profileCursor
		m.profileCursor = moveCursor(m.profileCursor, len(m.profiles), delta)
		if m.profileCursor != prev {
			m.refreshModels()
		}
	case launchOptionsColumnModel:
		m.modelCursor = moveCursor(m.modelCursor, len(m.models), delta)
	}
}

func (m *launchOptionsModel) refreshModels() {
	m.models = launchModelsForProfile(m.cfg, m.selectedProfile())
	m.modelCursor = 0
}

func (m *launchOptionsModel) currentSelection() LaunchSelection {
	return LaunchSelection{
		Integration: safeAt(m.integrations, m.integrationCursor),
		Profile:     m.selectedProfile(),
		Model:       safeAt(m.models, m.modelCursor),
	}
}

func (m *launchOptionsModel) selectedProfile() string {
	return safeAt(m.profiles, m.profileCursor)
}

func launchProfileNames(cfg *config.RootConfig) []string {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func launchModelsForProfile(cfg *config.RootConfig, profileName string) []string {
	if cfg == nil || profileName == "" {
		return nil
	}
	profile, err := cfg.ProfileByName(profileName)
	if err != nil {
		return nil
	}
	return config.EffectiveProfileModels(profile)
}

func defaultProfileName(cfg *config.RootConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.DefaultProfile
}

func moveCursor(cursor, length, delta int) int {
	if length <= 0 {
		return 0
	}
	cursor += delta
	if cursor < 0 {
		return 0
	}
	if cursor >= length {
		return length - 1
	}
	return cursor
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}

func indexOfFold(values []string, want string) int {
	for i, value := range values {
		if strings.EqualFold(value, want) {
			return i
		}
	}
	return -1
}

func safeAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}
