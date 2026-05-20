package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type TokenUsageSnapshot struct {
	Windows     []TokenUsageWindow
	SourcePath  string
	UpdatedAt   time.Time
	RecordCount int
}

type TokenUsageWindow struct {
	Window        string
	Label         string
	TrendLabel    string
	Summary       TokenUsageSummary
	Breakdowns    []TokenUsageBreakdown
	DailySeries   []TokenUsageDailyPoint
	HeavyRequests []TokenUsageRequest
}

type TokenUsageSummary struct {
	Requests          int
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
}

type TokenUsageBreakdown struct {
	Client            string
	Model             string
	Requests          int
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
}

type TokenUsageDailyPoint struct {
	Label             string
	Requests          int
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
}

type TokenUsageRequest struct {
	Timestamp         time.Time
	Client            string
	Model             string
	Stream            bool
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
}

type TokenUsageLoader func() (TokenUsageSnapshot, error)

type tokenUsageModel struct {
	snapshot TokenUsageSnapshot
	window   int
	width    int
	height   int
	load     TokenUsageLoader
	status   string
}

type tokenUsageRefreshMsg struct {
	snapshot TokenUsageSnapshot
	err      error
}

func ShowTokenUsage(snapshot TokenUsageSnapshot, refreshers ...TokenUsageLoader) error {
	var load TokenUsageLoader
	if len(refreshers) > 0 {
		load = refreshers[0]
	}
	m := newTokenUsageModel(snapshot, load)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func RenderTokenUsageSnapshot(snapshot TokenUsageSnapshot, width, height, cursor int) (string, error) {
	m := newTokenUsageModel(snapshot)
	width, height = normalizeSnapshotSize(width, height)
	m.width = width
	m.height = height
	if cursor >= 0 && cursor < len(m.snapshot.Windows) {
		m.window = cursor
	}
	return m.View(), nil
}

func newTokenUsageModel(snapshot TokenUsageSnapshot, refreshers ...TokenUsageLoader) *tokenUsageModel {
	var load TokenUsageLoader
	if len(refreshers) > 0 {
		load = refreshers[0]
	}
	m := &tokenUsageModel{load: load}
	m.setSnapshot(snapshot)
	return m
}

func (m *tokenUsageModel) setSnapshot(snapshot TokenUsageSnapshot) {
	selectedWindow := selectedWindowValue(m.snapshot.Windows, m.window)
	m.snapshot = normalizeTokenUsageSnapshot(snapshot)
	m.window = selectedWindowIndex(m.snapshot.Windows, selectedWindow)
}

func normalizeTokenUsageSnapshot(snapshot TokenUsageSnapshot) TokenUsageSnapshot {
	if len(snapshot.Windows) == 0 {
		snapshot.Windows = []TokenUsageWindow{
			{Window: "today", Label: "Today"},
			{Window: "7d", Label: "7d"},
			{Window: "30d", Label: "30d"},
			{Window: "all", Label: "All"},
		}
	}
	for i := range snapshot.Windows {
		if snapshot.Windows[i].Window == "" {
			snapshot.Windows[i].Window = strings.ToLower(snapshot.Windows[i].Label)
		}
		if snapshot.Windows[i].Label == "" {
			snapshot.Windows[i].Label = snapshot.Windows[i].Window
		}
	}
	if snapshot.UpdatedAt.IsZero() {
		snapshot.UpdatedAt = time.Now()
	}
	return snapshot
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
		m.setSnapshot(msg.snapshot)
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
			if m.window < len(m.snapshot.Windows)-1 {
				m.window++
			}
		}
	case tea.MouseMsg:
		if !isPrimaryClick(msg.Type) {
			return m, nil
		}
		index := msg.X / 10
		if msg.Y == 3 && index >= 0 && index < len(m.snapshot.Windows) {
			m.window = index
		}
	}
	return m, nil
}

func (m *tokenUsageModel) View() string {
	if m.width == 0 {
		return "loading..."
	}
	width := m.width - 6
	if width < 72 {
		width = 72
	}
	window := m.currentWindow()
	header := dashboardHeaderStyle.Width(width).Render("Token usage")
	tabs := m.renderWindowTabs(width)
	body := pmPanelStyle.Width(width).Render(m.renderUsageBody(window, width))
	status := m.defaultStatus()
	if strings.TrimSpace(m.status) != "" {
		status = m.status
	}
	help := pmStatusBarStyle.Width(width).Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Foreground(colorText).MaxWidth(width/2).Render(status),
			lipgloss.NewStyle().Width(width-35).Align(lipgloss.Right).Foreground(colorMuted).Render("Left/Right Time - R Refresh - Q Back"),
		),
	)
	return fitToViewportHeight(dashboardFrameStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, help)), m.height)
}

