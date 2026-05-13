package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DashboardAction struct {
	Title       string
	Description string
}

type DashboardSummary struct {
	CurrentProfile string
	ConfigPath     string
}

type dashboardModel struct {
	title    string
	actions  []DashboardAction
	summary  DashboardSummary
	cursor   int
	width    int
	height   int
	canceled bool
	choice   string
}

var (
	dashboardFrameStyle  = lipgloss.NewStyle().Margin(0, 1)
	dashboardHeaderStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorFocus).
				Bold(true).
				Padding(0, 2)
	dashboardSectionTitleStyle = lipgloss.NewStyle().
					Foreground(colorLabel).
					Bold(true)
	dashboardMenuItemStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Padding(0, 1)
	dashboardSelectedItemStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#ffffff")).
					Background(colorFocus).
					Bold(true).
					Padding(0, 1)
	dashboardBodyTextStyle = lipgloss.NewStyle().
				Foreground(colorTextSoft)
	dashboardMutedTextStyle = lipgloss.NewStyle().
				Foreground(colorMuted)
)

func SelectDashboard(title string, actions []DashboardAction, summary DashboardSummary) (string, error) {
	if len(actions) == 0 {
		return "", fmt.Errorf("no actions")
	}
	m := &dashboardModel{
		title:   title,
		actions: actions,
		summary: summary,
	}
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithMouseCellMotion())
	out, err := p.Run()
	if err != nil {
		return "", err
	}
	result := out.(*dashboardModel)
	if result.canceled {
		return "", fmt.Errorf("aborted")
	}
	return result.choice, nil
}

func (m *dashboardModel) Init() tea.Cmd { return nil }

func (m *dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.canceled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.actions)-1 {
				m.cursor++
			}
		case "enter":
			m.choice = m.actions[m.cursor].Title
			return m, tea.Quit
		}
	case tea.MouseMsg:
		if !isPrimaryClick(msg.Type) {
			return m, nil
		}
		row := msg.Y - 4
		if row >= 0 && row < len(m.actions) {
			m.cursor = row
			m.choice = m.actions[m.cursor].Title
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *dashboardModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	leftWidth := 34
	rightWidth := m.width - leftWidth - 8
	if rightWidth < 44 {
		rightWidth = 44
	}

	header := dashboardHeaderStyle.Width(m.width - 6).Render(m.title)
	left := m.renderMenuPane(leftWidth)
	right := m.renderDetailPane(rightWidth)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		pmFocusedPanelStyle.Width(leftWidth).Render(left),
		pmPanelStyle.Width(rightWidth).Render(right),
	)

	help := pmStatusBarStyle.Width(m.width - 6).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Foreground(colorText).Render("Ready"),
			lipgloss.NewStyle().Width(m.width-24).Align(lipgloss.Right).Foreground(colorMuted).Render("↑/↓ Move · Enter Select · Q Quit"),
		),
	)

	return fitToViewportHeight(dashboardFrameStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, help)), m.height)
}

func (m *dashboardModel) renderMenuPane(width int) string {
	lines := []string{
		dashboardSectionTitleStyle.Render("Menu"),
		"",
	}
	for i, action := range m.actions {
		line := "  " + action.Title
		style := dashboardMenuItemStyle.Width(width - 4)
		if i == m.cursor {
			line = "> " + action.Title
			style = dashboardSelectedItemStyle.Width(width - 4)
		}
		lines = append(lines, style.Render(line))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *dashboardModel) renderDetailPane(width int) string {
	action := m.actions[m.cursor]
	lines := []string{
		lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(action.Title),
		"",
		dashboardBodyTextStyle.Width(width - 4).Render(action.Description),
		"",
		dashboardSectionTitleStyle.Render("Current Context"),
		dashboardBodyTextStyle.Render("Current profile: " + emptyFallback(m.summary.CurrentProfile, "not set")),
		dashboardMutedTextStyle.Width(width - 4).Render("Config file: " + emptyFallback(m.summary.ConfigPath, "unavailable")),
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func emptyFallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
