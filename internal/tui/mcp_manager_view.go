package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the unified 2-column MCP Workbench + Overlay Modals.
func (m *mcpManagerModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	header := dashboardHeaderStyle.Width(m.width - 6).Render(m.renderHeaderSummary())

	leftPanelW := 34
	if m.width < 100 {
		leftPanelW = 28
	}
	if leftPanelW > m.width/3 && m.width < 75 {
		leftPanelW = max(22, m.width/3)
	}
	m.leftPanelWidth = leftPanelW

	availableW := m.width - 10
	if availableW < 40 {
		availableW = 40
	}
	rightPanelW := availableW - leftPanelW
	if rightPanelW < 24 {
		rightPanelW = 24
	}
	inputW := rightPanelW - pmLabelWidth - 5
	if inputW < 14 {
		inputW = 14
	}
	m.inputWidth = inputW

	footerText := m.contextHelpText()
	footer := pmStatusBarStyle.Width(m.width - 4).Render(
		lipgloss.NewStyle().Align(lipgloss.Right).Foreground(colorMuted).Render(footerText),
	)

	availableOuterH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	paneInnerH := 20
	if availableOuterH > 2 {
		paneInnerH = availableOuterH - 2
	}

	leftPane := m.renderLeftPane(leftPanelW, paneInnerH)
	rightPane := m.renderRightPane(rightPanelW, paneInnerH)

	paneHeight := max(lipgloss.Height(leftPane), lipgloss.Height(rightPane))
	if paneInnerH > 0 {
		paneHeight = paneInnerH
	}

	leftStyle := pmPanelStyle.Width(leftPanelW).Height(paneHeight)
	rightStyle := pmPanelStyle.Width(rightPanelW).Height(paneHeight)

	if m.focusArea == mcpFocusList {
		leftStyle = leftStyle.BorderForeground(colorBorderFocus)
	} else {
		rightStyle = rightStyle.BorderForeground(colorBorderFocus)
	}

	leftRendered := leftStyle.Render(leftPane)
	rightRendered := rightStyle.Render(rightPane)
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftRendered, rightRendered)

	appMarginX := 1
	appMarginY := 0
	bodyX := appMarginX
	bodyY := appMarginY + lipgloss.Height(header)
	leftRenderedW := lipgloss.Width(leftRendered)

	offsetX := pmBorderSize + pmPaddingH
	offsetY := pmBorderSize + pmPaddingV
	m.leftContentX = bodyX + offsetX
	m.leftContentY = bodyY + offsetY
	m.rightContentX = bodyX + leftRenderedW + offsetX
	m.rightContentY = bodyY + offsetY

	ui := pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))

	if m.modalKind != mcpModalNone {
		return fitToViewportHeight(m.overlayModal(ui), m.height)
	}
	return fitToViewportHeight(ui, m.height)
}

