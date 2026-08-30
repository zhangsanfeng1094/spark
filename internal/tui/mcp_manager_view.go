package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m *mcpManagerModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	header := dashboardHeaderStyle.Width(m.width - 6).Render("MCP Manager")
	leftW := m.leftPaneWidth()
	rightW := m.width - leftW - 4
	if rightW < 44 {
		rightW = 44
	}

	statusBar := pmStatusBarStyle.Width(m.width - 4).Render(
		lipgloss.NewStyle().Align(lipgloss.Right).Foreground(colorMuted).Render(m.contextHelpText()),
	)

	left := m.renderServerList(0)
	right := m.renderDetails()
	paneInnerH := max(lipgloss.Height(left), lipgloss.Height(right))
	availableOuterH := m.height - lipgloss.Height(header) - lipgloss.Height(statusBar)
	if availableOuterH > 2 {
		paneInnerH = availableOuterH - 2
	}
	left = m.renderServerList(paneInnerH)

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.leftPaneStyle(leftW).Render(left),
		m.rightPaneStyle(rightW).Render(right),
	)

	return fitToViewportHeight(pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar)), m.height)
}

func (m *mcpManagerModel) leftPaneWidth() int {
	leftW := 34
	if m.width > 0 && m.width < 100 {
		leftW = 36
	}
	if m.width > 0 && leftW > m.width/2 {
		leftW = m.width / 2
	}
	if leftW < 34 {
		return 34
	}
	return leftW
}

func (m *mcpManagerModel) leftPaneItemWidth() int {
	return max(28, m.leftPaneWidth()-4)
}

func (m *mcpManagerModel) leftPaneStyle(width int) lipgloss.Style {
	style := pmPanelStyle.Width(width)
	if !m.editing && !m.transferring && m.browseFocus != mcpBrowseFocusActions {
		return pmFocusedPanelStyle.Width(width)
	}
	return style
}

func (m *mcpManagerModel) rightPaneStyle(width int) lipgloss.Style {
	style := pmPanelStyle.Width(width)
	if m.editing || m.transferring || m.browseFocus == mcpBrowseFocusActions {
		return pmFocusedPanelStyle.Width(width)
	}
	return style
}

func (m *mcpManagerModel) renderStatusBar() string {
	if m.confirmDelete {
		return m.status
	}
	statusMain, statusLog := splitStatusText(m.status)
	if statusMain == "" {
		statusMain = "Ready."
	}
	main := styleStatusMain(statusMain)
	help := lipgloss.NewStyle().Foreground(colorDim).Align(lipgloss.Right).Render(m.contextHelpText())
	if statusLog == "" {
		return lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(m.width/2).Render(main),
			lipgloss.NewStyle().Width(max(0, m.width/2-6)).Render(help),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(m.width/2).Render(main),
			lipgloss.NewStyle().Width(max(0, m.width/2-6)).Render(help),
		),
		pmStatusLogStyle.Render("↳ "+statusLog),
	)
}

func (m *mcpManagerModel) statusSummaryText() string {
	main, _ := splitStatusText(m.status)
	main = strings.TrimSpace(main)
	if main == "" || strings.EqualFold(main, "Ready.") || strings.EqualFold(main, "Ready") {
		return "No recent activity"
	}
	return main
}

func (m *mcpManagerModel) renderStatusSection(width int) []string {
	title := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Status")
	statusText := m.statusSummaryText()
	return []string{
		title,
		lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(statusText),
	}
}

func (m *mcpManagerModel) renderHelpSection(width int) []string {
	title := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Help")
	return []string{
		title,
		lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(m.contextHelpText()),
	}
}

func (m *mcpManagerModel) contextHelpText() string {
	if m.confirmDelete {
		return "Y confirm • N/Esc/Q cancel"
	}
	if m.editing {
		if m.editorMode == mcpEditorModeRaw {
			return "F2 Save • F5 Save & Probe • F3 Form • Esc Cancel"
		}
		return "Tab Move • F4 Raw • F2 Save • F5 Save & Probe • Esc Cancel"
	}
	if m.transferring {
		return "Up/Down Select • Enter Run Transfer • Esc Cancel"
	}
	switch m.browseFocus {
	case mcpBrowseFocusQuickAdd:
		return "Enter Run Action • Tab Next Zone • A Add • T Transfer • " + screenBackHelp
	case mcpBrowseFocusServers:
		return "Up/Down Select • Tab Actions • E Edit • P Probe • Space Toggle • " + screenBackHelp
	case mcpBrowseFocusActions:
		return "Up/Down Action • Enter Run • Tab Next Zone • " + screenBackHelp
	default:
		return screenBackHelp
	}
}