func (m *tokenUsageModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.load()
		return tokenUsageRefreshMsg{snapshot: snapshot, err: err}
	}
}

func (m *tokenUsageModel) renderWindowTabs(width int) string {
	parts := make([]string, 0, len(m.snapshot.Windows))
	for i, window := range m.snapshot.Windows {
		style := dashboardMenuItemStyle.Padding(0, 2)
		if i == m.window {
			style = dashboardSelectedItemStyle.Padding(0, 2)
		}
		parts = append(parts, style.Render(window.Label))
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Top, parts...))
}

func (m *tokenUsageModel) renderUsageBody(window TokenUsageWindow, width int) string {
	contentWidth := width - 4
	lines := []string{
		lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(window.Label + " - compat proxy usage"),
		"",
	}
	if window.Summary.Requests == 0 {
		lines = append(lines,
			dashboardMutedTextStyle.Width(contentWidth).Render("No recorded compat proxy usage for this time range."),
			"",
			m.renderMetadata(contentWidth),
		)
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}
	lines = append(lines,
		m.renderOverview(window.Summary, contentWidth),
		"",
		lipgloss.NewStyle().Bold(true).Render("Breakdown by source and model"),
		m.renderBreakdownTable(window.Breakdowns, contentWidth),
		"",
		lipgloss.NewStyle().Bold(true).Render(window.trendTitle()),
		m.renderDailyTrend(window.DailySeries, contentWidth),
		"",
		lipgloss.NewStyle().Bold(true).Render("Largest requests"),
		m.renderHeavyRequests(window.HeavyRequests, contentWidth),
		"",
		m.renderMetadata(contentWidth),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *tokenUsageModel) renderOverview(summary TokenUsageSummary, width int) string {
	avg := 0
	if summary.Requests > 0 {
		avg = summary.TotalTokens / summary.Requests
	}
	inputShare := formatPercent(summary.InputTokens, summary.TotalTokens)
	outputShare := formatPercent(summary.OutputTokens, summary.TotalTokens)
	cacheRate := formatCacheRatio(summary.CachedInputTokens, summary.InputTokens)
	lines := []string{
		fmt.Sprintf("Total %s   Requests %s   Avg/request %s   Cached read %s",
			formatInt(summary.TotalTokens), formatInt(summary.Requests), formatInt(avg), formatInt(summary.CachedInputTokens)),
		fmt.Sprintf("Input %s (%s)   Output %s (%s)   Cache/input %s",
			formatInt(summary.InputTokens), inputShare, formatInt(summary.OutputTokens), outputShare, cacheRate),
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *tokenUsageModel) renderBreakdownTable(rows []TokenUsageBreakdown, width int) string {
	if len(rows) == 0 {
		return dashboardMutedTextStyle.Width(width).Render("No source/model breakdown available.")
	}
	widths := []int{10, 18, 10, 5, 8, 8, 9, 7}
	header := tableLine([]string{"Source", "Model", "Tokens", "Req", "Input", "Output", "Cached", "Cache"}, widths)
	lines := []string{dashboardMutedTextStyle.Render(header)}
	limit := minInt(len(rows), 6)
	for i := 0; i < limit; i++ {
		row := rows[i]
		lines = append(lines, tableLine([]string{
			clientLabel(row.Client),
			row.Model,
			formatInt(row.TotalTokens),
			formatInt(row.Requests),
			formatInt(row.InputTokens),
			formatInt(row.OutputTokens),
			formatInt(row.CachedInputTokens),
			formatCacheRatio(row.CachedInputTokens, row.InputTokens),
		}, widths))
	}
	if len(rows) > limit {
		lines = append(lines, dashboardMutedTextStyle.Render(fmt.Sprintf("+ %d more source/model rows", len(rows)-limit)))
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *tokenUsageModel) renderDailyTrend(points []TokenUsageDailyPoint, width int) string {
	if len(points) == 0 {
		return dashboardMutedTextStyle.Width(width).Render("No trend data available.")
	}
	points = visibleTrendPoints(points, 14)
	maxTokens := 0
	for _, point := range points {
		if point.TotalTokens > maxTokens {
			maxTokens = point.TotalTokens
		}
	}
	barWidth := width - 28
	if barWidth < 8 {
		barWidth = 8
	}
	if barWidth > 36 {
		barWidth = 36
	}
	lines := make([]string, 0, len(points))
	barStyle := lipgloss.NewStyle().Foreground(colorAccent)
	emptyStyle := lipgloss.NewStyle().Foreground(colorMuted)
	for _, point := range points {
		size := 0
		if maxTokens > 0 {
			size = point.TotalTokens * barWidth / maxTokens
			if point.TotalTokens > 0 && size == 0 {
				size = 1
			}
		}
		bar := barStyle.Render(strings.Repeat("█", size))
		if size == 0 {
			bar = emptyStyle.Render("·")
		}
		lines = append(lines, fmt.Sprintf("%-5s %9s tokens  %-*s  %s req",
			point.Label, formatInt(point.TotalTokens), barWidth, bar, formatInt(point.Requests)))
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func visibleTrendPoints(points []TokenUsageDailyPoint, limit int) []TokenUsageDailyPoint {
	if limit <= 0 {
		return points
	}
	first := -1
	last := -1
	for i, point := range points {
		if point.TotalTokens == 0 && point.Requests == 0 {
			continue
		}
		if first == -1 {
			first = i
		}
		last = i
	}
	if first == -1 {
		if len(points) <= limit {
			return points
		}
		return points[len(points)-limit:]
	}
	if first > 0 {
		first--
	}
	if last < len(points)-1 {
		last++
	}
	points = points[first : last+1]
	if len(points) > limit {
		points = points[len(points)-limit:]
	}
	return points
}

func (m *tokenUsageModel) renderHeavyRequests(rows []TokenUsageRequest, width int) string {
	if len(rows) == 0 {
		return dashboardMutedTextStyle.Width(width).Render("No request details available.")
	}
	widths := []int{12, 10, 18, 10, 11, 9}
	header := tableLine([]string{"Time", "Source", "Model", "Tokens", "In/Out", "Cached"}, widths)
	lines := []string{dashboardMutedTextStyle.Render(header)}
	limit := minInt(len(rows), 5)
	for i := 0; i < limit; i++ {
		row := rows[i]
		lines = append(lines, tableLine([]string{
			row.Timestamp.Format("01-02 15:04"),
			clientLabel(row.Client),
			row.Model,
			formatInt(row.TotalTokens),
			formatInt(row.InputTokens) + "/" + formatInt(row.OutputTokens),
			formatInt(row.CachedInputTokens),
		}, widths))
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *tokenUsageModel) renderMetadata(width int) string {
	source := m.snapshot.SourcePath
	if strings.TrimSpace(source) == "" {
		source = "unknown source"
	}
	updated := m.snapshot.UpdatedAt.Format("2006-01-02 15:04")
	return dashboardMutedTextStyle.Width(width).Render(fitText(fmt.Sprintf("Compat proxy usage only | Records %s | Updated %s | %s",
		formatInt(m.snapshot.RecordCount), updated, source), width))
}

func (m *tokenUsageModel) currentWindow() TokenUsageWindow {
	if len(m.snapshot.Windows) == 0 {
		return TokenUsageWindow{Window: "today", Label: "Today"}
	}
	if m.window < 0 || m.window >= len(m.snapshot.Windows) {
		m.window = 0
	}
	return m.snapshot.Windows[m.window]
}

func (w TokenUsageWindow) trendTitle() string {
	if strings.TrimSpace(w.TrendLabel) != "" {
		return w.TrendLabel
	}
	if w.Window == "today" {
		return "Hourly trend"
	}
	return "Daily trend"
}

func (m *tokenUsageModel) defaultStatus() string {
	return fmt.Sprintf("Compat proxy usage only | %s records", formatInt(m.snapshot.RecordCount))
}

func selectedWindowValue(windows []TokenUsageWindow, index int) string {
	if index >= 0 && index < len(windows) {
		return windows[index].Window
	}
	return ""
}

func selectedWindowIndex(windows []TokenUsageWindow, selected string) int {
	if selected == "" {
		return 0
	}
	for i, window := range windows {
		if window.Window == selected {
			return i
		}
	}
	return 0
}

func tableLine(values []string, widths []int) string {
	parts := make([]string, 0, len(values))
	for i, value := range values {
		width := widths[i]
		parts = append(parts, padRight(fitText(value, width), width))
	}
	return strings.Join(parts, " ")
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}

func fitText(value string, width int) string {
	if width <= 0 || len(value) <= width {
		return value
	}
	if width <= 1 {
		return value[:width]
	}
	return value[:width-1] + "."
}

func formatPercent(part, total int) string {
	if total <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(total))
}

func formatCacheRatio(cached, input int) string {
	if input <= 0 || cached > input {
		return "n/a"
	}
	return formatPercent(cached, input)
}

func usageMetricLine(label string, value int) string {
	return fmt.Sprintf("%-18s %12s", label+":", formatInt(value))
}

func formatInt(value int) string {
	s := strconv.Itoa(value)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	prefix := len(s) % 3
	if prefix == 0 {
		prefix = 3
	}
	b.WriteString(s[:prefix])
	for i := prefix; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func clientLabel(client string) string {
	client = strings.TrimSpace(client)
	if client == "" {
		return "Unknown"
	}
	if strings.EqualFold(client, "all") {
		return "All"
	}
	return strings.ToUpper(client[:1]) + client[1:]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