func (m *mcpManagerModel) renderHeaderSummary() string {
	total := len(m.cfg.McpServers)
	healthy, errorCount, unknown, disabled := 0, 0, 0, 0
	for name, srv := range m.cfg.McpServers {
		if srv != nil && !srv.Enabled {
			disabled++
			continue
		}
		status := summarizeMCPStatus(name, srv, m.probes[name])
		switch status.Kind {
		case mcpStatusReachable, mcpStatusConfigured:
			healthy++
		case mcpStatusBroken:
			errorCount++
		default:
			unknown++
		}
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("MCP Manager")

	var statusParts []string
	statusParts = append(statusParts, fmt.Sprintf("%d servers", total))
	if errorCount > 0 {
		statusParts = append(statusParts, lipgloss.NewStyle().Foreground(colorError).Bold(true).Render(fmt.Sprintf("%d error", errorCount)))
	}
	if healthy > 0 {
		statusParts = append(statusParts, lipgloss.NewStyle().Foreground(colorSuccess).Render(fmt.Sprintf("%d healthy", healthy)))
	}
	if unknown > 0 {
		statusParts = append(statusParts, lipgloss.NewStyle().Foreground(colorWarning).Render(fmt.Sprintf("%d unknown", unknown)))
	}
	if disabled > 0 {
		statusParts = append(statusParts, lipgloss.NewStyle().Foreground(colorDim).Render(fmt.Sprintf("%d disabled", disabled)))
	}
	counts := strings.Join(statusParts, " · ")

	shortcuts := lipgloss.NewStyle().Foreground(colorMuted).Render("Click to select • Tab to switch")
	return fmt.Sprintf("%s    %s    %s", title, counts, shortcuts)
}

func (m *mcpManagerModel) renderLeftPane(width, height int) string {
	listWidth := width - 2
	if listWidth < 20 {
		listWidth = 20
	}

	listTitle := "Servers"
	if m.searching {
		listTitle = fmt.Sprintf("Search: %s_", m.searchQuery)
	} else if m.filterOption != mcpFilterAll {
		listTitle = fmt.Sprintf("Servers [%s]", filterOptionLabel(m.filterOption))
	}

	topLines := []string{
		lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render(listTitle),
		"",
	}
	m.leftVisibleRows = nil
	m.leftVisibleIdxs = nil

	bottomLines := m.renderLeftPaneBottom(listWidth)

	if len(m.filtered) == 0 {
		topLines = append(topLines, pmItemStyle.Width(listWidth).Render("  No servers configured"))
	} else {
		visibleSlots := len(m.filtered)
		if height > 0 {
			overhead := lipgloss.Height(lipgloss.JoinVertical(lipgloss.Left, append(topLines, bottomLines...)...))
			visibleSlots = max(1, height-overhead)
		}
		start, end, showUp, showDown := profileWindow(len(m.filtered), m.selected, visibleSlots)
		if showUp {
			topLines = append(topLines, lipgloss.NewStyle().Foreground(colorDim).Width(listWidth).Render("  ↑ more"))
		}
		for i := start; i < end; i++ {
			topLines = append(topLines, m.renderServerRow(i, m.filtered[i], listWidth-2))
			m.leftVisibleRows = append(m.leftVisibleRows, len(topLines)-1)
			m.leftVisibleIdxs = append(m.leftVisibleIdxs, i)
		}
		if showDown {
			topLines = append(topLines, lipgloss.NewStyle().Foreground(colorDim).Width(listWidth).Render("  ↓ more"))
		}
	}

	content := joinTopAndBottom(topLines, bottomLines, height)
	finalLines := strings.Split(content, "\n")
	fillerOffset := len(finalLines) - len(topLines) - len(bottomLines)
	if fillerOffset < 0 {
		fillerOffset = 0
	}
	m.leftButtonsRelY += fillerOffset
	m.leftButtonsRow2Y += fillerOffset
	return content
}

func (m *mcpManagerModel) renderLeftPaneBottom(width int) []string {
	btnStyle := pmLeftBtnStyle.Copy().MarginRight(0)

	addBtn := btnStyle.Render("Add")
	importBtn := btnStyle.Render("Import")
	transferBtn := btnStyle.Render("Transfer")
	refreshBtn := btnStyle.Render("Refresh")

	btnGap := "  "
	if width < 26 {
		btnGap = " "
	}
	col1W := max(lipgloss.Width(addBtn), lipgloss.Width(transferBtn))
	col2W := max(lipgloss.Width(importBtn), lipgloss.Width(refreshBtn))
	btnRow1 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(col1W).Render(addBtn),
		btnGap,
		lipgloss.NewStyle().Width(col2W).Render(importBtn),
	)
	btnRow2 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(col1W).Render(transferBtn),
		btnGap,
		lipgloss.NewStyle().Width(col2W).Render(refreshBtn),
	)
	lines := []string{"", btnRow1, btnRow2}
	m.leftButtonsRelY = len(lines) - 2
	m.leftButtonsRow2Y = len(lines) - 1
	m.leftButtonsRelH = lipgloss.Height(btnRow1)
	m.leftButtonsRowW = lipgloss.Width(btnRow1)
	m.leftButtonsRow2W = lipgloss.Width(btnRow2)
	m.leftAddBtnW = lipgloss.Width(addBtn) + lipgloss.Width(btnGap)
	m.leftImportBtnW = lipgloss.Width(importBtn)
	m.leftTransferBtnW = lipgloss.Width(transferBtn) + lipgloss.Width(btnGap)
	m.leftRefreshBtnW = lipgloss.Width(refreshBtn)
	return lines
}