func (m *mcpManagerModel) renderServerList(height int) string {
	lines := []string{
		lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("MCP Servers"),
		"",
	}
	if len(m.names) == 0 {
		lines = append(lines, pmItemStyle.Width(m.leftPaneItemWidth()).Render("  No servers configured"))
	} else {
		for i, name := range m.names {
			lines = append(lines, m.renderServerRow(i, name))
		}
	}
	return joinTopAndBottom(lines, m.renderLeftPaneBottom(), height)
}

func (m *mcpManagerModel) renderLeftPaneBottom() []string {
	width := m.leftPaneItemWidth()
	lines := []string{"", lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Current")}
	name := m.currentName()
	if name == "" {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render("No server selected"),
			lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render("Use Add to create one."),
		)
	} else {
		server := m.cfg.GetMcpServer(name)
		status := m.currentStatus()
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorTextSoft).Width(width).Render(truncateDisplay(name, width)),
			lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(fmt.Sprintf("%s %s", renderStatusBadge(status), strings.Title(status.Headline))),
			lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(transportLabel(server)),
		)
	}

	addBtn := pmLeftBtnStyle.Copy().MarginRight(0).Render("[A] Add")
	transferBtn := pmLeftBtnStyle.Copy().MarginRight(0).Render("[T] Transfer")
	if m.browseFocus == mcpBrowseFocusQuickAdd {
		if m.quickAddIndex == 0 {
			addBtn = pmLeftActiveBtnStyle.Copy().MarginRight(0).Render("[A] Add")
		} else if m.quickAddIndex == 1 {
			transferBtn = pmLeftActiveBtnStyle.Copy().MarginRight(0).Render("[T] Transfer")
		}
	}
	gap := "  "
	lines = append(lines, "", lipgloss.JoinHorizontal(lipgloss.Top, addBtn, gap, transferBtn))
	return lines
}

