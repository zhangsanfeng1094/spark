package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type TokenUsageSummary struct {
	Window            string
	Client            string
	Label             string
	Requests          int
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
}

type TokenUsageLoader func() ([]TokenUsageSummary, error)

type tokenUsageModel struct {
	summaries []TokenUsageSummary
	windows   []string
	clients   []string
	window    int
	client    int
	width     int
	height    int
	load      TokenUsageLoader
	status    string
}

type tokenUsageRefreshMsg struct {
	summaries []TokenUsageSummary
	err       error
}

func ShowTokenUsage(summaries []TokenUsageSummary, refreshers ...TokenUsageLoader) error {
	var load TokenUsageLoader
	if len(refreshers) > 0 {
		load = refreshers[0]
	}
	m := newTokenUsageModel(summaries, load)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func RenderTokenUsageSnapshot(summaries []TokenUsageSummary, width, height, cursor int) (string, error) {
	m := newTokenUsageModel(summaries)
	width, height = normalizeSnapshotSize(width, height)
	m.width = width
	m.height = height
	if cursor >= 0 && cursor < len(m.windows) {
		m.window = cursor
	}
	return m.View(), nil
}

func newTokenUsageModel(summaries []TokenUsageSummary, refreshers ...TokenUsageLoader) *tokenUsageModel {
	var load TokenUsageLoader
	if len(refreshers) > 0 {
		load = refreshers[0]
	}
	m := &tokenUsageModel{load: load}
	m.setSummaries(summaries)
	return m
}

func normalizeTokenUsageSummaries(summaries []TokenUsageSummary) []TokenUsageSummary {
	if len(summaries) == 0 {
		summaries = []TokenUsageSummary{
			{Window: "today", Client: "all", Label: "Today"},
			{Window: "7d", Client: "all", Label: "7d"},
			{Window: "30d", Client: "all", Label: "30d"},
			{Window: "all", Client: "all", Label: "All"},
		}
	}
	for i := range summaries {
		if summaries[i].Client == "" {
			summaries[i].Client = "all"
		}
		if summaries[i].Label == "" {
			summaries[i].Label = summaries[i].Window
		}
	}
	return summaries
}

func (m *tokenUsageModel) setSummaries(summaries []TokenUsageSummary) {
	summaries = normalizeTokenUsageSummaries(summaries)
	selectedWindow := selectedValue(m.windows, m.window)
	selectedClient := selectedValue(m.clients, m.client)
	m.summaries = summaries
	m.windows = uniqueSummaryValues(summaries, func(s TokenUsageSummary) string { return s.Window })
	m.clients = uniqueSummaryValues(summaries, func(s TokenUsageSummary) string { return s.Client })
	m.window = selectedIndex(m.windows, selectedWindow)
	m.client = selectedIndex(m.clients, selectedClient)
}

func (m *tokenUsageModel) Init() tea.Cmd { return nil }

func (m *tokenUsageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tokenUsageRefreshMsg:
		if msg.err != nil {
			m.status = "Refresh failed: " + msg.err.Error()
			return m, nil
		}
		m.setSummaries(msg.summaries)
		m.status = "Refreshed token usage"
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "r":
			if m.load == nil {
				m.status = "Refresh unavailable"
				return m, nil
			}
			m.status = "Refreshing token usage..."
			return m, m.refreshCmd()
		case "left", "h":
			if m.window > 0 {
				m.window--
			}
		case "right", "l":
			if m.window < len(m.windows)-1 {
				m.window++
			}
		case "up", "k":
			if m.client > 0 {
				m.client--
			}
		case "down", "j":
			if m.client < len(m.clients)-1 {
				m.client++
			}
		}
	case tea.MouseMsg:
		if !isPrimaryClick(msg.Type) {
			return m, nil
		}
		index := msg.X / 10
		if msg.Y == 3 && index >= 0 && index < len(m.windows) {
			m.window = index
		}
		if msg.Y == 4 && index >= 0 && index < len(m.clients) {
			m.client = index
		}
	}
	return m, nil
}