func (m *mcpManagerModel) renderServerRow(i int, name string, itemWidth int) string {
	server := m.cfg.GetMcpServer(name)
	status := summarizeMCPStatus(name, server, m.probes[name])
	isFocused := (i == m.selected)

	prefix := "  "
	if isFocused {
		prefix = "▶ "
	}

	var icon string
	switch status.Kind {
	case mcpStatusReachable, mcpStatusConfigured:
		icon = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("●")
	case mcpStatusBroken:
		icon = lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
	default:
		if server != nil && !server.Enabled {
			icon = lipgloss.NewStyle().Foreground(colorDim).Render("○")
		} else {
			icon = lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("?")
		}
	}

	enabled := server != nil && server.Enabled
	stateLabel := "On "
	stateStyle := lipgloss.NewStyle().Foreground(colorSuccess)
	if !enabled {
		stateLabel = "Off"
		stateStyle = lipgloss.NewStyle().Foreground(colorDim)
	}

	transportStr := strings.ToUpper(transportLabel(server))
	if len(transportStr) > 8 {
		transportStr = transportStr[:8]
	}

	nameW := max(6, itemWidth-20)
	displayName := truncateDisplay(name, nameW)

	nameStyle := lipgloss.NewStyle().Width(nameW).Foreground(colorTextSoft)
	if isFocused {
		nameStyle = nameStyle.Foreground(colorText).Bold(true)
	} else if !enabled {
		nameStyle = nameStyle.Foreground(colorDim)
	}

	transportCol := lipgloss.NewStyle().Width(8).Foreground(colorMuted).Render(transportStr)
	stateCol := stateStyle.Render(stateLabel)

	content := fmt.Sprintf("%s%s %s %s %s", prefix, icon, nameStyle.Render(displayName), transportCol, stateCol)
	style := pmItemStyle.Copy().Width(itemWidth)
	if isFocused {
		if m.focusArea == mcpFocusList {
			style = pmFocusedItemStyle.Copy().Width(itemWidth)
		} else {
			style = pmSelectedMutedItemStyle.Copy().Width(itemWidth)
		}
	}
	return style.Render(content)
}