func (m *mcpManagerModel) renderDetails() string {
	if m.editing {
		return m.renderEditor()
	}
	if m.transferring {
		return m.renderTransferMenu()
	}
	name := m.currentName()
	if name == "" {
		return m.renderEmptyState()
	}
	server := m.cfg.GetMcpServer(name)
	status := m.currentStatus()
	contentWidth := m.detailContentWidth()
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Config · " + name),
		"",
	}
	lines = append(lines, renderMCPConfigRow("Status", fmt.Sprintf("%s %s", renderStatusBadge(status), strings.Title(status.Headline)), contentWidth))
	lines = append(lines, renderMCPConfigRow("Transport", transportLabel(server), contentWidth))
	lines = append(lines, renderMCPConfigRow("Enabled", fmt.Sprintf("%t", server != nil && server.Enabled), contentWidth))
	if server != nil {
		if strings.TrimSpace(server.Command) != "" {
			lines = append(lines, renderMCPConfigRow("Command", server.Command, contentWidth))
		}
		if len(server.Args) > 0 {
			lines = append(lines, renderMCPConfigRow("Args", fmt.Sprintf("%d configured", len(server.Args)), contentWidth))
			lines = append(lines, renderMCPConfigRow("Arg Preview", strings.Join(server.Args, " "), contentWidth))
		}
		if strings.TrimSpace(server.URL) != "" {
			lines = append(lines, renderMCPConfigRow("URL", server.URL, contentWidth))
		}
		if len(server.Env) > 0 {
			lines = append(lines, renderMCPConfigRow("Env", fmt.Sprintf("%d entries", len(server.Env)), contentWidth))
		}
		if strings.TrimSpace(server.DisabledReason) != "" {
			lines = append(lines, renderMCPConfigRow("Disabled reason", server.DisabledReason, contentWidth))
		}
	}
	if probe := m.probes[name]; probe != nil {
		lines = append(lines, renderMCPConfigRow("Last probe", probe.ProbedAt.Format(time.RFC3339), contentWidth))
		lines = append(lines, renderMCPConfigRow("Latency", probe.Latency.Round(time.Millisecond).String(), contentWidth))
		if probe.ToolsCount > 0 {
			lines = append(lines, renderMCPConfigRow("Tools detected", fmt.Sprintf("%d", probe.ToolsCount), contentWidth))
		}
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Actions"))
	for i, action := range m.browseActions() {
		lines = append(lines, m.renderActionItem(i, action, contentWidth))
	}
	lines = append(lines, "", lipgloss.NewStyle().Bold(true).Render("Diagnostics"))
	lines = append(lines, m.renderDiagnostics(status, m.probes[name])...)
	lines = append(lines, "")
	lines = append(lines, m.renderStatusSection(contentWidth)...)
	lines = append(lines, "")
	lines = append(lines, m.renderHelpSection(contentWidth)...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) detailContentWidth() int {
	if m.width <= 0 {
		return 76
	}
	return max(36, m.width-m.leftPaneWidth()-12)
}

func renderMCPConfigRow(label, value string, width int) string {
	labelW := 16
	inputW := max(24, width-labelW-5)
	if strings.TrimSpace(value) == "" {
		value = " "
	}
	labelStyle := pmLabelStyle.Copy().Width(labelW)
	inputStyle := pmCompactReadOnlyInputStyle.Copy().Width(inputW)
	divider := lipgloss.NewStyle().Foreground(colorBorder).Render("│")
	return lipgloss.JoinHorizontal(lipgloss.Center,
		labelStyle.Render(label),
		divider,
		inputStyle.Render(truncateDisplay(value, inputW-2)),
	)
}

func (m *mcpManagerModel) renderEmptyState() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("No MCP servers yet"),
		"",
		"Manage local and remote MCP endpoints from one place.",
		"Recommended path: create a server, choose transport in the editor, then run a probe.",
		"",
		lipgloss.NewStyle().Bold(true).Render("Start Here"),
		"• Create MCP Server",
		"• Choose transport in the editor",
		"• Save and probe to verify the endpoint",
		"",
		lipgloss.NewStyle().Foreground(colorDim).Render("The Actions list on the left is the default focus."),
	}
	lines = append(lines, "")
	lines = append(lines, m.renderStatusSection(max(36, m.width-50))...)
	lines = append(lines, "")
	lines = append(lines, m.renderHelpSection(max(36, m.width-50))...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderEditor() string {
	title := "Edit Server"
	if m.adding {
		title = "Create Server"
	}
	if !m.adding && m.editOriginalName != "" {
		title += ": " + m.editOriginalName
	}
	modeForm := renderMCPModeToggle("F3 Form", m.editorMode == mcpEditorModeForm)
	modeRaw := renderMCPModeToggle("F4 Raw YAML/JSON", m.editorMode == mcpEditorModeRaw)
	if m.editorMode == mcpEditorModeRaw {
		modeForm = renderMCPModeToggle("F3 Form", false)
		modeRaw = renderMCPModeToggle("F4 Raw YAML/JSON", true)
	}
	contentWidth := max(36, m.width-50)
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(title),
		fmt.Sprintf("View: %s   %s", modeForm, modeRaw),
		lipgloss.NewStyle().Foreground(colorDim).Render(m.editorIntroText()),
		"",
	}
	if m.editorMode == mcpEditorModeForm {
		for _, i := range m.visibleEditFieldIndices() {
			field := m.editFields[i]
			value := m.renderEditorFieldValue(i, field, i == m.editFocus)
			label := fmt.Sprintf("%-16s", field.label)
			style := pmCompactInputStyle.Copy().Width(max(24, m.width-64))
			labelStyle := pmLabelStyle
			dividerColor := colorBorder
			if i == m.editFocus {
				style = pmCompactFocusedInputStyle.Copy().Width(max(24, m.width-64))
				labelStyle = pmFocusedLabelStyle
				dividerColor = colorFocus
			}
			divider := lipgloss.NewStyle().Foreground(dividerColor).Render("│")
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Center, labelStyle.Render(label), divider, style.Render(value)))
		}
		if hint := m.editorFieldHint(); hint != "" {
			lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render(hint))
		}
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render("Tab/Shift+Tab move fields. Left/Right changes select fields."))
	} else {
		editorText := renderCursorText(m.rawEditor, m.rawCursor)
		lines = append(lines, pmFocusedInputStyle.Copy().Width(contentWidth).Height(max(12, m.height-14)).Render(editorText))
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render("Tab exits editor. Format must be valid YAML/JSON."))
	}
	actionTitle := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Actions")
	saveBtn := pmCompactPrimaryBtnStyle.Copy().MarginRight(0).Render("[F2] Save")
	probeBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("[F5] Save & Probe")
	cancelBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("[Esc] Cancel")
	actionRow1 := lipgloss.JoinHorizontal(lipgloss.Top, probeBtn, lipgloss.PlaceHorizontal(max(2, contentWidth-lipgloss.Width(probeBtn)-lipgloss.Width(saveBtn)), lipgloss.Right, saveBtn))
	actionRow2 := cancelBtn
	lines = append(lines, "", actionTitle, actionRow1, actionRow2, "")
	lines = append(lines, m.renderStatusSection(contentWidth)...)
	lines = append(lines, "")
	lines = append(lines, m.renderHelpSection(contentWidth)...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func renderMCPModeToggle(label string, selected bool) string {
	if selected {
		return "(●) " + label
	}
	return "( ) " + label
}

func (m *mcpManagerModel) renderEditorFieldValue(index int, field mcpEditField, focused bool) string {
	value := field.value
	if field.kind == mcpEditKindSelect {
		value = renderMCPSelectSegments(field.options, field.value, focused)
	}
	if field.kind != mcpEditKindSelect && focused {
		cursor := m.editCursor[index]
		value = renderCursorText(value, cursor)
	} else if value == "" {
		value = " "
	}
	return value
}

func renderMCPSelectSegments(options []string, current string, focused bool) string {
	if len(options) == 0 {
		return " "
	}
	current = strings.TrimSpace(current)
	parts := make([]string, 0, len(options))
	for _, option := range options {
		label := " " + option + " "
		selected := option == current
		switch {
		case selected && focused:
			parts = append(parts, lipgloss.NewStyle().Foreground(colorText).Background(colorFocus).Bold(true).Render(label))
		case selected:
			parts = append(parts, lipgloss.NewStyle().Foreground(colorText).Background(lipgloss.Color("#3a334a")).Bold(true).Render(label))
		case focused:
			parts = append(parts, lipgloss.NewStyle().Foreground(colorTextSoft).Background(colorFieldBgFocus).Render(label))
		default:
			parts = append(parts, lipgloss.NewStyle().Foreground(colorMuted).Background(colorFieldBg).Render(label))
		}
	}
	separator := lipgloss.NewStyle().Foreground(colorBorder).Render(" ")
	return lipgloss.JoinHorizontal(lipgloss.Center, joinWithSeparator(parts, separator)...)
}

func joinWithSeparator(parts []string, separator string) []string {
	if len(parts) <= 1 {
		return parts
	}
	out := make([]string, 0, len(parts)*2-1)
	for i, part := range parts {
		if i > 0 {
			out = append(out, separator)
		}
		out = append(out, part)
	}
	return out
}

func (m *mcpManagerModel) editorIntroText() string {
	if m.adding {
		transport := ""
		if len(m.editFields) > mcpEditFieldTransport {
			transport = strings.TrimSpace(m.editFields[mcpEditFieldTransport].value)
		}
		return fmt.Sprintf("Fill the minimum fields for a %s server, then save or probe.", transport)
	}
	return "Review the current server, then edit only the fields that need to change."
}

func (m *mcpManagerModel) editorFieldHint() string {
	switch m.editFocus {
	case mcpEditFieldCommand:
		return "Command: executable name or absolute path used to launch the stdio server."
	case mcpEditFieldArgs:
		return "Args: one argument per line."
	case mcpEditFieldEnv:
		return "Env: use KEY=value, one per line."
	case mcpEditFieldURL:
		return "URL: point directly to the MCP endpoint."
	default:
		return ""
	}
}

func containsEditField(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func filterPrintableRunes(runes []rune) []rune {
	out := make([]rune, 0, len(runes))
	for _, r := range runes {
		if r < 32 {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (m *mcpManagerModel) renderQuickAddItem(i int, item mcpQuickAddItem) string {
	title := item.Label
	summary := m.quickAddSummary(item)
	style := pmItemStyle.Copy().Width(m.leftPaneItemWidth())
	prefix := "  "
	if i == m.quickAddIndex {
		if m.browseFocus == mcpBrowseFocusQuickAdd {
			style = pmFocusedItemStyle.Copy().Width(m.leftPaneItemWidth())
			prefix = "➤ "
		} else {
			style = pmSelectedItemStyle.Copy().Width(m.leftPaneItemWidth())
			prefix = "◆ "
		}
	}
	return style.Render(prefix + title + "\n" + strings.Repeat(" ", len(prefix)) + "  " + summary)
}

func (m *mcpManagerModel) quickAddSummary(item mcpQuickAddItem) string {
	switch item.Key {
	case "add":
		return "New server"
	case "transfer":
		return "Import/Export"
	default:
		return item.Description
	}
}

func (m *mcpManagerModel) renderServerRow(i int, name string) string {
	server := m.cfg.GetMcpServer(name)
	status := summarizeMCPStatus(name, server, m.probes[name])
	detail := transportLabel(server)
	if server != nil && !server.Enabled {
		detail += " • disabled"
	}
	if m.running[name] {
		detail += " • probing"
	}
	line1 := fmt.Sprintf("%s %s", renderStatusBadge(status), name)
	style := pmItemStyle.Copy().Width(m.leftPaneItemWidth())
	prefix := "  "
	if i == m.selected {
		if m.browseFocus == mcpBrowseFocusServers {
			style = pmFocusedItemStyle.Copy().Width(m.leftPaneItemWidth())
			prefix = "➤ "
		} else {
			style = pmSelectedItemStyle.Copy().Width(m.leftPaneItemWidth())
			prefix = "◆ "
		}
	}
	return style.Render(prefix + line1 + "\n" + strings.Repeat(" ", len(prefix)) + "  " + detail)
}

func (m *mcpManagerModel) renderDiagnostics(status mcpStatusSummary, probe *mcpProbeResult) []string {
	lines := []string{
		fmt.Sprintf("Current State: %s %s", renderStatusBadge(status), strings.Title(status.Headline)),
		fmt.Sprintf("Why: %s", status.Detail),
	}
	nextAction := "No action needed."
	if len(status.Suggestions) > 0 {
		nextAction = status.Suggestions[0]
	}
	if status.Kind == mcpStatusUnknown {
		nextAction = "Run Probe to verify initialize and tools/list."
	}
	if probe != nil && probe.Err != "" {
		lines = append(lines, fmt.Sprintf("Failure Stage: %s", probe.Stage))
	}
	lines = append(lines, fmt.Sprintf("Next Action: %s", nextAction))
	if len(status.Suggestions) > 1 {
		lines = append(lines, "Suggested fixes:")
		for _, suggestion := range status.Suggestions[1:min(3, len(status.Suggestions))] {
			lines = append(lines, "• "+suggestion)
		}
	}
	return lines
}

func (m *mcpManagerModel) renderActionItem(i int, action mcpActionItem, width int) string {
	label := fmt.Sprintf("%s  %s", action.Label, lipgloss.NewStyle().Foreground(colorDim).Render(action.Description))
	style := pmItemStyle.Copy().Width(width)
	prefix := "  "
	if m.browseFocus == mcpBrowseFocusActions && i == m.actionIndex {
		style = pmSelectedItemStyle.Copy().Width(width)
		prefix = "➤ "
	}
	return style.Render(prefix + label)
}

func (m *mcpManagerModel) renderTransferMenu() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Transfer MCP Servers"),
		"",
		"Choose where to import from or export to.",
		"",
	}
	for i, item := range m.transferItems {
		lines = append(lines, m.renderTransferItem(i, item))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorDim).Render("Import skips same-name Spark servers. Export overwrites same-name target servers."))
	lines = append(lines, "")
	lines = append(lines, m.renderStatusSection(max(36, m.width-50))...)
	lines = append(lines, "")
	lines = append(lines, m.renderHelpSection(max(36, m.width-50))...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderTransferItem(i int, item mcpTransferItem) string {
	label := fmt.Sprintf("%s  %s", item.Label, lipgloss.NewStyle().Foreground(colorDim).Render(item.Description))
	style := pmItemStyle.Copy().Width(max(24, m.width-48))
	prefix := "  "
	if i == m.transferIndex {
		style = pmSelectedItemStyle.Copy().Width(max(24, m.width-48))
		prefix = "➤ "
	}
	return style.Render(prefix + label)
}

func renderStatusBadge(status mcpStatusSummary) string {
	switch status.Kind {
	case mcpStatusConfigured, mcpStatusReachable:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Bold(true).Render(status.Badge)
	case mcpStatusBroken:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Bold(true).Render(status.Badge)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c")).Bold(true).Render(status.Badge)
	}
}