func (m *tokenUsageModel) View() string {
	if m.width == 0 {
		return "loading..."
	}
	width := m.width - 6
	if width < 44 {
		width = 44
	}
	summary := m.currentSummary()
	header := dashboardHeaderStyle.Width(width).Render("Token usage")
	tabs := lipgloss.JoinVertical(lipgloss.Left, m.renderWindowTabs(width), m.renderClientTabs(width))
	body := pmPanelStyle.Width(width).Render(m.renderUsageBody(summary, width))
	status := "Compat proxy usage"
	if strings.TrimSpace(m.status) != "" {
		status = m.status
	}
	help := pmStatusBarStyle.Width(width).Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Foreground(colorText).MaxWidth(width/2).Render(status),
			lipgloss.NewStyle().Width(width-42).Align(lipgloss.Right).Foreground(colorMuted).Render("Left/Right Time · Up/Down Client · R Refresh · Q Back"),
		),
	)
	return fitToViewportHeight(dashboardFrameStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, help)), m.height)
}

func (m *tokenUsageModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		summaries, err := m.load()
		return tokenUsageRefreshMsg{summaries: summaries, err: err}
	}
}

func (m *tokenUsageModel) renderWindowTabs(width int) string {
	parts := make([]string, 0, len(m.windows))
	for i, window := range m.windows {
		style := dashboardMenuItemStyle.Padding(0, 2)
		if i == m.window {
			style = dashboardSelectedItemStyle.Padding(0, 2)
		}
		parts = append(parts, style.Render(m.windowLabel(window)))
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Top, parts...))
}

func (m *tokenUsageModel) renderClientTabs(width int) string {
	parts := make([]string, 0, len(m.clients))
	for i, client := range m.clients {
		style := dashboardMenuItemStyle.Padding(0, 2)
		if i == m.client {
			style = dashboardSelectedItemStyle.Padding(0, 2)
		}
		parts = append(parts, style.Render(clientLabel(client)))
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Top, parts...))
}

func (m *tokenUsageModel) renderUsageBody(summary TokenUsageSummary, width int) string {
	lines := []string{
		lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(summary.Label + " · " + clientLabel(summary.Client)),
		"",
		usageMetricLine("Total tokens", summary.TotalTokens),
		usageMetricLine("Cached tokens", summary.CachedInputTokens),
		usageMetricLine("Input tokens", summary.InputTokens),
		usageMetricLine("Output tokens", summary.OutputTokens),
		usageMetricLine("Requests", summary.Requests),
	}
	if summary.Requests == 0 {
		lines = append(lines, "", dashboardMutedTextStyle.Width(width-4).Render("No recorded compat proxy usage."))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *tokenUsageModel) currentSummary() TokenUsageSummary {
	window := selectedValue(m.windows, m.window)
	client := selectedValue(m.clients, m.client)
	for _, summary := range m.summaries {
		if summary.Window == window && summary.Client == client {
			return summary
		}
	}
	return TokenUsageSummary{Window: window, Client: client, Label: m.windowLabel(window)}
}

func (m *tokenUsageModel) windowLabel(window string) string {
	for _, summary := range m.summaries {
		if summary.Window == window && summary.Label != "" {
			return summary.Label
		}
	}
	return window
}

func uniqueSummaryValues(summaries []TokenUsageSummary, value func(TokenUsageSummary) string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, summary := range summaries {
		v := strings.TrimSpace(value(summary))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func selectedValue(values []string, index int) string {
	if len(values) == 0 {
		return ""
	}
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func selectedIndex(values []string, selected string) int {
	for i, value := range values {
		if value == selected {
			return i
		}
	}
	return 0
}

func clientLabel(client string) string {
	client = strings.TrimSpace(client)
	if client == "" {
		return "All"
	}
	if strings.EqualFold(client, "all") {
		return "All"
	}
	return strings.ToUpper(client[:1]) + client[1:]
}

func usageMetricLine(label string, value int) string {
	return fmt.Sprintf("%-16s %12s", label+":", formatInt(value))
}

func formatInt(value int) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	s := strconv.Itoa(value)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}