func (m *mcpManagerModel) renderRightPane(width, height int) string {
	name := m.currentName()
	if name == "" {
		return m.renderEmptyState()
	}
	server := m.cfg.GetMcpServer(name)
	status := m.currentStatus()
	probe := m.probes[name]

	inputW := max(16, width-pmLabelWidth-5)
	contentWidth := pmLabelWidth + 1 + inputW

	var lines []string
	relY := 0

	// 1. Server Summary Header: Identity & Status Indicator
	statusIcon := "●"
	statusColor := colorSuccess
	statusText := "Healthy"

	switch status.Kind {
	case mcpStatusBroken:
		statusIcon = "✕"
		statusColor = colorError
		statusText = "Error"
	case mcpStatusUnknown:
		if server != nil && !server.Enabled {
			statusIcon = "○"
			statusColor = colorDim
			statusText = "Disabled"
		} else {
			statusIcon = "?"
			statusColor = colorWarning
			statusText = "Unknown"
		}
	}

	badge := lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusIcon + " " + statusText)
	nameTitle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(name)
	if m.dirty {
		nameTitle += " " + lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("● Modified")
	}

	headerGap := max(2, contentWidth-lipgloss.Width(nameTitle)-lipgloss.Width(badge))
	summaryRow := lipgloss.JoinHorizontal(lipgloss.Top, nameTitle, strings.Repeat(" ", headerGap), badge)
	lines = append(lines, summaryRow)
	relY += lipgloss.Height(summaryRow)

	// Endpoint / Command subtitle
	targetInfo := ""
	if server != nil {
		if isHTTPMCPServer(server) {
			targetInfo = fmt.Sprintf("HTTP · %s", server.URL)
		} else if server.Command != "" {
			targetInfo = fmt.Sprintf("STDIO · %s", server.Command)
		}
	}
	if targetInfo != "" {
		subLine := lipgloss.NewStyle().Foreground(colorMuted).Render(truncateDisplay(targetInfo, contentWidth))
		lines = append(lines, subLine)
		relY += lipgloss.Height(subLine)
	}

	// Effective Status Note / Override
	if server != nil && !server.Enabled {
		overrideMsg := "Disabled by Spark"
		if server.DisabledReason != "" {
			overrideMsg = fmt.Sprintf("Disabled: %s", server.DisabledReason)
		}
		overrideLine := lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render(overrideMsg)
		lines = append(lines, overrideLine)
		relY += lipgloss.Height(overrideLine)
	}

	lines = append(lines, "")
	relY++

	// 2. Configuration Section
	secConfig := renderFormSectionHeader("Configuration", contentWidth)
	lines = append(lines, secConfig)
	relY += lipgloss.Height(secConfig)

	// Editable Form Fields
	visible := m.visibleFieldIndices()
	m.fieldStartRelY = make([]int, len(visible))
	m.fieldEndRelY = make([]int, len(visible))
	m.fieldActualIndices = make([]int, len(visible))

	for fIdx, actualIdx := range visible {
		m.fieldActualIndices[fIdx] = actualIdx
		field := &m.draftFields[actualIdx]
		focused := (m.focusArea == mcpFocusFields && fIdx == m.focusField)

		var inputView string
		if field.Kind == mcpFieldKindSelect {
			inputView = renderSegmentedPills(field.Options, field.Value, focused, inputW)
		} else {
			if field.Input.Value() != field.Value {
				field.Input.SetValue(field.Value)
				field.Input.CursorEnd()
			}
			field.Input.Width = max(10, inputW-2)
			field.Input.Placeholder = field.Placeholder
			if focused {
				field.Input.Focus()
			} else {
				field.Input.Blur()
			}
			inputView = field.Input.View()
		}

		row := renderCompactFormRow(compactFormRowOptions{
			Label:       field.Label,
			Value:       field.Value,
			Placeholder: field.Placeholder,
			Width:       inputW,
			Focused:     focused,
			ReadOnly:    field.Kind == mcpFieldKindSelect,
			Required:    actualIdx == mcpFieldKeyName,
			InputView:   inputView,
		})
		rowH := lipgloss.Height(row)
		m.fieldStartRelY[fIdx] = relY
		m.fieldEndRelY[fIdx] = relY + rowH - 1
		lines = append(lines, row)
		relY += rowH
	}

	lines = append(lines, "")
	relY++

	// 3. Actions Section (follows directly under Configuration)
	actionsHeader := renderFormSectionHeader("Actions", contentWidth)
	probeBtn := m.renderActionBtn(0, "Probe")
	saveBtn := m.renderActionBtn(1, "Save")
	vimBtn := m.renderActionBtn(2, "Edit Raw")
	deleteBtn := m.renderActionBtn(3, "Delete")
	actionRow := lipgloss.JoinHorizontal(lipgloss.Top, probeBtn, "  ", saveBtn, "  ", vimBtn, "  ", deleteBtn)
	lines = append(lines, actionsHeader, actionRow, "")
	relY += lipgloss.Height(actionsHeader) + lipgloss.Height(actionRow) + 1

	m.rightButtonsRelY = relY - lipgloss.Height(actionRow) - 1
	m.rightButtonsRelH = lipgloss.Height(actionRow)
	m.rightButtonsRowW = lipgloss.Width(actionRow)
	m.rightProbeBtnW = lipgloss.Width(probeBtn)
	m.rightSaveBtnW = lipgloss.Width(saveBtn)
	m.rightVimBtnW = lipgloss.Width(vimBtn)
	m.rightDeleteBtnW = lipgloss.Width(deleteBtn)

	// 4. Diagnostics Feedback Section
	diagHeader := renderFormSectionHeader("Diagnostics", contentWidth)
	lines = append(lines, diagHeader)
	relY += lipgloss.Height(diagHeader)

	if probe == nil {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("  Not tested yet · Click Probe to test connection"))
	} else if probe.Err != "" {
		failTitle := lipgloss.NewStyle().Foreground(colorError).Bold(true).Render(fmt.Sprintf("  ✕ %s failed", probe.Stage))
		errDetail := lipgloss.NewStyle().Foreground(colorTextSoft).Render(fmt.Sprintf("    %s", probe.Err))
		lines = append(lines, failTitle, errDetail)
		if len(status.Suggestions) > 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorWarning).Render(fmt.Sprintf("    Tip: %s", status.Suggestions[0])))
		}
	} else {
		okLine := lipgloss.NewStyle().Foreground(colorSuccess).Render(fmt.Sprintf("  ✓ Connection & initialize OK (%s)", probe.Latency.Round(time.Millisecond)))
		toolsLine := lipgloss.NewStyle().Foreground(colorSuccess).Render(fmt.Sprintf("  ✓ tools/list: %d tool(s) discovered", probe.ToolsCount))
		lines = append(lines, okLine, toolsLine)
		if len(probe.ToolNames) > 0 {
			toolList := strings.Join(probe.ToolNames[:min(4, len(probe.ToolNames))], ", ")
			if len(probe.ToolNames) > 4 {
				toolList += fmt.Sprintf(" (+%d more)", len(probe.ToolNames)-4)
			}
			lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("    "+toolList))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderActionBtn(idx int, label string) string {
	if m.focusArea == mcpFocusActions && m.actionIdx == idx {
		return pmCompactPrimaryBtnStyle.Copy().MarginRight(0).Render(label)
	}
	return pmCompactBtnStyle.Copy().MarginRight(0).Render(label)
}

func (m *mcpManagerModel) renderEmptyState() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("No MCP servers configured"),
		"",
		"Manage local and remote MCP endpoints directly from this workbench.",
		"",
		lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Quick Start:"),
		"  • Click [Add] to add a new server",
		"  • Click [Import] to import from JSON/YAML/TOML or existing config file",
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) overlayModal(bg string) string {
	var modalContent string
	modalWidth := 56
	switch m.modalKind {
	case mcpModalAdd:
		modalContent = m.renderAddModal()
		modalWidth = 52
	case mcpModalImport:
		modalContent = m.renderImportModal()
		modalWidth = max(56, min(90, m.width-20))
	case mcpModalTransfer:
		modalContent = m.renderTransferModal()
		modalWidth = 60
	case mcpModalDeleteConfirm:
		modalContent = m.renderDeleteModal()
		modalWidth = 50
	case mcpModalProbeDetail:
		modalContent = m.renderProbeDetailModal()
		modalWidth = 64
	}

	modalBox := pmModalStyle.Width(modalWidth).Background(colorPanelBg).Render(modalContent)
	modalW := lipgloss.Width(modalBox)
	modalH := lipgloss.Height(modalBox)
	x := max(0, (m.width-modalW)/2)
	y := max(0, (m.height-modalH)/2)

	m.modalX = x
	m.modalY = y
	m.modalW = modalW
	m.modalH = modalH
	m.modalOptionStartY = y + 4 // 1 border + 1 title + 1 empty + 1 desc

	if bg != "" {
		return overlayBox(bg, modalBox, x, y)
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modalBox)
}

