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
	CurrentProfile         string
	QuickLaunchIntegration string
	DefaultProfile         string
	DefaultModel           string
	ConfigPath             string
	TotalProfiles          int
	TotalMCPServers        int
	EnabledMCPServers      int
	TotalSkills            int
	PromptEnabled          bool
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
				Foreground(colorAccent).
				Bold(true).
				Padding(0, 1)
	dashboardSectionTitleStyle = lipgloss.NewStyle().
					Foreground(colorLabel).
					Bold(true)
	dashboardMenuItemStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Padding(0, 1)
	dashboardSelectedItemStyle = lipgloss.NewStyle().
					Foreground(colorFocus).
					Bold(true).
					Padding(0, 1)
	dashboardBodyTextStyle = lipgloss.NewStyle().
				Foreground(colorTextSoft)
	dashboardQuickLaunchValueStyle = lipgloss.NewStyle().
					Foreground(colorAccent).
					Bold(true)
	dashboardDefaultProfileValueStyle = lipgloss.NewStyle().
						Foreground(colorSuccess).
						Bold(true)
	dashboardDefaultModelValueStyle = lipgloss.NewStyle().
					Foreground(colorWarning).
					Bold(true)
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
	panelHeight := max(lipgloss.Height(left), lipgloss.Height(right))
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		pmFocusedPanelStyle.Width(leftWidth).Height(panelHeight).Render(left),
		pmPanelStyle.Width(rightWidth).Height(panelHeight).Render(right),
	)

	statusText := "Ready"
	if m.summary.ConfigPath != "" {
		statusText = "Config: " + m.summary.ConfigPath
	}
	help := pmStatusBarStyle.Width(m.width - 6).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Foreground(colorMuted).Render(statusText),
			lipgloss.NewStyle().Width(max(0, m.width-lipgloss.Width(statusText)-8)).Align(lipgloss.Right).Foreground(colorMuted).Render("↑/↓ Move · Enter Select · Q Quit"),
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
	}

	renderRow := func(label, value string, valStyle lipgloss.Style) string {
		return lipgloss.JoinHorizontal(lipgloss.Top,
			dashboardBodyTextStyle.Render(label),
			valStyle.Render(value),
		)
	}

	switch action.Title {
	case "Quick launch":
		lines = append(lines,
			dashboardSectionTitleStyle.Render("Launch Target"),
			renderRow("Quick launch: ", emptyFallback(m.summary.QuickLaunchIntegration, "not set"), dashboardQuickLaunchValueStyle),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultProfileValueStyle),
			renderRow("Default model: ", emptyFallback(m.summary.DefaultModel, "not set"), dashboardDefaultModelValueStyle),
			dashboardMutedTextStyle.Width(width - 4).Render("Config file: " + emptyFallback(m.summary.ConfigPath, "unavailable")),
		)
	case "Launch options":
		lines = append(lines,
			dashboardSectionTitleStyle.Render("Launch Defaults"),
			renderRow("Default client: ", emptyFallback(m.summary.QuickLaunchIntegration, "not set"), dashboardQuickLaunchValueStyle),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultProfileValueStyle),
			renderRow("Default model: ", emptyFallback(m.summary.DefaultModel, "not set"), dashboardDefaultModelValueStyle),
		)
	case "Manage profiles":
		countText := fmt.Sprintf("%d configured", m.summary.TotalProfiles)
		if m.summary.TotalProfiles == 0 {
			countText = "none configured"
		}
		lines = append(lines,
			dashboardSectionTitleStyle.Render("Profiles Overview"),
			renderRow("Total profiles: ", countText, dashboardQuickLaunchValueStyle),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultProfileValueStyle),
			renderRow("Default model: ", emptyFallback(m.summary.DefaultModel, "not set"), dashboardDefaultModelValueStyle),
		)
	case "Manage MCP servers":
		countText := fmt.Sprintf("%d configured (%d enabled)", m.summary.TotalMCPServers, m.summary.EnabledMCPServers)
		if m.summary.TotalMCPServers == 0 {
			countText = "none configured"
		}
		lines = append(lines,
			dashboardSectionTitleStyle.Render("MCP Servers Overview"),
			renderRow("MCP servers: ", countText, dashboardQuickLaunchValueStyle),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultProfileValueStyle),
		)
	case "Manage skills":
		countText := fmt.Sprintf("%d installed", m.summary.TotalSkills)
		if m.summary.TotalSkills == 0 {
			countText = "none installed"
		}
		lines = append(lines,
			dashboardSectionTitleStyle.Render("Skills Overview"),
			renderRow("Agent skills: ", countText, dashboardQuickLaunchValueStyle),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultProfileValueStyle),
		)
	case "Token usage":
		lines = append(lines,
			dashboardSectionTitleStyle.Render("Usage Tracking"),
			renderRow("Tracking status: ", "Active (recorded in SQLite)", dashboardDefaultProfileValueStyle),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultModelValueStyle),
		)
	case "Manage settings":
		promptStatus := "disabled"
		if m.summary.PromptEnabled {
			promptStatus = "enabled"
		}
		lines = append(lines,
			dashboardSectionTitleStyle.Render("Global Settings"),
			renderRow("Default client: ", emptyFallback(m.summary.QuickLaunchIntegration, "not set"), dashboardQuickLaunchValueStyle),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultProfileValueStyle),
			renderRow("Prompt injection: ", promptStatus, dashboardDefaultModelValueStyle),
			dashboardMutedTextStyle.Width(width - 4).Render("Config file: " + emptyFallback(m.summary.ConfigPath, "unavailable")),
		)
	case "Quit":
		lines = append(lines,
			dashboardSectionTitleStyle.Render("Session"),
			renderRow("Status: ", "Ready to exit", dashboardMutedTextStyle),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultProfileValueStyle),
			dashboardMutedTextStyle.Width(width - 4).Render("Config file: " + emptyFallback(m.summary.ConfigPath, "unavailable")),
		)
	default:
		lines = append(lines,
			dashboardSectionTitleStyle.Render("Current Context"),
			renderRow("Default profile: ", emptyFallback(summaryDefaultProfile(m.summary), "not set"), dashboardDefaultProfileValueStyle),
			renderRow("Default model: ", emptyFallback(m.summary.DefaultModel, "not set"), dashboardDefaultModelValueStyle),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func summaryDefaultProfile(summary DashboardSummary) string {
	if summary.DefaultProfile != "" {
		return summary.DefaultProfile
	}
	return summary.CurrentProfile
}

func emptyFallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