func (m *mcpManagerModel) renderAddModal() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Create MCP Server")
	desc := lipgloss.NewStyle().Foreground(colorMuted).Render("Choose transport protocol:")

	transports := []string{"stdio  (local executable / command line)", "http   (remote HTTP endpoint)", "sse    (Server-Sent Events endpoint)"}
	options := make([]string, 0, len(transports))
	for i, t := range transports {
		prefix := "  "
		style := pmItemStyle
		if i == m.addTransport {
			prefix = "▶ "
			style = pmFocusedItemStyle
		}
		options = append(options, style.Render(prefix+t))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		desc,
		"",
		lipgloss.JoinVertical(lipgloss.Left, options...),
	)
}

func (m *mcpManagerModel) renderImportModal() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Import MCP Servers (JSON / YAML / TOML / File)")
	desc := lipgloss.NewStyle().Foreground(colorMuted).Render("Paste snippet or enter local file path (e.g. ~/.codex/config.toml):")
	contentWidth := max(36, m.width-40)

	editorText := renderCursorText(m.importBuffer, m.importCursor)
	inputBox := pmFocusedInputStyle.Copy().Width(contentWidth).Height(max(8, m.height-14)).Render(editorText)

	vimBtn := pmCompactPrimaryBtnStyle.Copy().MarginRight(0).Render("Open in Editor")
	saveBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("Import")
	cancelBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("Cancel")
	actionRow := lipgloss.JoinHorizontal(lipgloss.Top, vimBtn, "   ", saveBtn, "   ", cancelBtn)

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		desc,
		"",
		inputBox,
		"",
		actionRow,
	)
}

func (m *mcpManagerModel) renderTransferModal() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Transfer MCP Servers"),
		"",
		"Click to choose sync direction:",
		"",
	}
	for i, item := range m.transferItems {
		label := fmt.Sprintf("%-24s %s", item.Label, lipgloss.NewStyle().Foreground(colorDim).Render(item.Description))
		style := pmItemStyle.Copy().Width(max(24, m.width-48))
		prefix := "  "
		if i == m.transferIndex {
			style = pmFocusedItemStyle.Copy().Width(max(24, m.width-48))
			prefix = "▶ "
		}
		lines = append(lines, style.Render(prefix+label))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderDeleteModal() string {
	name := m.currentName()
	title := lipgloss.NewStyle().Bold(true).Foreground(colorError).Render("Delete MCP Server")
	desc := fmt.Sprintf("Are you sure you want to remove server %q?", name)
	deleteBtn := pmCompactPrimaryBtnStyle.Copy().MarginRight(0).Render("Confirm Delete")
	cancelBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("Cancel")
	actionRow := lipgloss.JoinHorizontal(lipgloss.Top, deleteBtn, "   ", cancelBtn)
	return lipgloss.JoinVertical(lipgloss.Left, title, "", desc, "", actionRow)
}

func (m *mcpManagerModel) renderProbeDetailModal() string {
	name := m.currentName()
	probe := m.probes[name]
	title := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Probe Diagnostics · " + name)
	if probe == nil {
		return lipgloss.JoinVertical(lipgloss.Left, title, "", "No probe data yet. Click outside or Esc to close.")
	}
	lines := []string{
		title,
		"",
	}
	if probe.Err != "" {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorError).Bold(true).Render(fmt.Sprintf("✕ %s failed", probe.Stage)),
			"",
			"Error: "+probe.Err,
			"",
		)
	} else {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("✓ Connection & Initialize OK"),
			lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render(fmt.Sprintf("✓ Discovered %d tools", probe.ToolsCount)),
			"",
		)
		for _, tool := range probe.ToolNames {
			lines = append(lines, "  • "+lipgloss.NewStyle().Foreground(colorTextSoft).Render(tool))
		}
		lines = append(lines, "")
	}
	closeBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("Close")
	lines = append(lines, closeBtn)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) contextHelpText() string {
	if m.modalKind != mcpModalNone {
		switch m.modalKind {
		case mcpModalImport:
			return "Click buttons or press Enter to import • Esc/Q Back"
		case mcpModalAdd:
			return "Click option to select • Esc/Q Back"
		case mcpModalDeleteConfirm:
			return "Click to confirm or cancel • Esc/Q Back"
		default:
			return "Click to confirm • Esc/Q Back"
		}
	}

	saveLabel := "Save"
	if m.dirty {
		saveLabel = lipgloss.NewStyle().Foreground(colorFocus).Bold(true).Render("Save *")
	} else {
		saveLabel = lipgloss.NewStyle().Foreground(colorDim).Render("Save")
	}

	switch m.focusArea {
	case mcpFocusFields:
		return fmt.Sprintf("Tab Next Field    •    Click to select / toggle options    •    %s    •    Esc/Q Back", saveLabel)
	case mcpFocusActions:
		return "Click action or press Enter    •    Tab Switch Section    •    Esc/Q Back"
	default:
		return fmt.Sprintf("Click to select server    •    Tab Switch Section    •    %s    •    Esc/Q Back", saveLabel)
	}
}

func renderStatusBadge(status mcpStatusSummary) string {
	switch status.Kind {
	case mcpStatusConfigured, mcpStatusReachable:
		return lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render(status.Badge)
	case mcpStatusBroken:
		return lipgloss.NewStyle().Foreground(colorError).Bold(true).Render(status.Badge)
	default:
		return lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render(status.Badge)
	}
}
