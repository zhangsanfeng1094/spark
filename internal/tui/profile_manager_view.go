package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

func (m *pmModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	header := dashboardHeaderStyle.Width(m.width - 6).Render("Spark Profiles")
	leftPanelW := 30
	rightPanelW := m.width - leftPanelW - 10
	if rightPanelW < 54 {
		rightPanelW = 54
	}
	inputW := rightPanelW - pmLabelWidth - 5
	if inputW < 24 {
		inputW = 24
	}
	m.inputWidth = inputW

	footerText := m.helpText()
	footer := pmStatusBarStyle.Width(m.width - 4).Render(
		lipgloss.NewStyle().Align(lipgloss.Right).Foreground(colorMuted).Render(footerText),
	)

	paneInnerH := max(
		lipgloss.Height(m.renderLeftPane(0)),
		lipgloss.Height(m.renderRightPane(0)),
	)
	availableOuterH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if availableOuterH > 2 {
		paneInnerH = availableOuterH - 2
	}

	leftPane := m.renderLeftPane(paneInnerH)
	rightPane := m.renderRightPane(paneInnerH)

	leftStyle := pmPanelStyle.Width(leftPanelW)
	rightStyle := pmPanelStyle.Width(rightPanelW)

	if m.focusArea == pmFocusProfiles {
		leftStyle = pmFocusedPanelStyle.Width(leftPanelW)
	} else if m.focusArea == pmFocusFields || m.focusArea == pmFocusActions {
		rightStyle = pmFocusedPanelStyle.Width(rightPanelW)
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
	if m.modalOpen {
		return fitToViewportHeight(m.overlayModal(ui), m.height)
	}
	return fitToViewportHeight(ui, m.height)
}

func (m *pmModel) renderLeftPane(height int) string {
	const listWidth = 26

	topLines := []string{
		lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Profiles"),
		"",
	}
	m.leftVisibleRows = nil
	m.leftVisibleIdxs = nil

	bottomLines := m.renderLeftPaneBottom(listWidth)
	visibleSlots := len(m.profileNames)
	if height > 0 {
		visibleSlots = max(0, height-lipgloss.Height(lipgloss.JoinVertical(lipgloss.Left, append(topLines, bottomLines...)...)))
	}

	start, end, showUp, showDown := profileWindow(len(m.profileNames), m.selected, visibleSlots)
	if showUp {
		topLines = append(topLines, lipgloss.NewStyle().Foreground(colorDim).Width(listWidth).Render("  ↑ more"))
	}
	for i := start; i < end; i++ {
		name := m.profileNames[i]
		prefix := "  "
		if i == m.selected {
			prefix = "> "
		}
		marker := ""
		if m.cfg.DefaultProfile == name {
			marker = " " + pmBadgeStyle.Render("★")
		}
		nameW := max(1, listWidth-lipgloss.Width(prefix)-lipgloss.Width(marker)-1)
		displayName := prefix + truncateDisplay(name, nameW) + marker

		if i == m.selected {
			if m.focusArea == pmFocusProfiles {
				topLines = append(topLines, pmFocusedItemStyle.Width(listWidth).Render(displayName))
			} else {
				topLines = append(topLines, pmSelectedMutedItemStyle.Width(listWidth).Render(displayName))
			}
		} else {
			topLines = append(topLines, pmItemStyle.Width(listWidth).Render(displayName))
		}
		m.leftVisibleRows = append(m.leftVisibleRows, len(topLines)-1)
		m.leftVisibleIdxs = append(m.leftVisibleIdxs, i)
	}
	if showDown {
		topLines = append(topLines, lipgloss.NewStyle().Foreground(colorDim).Width(listWidth).Render("  ↓ more"))
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

func (m *pmModel) renderLeftPaneBottom(width int) []string {
	btnStyle := pmLeftBtnStyle.Copy().MarginRight(0)
	activeBtnStyle := pmLeftActiveBtnStyle.Copy().MarginRight(0)

	addBtn := btnStyle.Render("F3 Add")
	copyBtn := btnStyle.Render("F4 Copy")
	defaultBtn := btnStyle.Render("F6 Default")
	delBtn := btnStyle.Render("F7 Del")
	if m.focusArea == pmFocusActions {
		if m.actionIndex == pmActAdd {
			addBtn = activeBtnStyle.Render("F3 Add")
		} else if m.actionIndex == pmActCopy {
			copyBtn = activeBtnStyle.Render("F4 Copy")
		} else if m.actionIndex == pmActDefault {
			defaultBtn = activeBtnStyle.Render("F6 Default")
		} else if m.actionIndex == pmActDel {
			delBtn = activeBtnStyle.Render("F7 Del")
		}
	}

	btnGap := "  "
	col1W := max(lipgloss.Width(addBtn), lipgloss.Width(defaultBtn))
	col2W := max(lipgloss.Width(copyBtn), lipgloss.Width(delBtn))
	btnRow1 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(col1W).Render(addBtn),
		btnGap,
		lipgloss.NewStyle().Width(col2W).Render(copyBtn),
	)
	btnRow2 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(col1W).Render(defaultBtn),
		btnGap,
		lipgloss.NewStyle().Width(col2W).Render(delBtn),
	)
	lines := m.leftSummaryLines(width)
	lines = append(lines, "", btnRow1, btnRow2)
	m.leftButtonsRelY = len(lines) - 2
	m.leftButtonsRow2Y = len(lines) - 1
	m.leftButtonsRelH = lipgloss.Height(btnRow1)
	m.leftButtonsRowW = lipgloss.Width(btnRow1)
	m.leftButtonsRow2W = lipgloss.Width(btnRow2)
	m.leftAddBtnW = lipgloss.Width(addBtn) + lipgloss.Width(btnGap)
	m.leftCopyBtnW = lipgloss.Width(copyBtn) + lipgloss.Width(btnGap)
	m.leftDefaultBtnW = lipgloss.Width(defaultBtn) + lipgloss.Width(btnGap)
	return lines
}

func (m *pmModel) renderRightPane(height int) string {
	var topLines []string
	relY := 0
	contentWidth := pmLabelWidth + 1 + m.inputWidth

	title := fmt.Sprintf("Config · %s", m.currentProfileName())
	if m.dirty {
		title += " *"
	}
	titleLine := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
	topLines = append(topLines, titleLine)
	relY += lipgloss.Height(titleLine)
	topLines = append(topLines, "")
	relY++
	m.fieldStartRelY = make([]int, len(m.fields))
	m.fieldEndRelY = make([]int, len(m.fields))

	for i, f := range m.fields {
		val := f.value
		if f.masked && val != "" {
			val = strings.Repeat("*", len(val))
		}

		focused := m.focusArea == pmFocusFields && i == m.focusField
		decoratedValue := m.decorateFieldValue(i, val)
		cursor := f.cursor
		if focused && !f.readOnly && f.cursor >= len([]rune(val)) {
			cursor = len([]rune(decoratedValue))
		}
		row := renderCompactFormRow(compactFormRowOptions{
			Label:      f.label,
			Value:      decoratedValue,
			Width:      m.inputWidth,
			Focused:    focused,
			ReadOnly:   f.readOnly,
			Required:   f.required,
			Cursor:     cursor,
			ShowCursor: focused && !f.readOnly,
		})
		rowH := lipgloss.Height(row)
		m.fieldStartRelY[i] = relY
		m.fieldEndRelY[i] = relY + rowH - 1
		topLines = append(topLines, row)
		relY += rowH
	}

	testBtn := pmCompactBtnStyle.Render("F8 Test")
	saveBtn := pmCompactBtnStyle.Render("F2 Save")
	if m.focusArea == pmFocusActions {
		if m.actionIndex == pmActTest {
			testBtn = pmCompactActiveBtnStyle.Render("F8 Test")
		} else if m.actionIndex == pmActSave {
			saveBtn = pmCompactActiveBtnStyle.Render("F2 Save")
		}
	}

	actionTitle := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Actions")
	btnRow := lipgloss.JoinHorizontal(
		lipgloss.Top,
		testBtn,
		lipgloss.PlaceHorizontal(
			max(2, contentWidth-lipgloss.Width(testBtn)),
			lipgloss.Right,
			saveBtn,
		),
	)
	var bottomLines []string
	if help := m.contextHintText(); help != "" {
		helpTitle := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Help")
		helpText := lipgloss.NewStyle().Foreground(colorMuted).Width(contentWidth).Render(help)
		bottomLines = append(bottomLines, helpTitle, helpText, "")
	}
	bottomLines = append(bottomLines, actionTitle, btnRow, "")
	statusTitle := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Status")
	statusSummary, statusSummaryStyle := m.renderStatusSummaryLine()
	bottomLines = append(bottomLines, statusTitle, statusSummaryStyle.Render(statusSummary))

	content := joinTopAndBottom(topLines, bottomLines, height)
	joinedTop := lipgloss.JoinVertical(lipgloss.Left, topLines...)
	finalLines := strings.Split(content, "\n")
	topHeight := lipgloss.Height(joinedTop)
	fillerHeight := len(finalLines) - topHeight - len(bottomLines)
	if fillerHeight < 0 {
		fillerHeight = 0
	}
	m.rightButtonsRelY = topHeight + fillerHeight
	m.rightButtonsRelH = lipgloss.Height(btnRow)
	m.rightButtonsRowW = lipgloss.Width(btnRow)
	m.rightTestBtnW = lipgloss.Width(testBtn)
	m.rightButtonsGapW = max(2, contentWidth-lipgloss.Width(testBtn)-lipgloss.Width(saveBtn))
	return content
}

func (m *pmModel) overlayModal(bg string) string {
	_ = bg
	var options []string
	switch m.modalKind {
	case pmModalKindOpenAIAPIType:
		options = append(options, "Select OpenAI API Types:")
		options = append(options, "")
		for i, opt := range m.visibleAPITypeOptions() {
			cursor := "   "
			style := pmItemStyle
			if i == m.modalCursor {
				cursor = " ➤ "
				style = pmSelectedItemStyle
			}
			check := "[ ]"
			if m.apiTypeSelected[opt] {
				check = "[x]"
			}
			options = append(options, style.Render(cursor+check+" "+opt))
		}
		options = append(options, "")
		options = append(options, "[Space] Toggle  [Enter] Confirm  [Esc] Cancel")
	case pmModalKindModels:
		modalInnerWidth := 70
		listWidth := 52
		panelRow := func(content string) string {
			return lipgloss.NewStyle().Background(colorPanelBg).Width(modalInnerWidth).Render(content)
		}
		options = append(options, panelRow(lipgloss.NewStyle().Bold(true).Render("Edit Models")))
		options = append(options, panelRow(""))
		searchLine := "Search: " + m.modelSearchQuery
		if m.modelSearchFocused && !m.modelEditMode {
			searchLine += "█"
		}
		options = append(options, panelRow(pmInputStyle.Copy().Width(listWidth).Render(searchLine)))
		options = append(options, panelRow(""))
		filtered := m.filteredModelIndices()
		listLines := make([]string, 0, pmModelsModalMaxVisible+2)
		if len(m.modelItems) == 0 {
			m.modelModalVisibleCount = 0
			m.modelModalScroll = 0
			listLines = append(listLines, centeredModalText("(empty)", listWidth, pmItemStyle))
		} else if len(filtered) == 0 {
			m.modelModalVisibleCount = 0
			m.modelModalScroll = 0
			listLines = append(listLines, centeredModalText("(no match)", listWidth, pmItemStyle))
		} else {
			visible := len(filtered)
			if visible > pmModelsModalMaxVisible {
				visible = pmModelsModalMaxVisible
			}
			m.modelModalVisibleCount = visible
			m.syncModelsModalScroll()
			if m.modelModalScroll > 0 {
				listLines = append(listLines, centeredModalText("↑ more", listWidth, lipgloss.NewStyle().Foreground(colorDim)))
			}
			start := m.modelModalScroll
			end := start + visible
			if end > len(filtered) {
				end = len(filtered)
			}
			for i := start; i < end; i++ {
				actualIdx := filtered[i]
				model := m.modelItems[actualIdx]
				cursor := "   "
				style := pmItemStyle
				if actualIdx == m.modalCursor {
					cursor = " ➤ "
					style = pmSelectedItemStyle
				}
				prefix := "  "
				if model == m.defaultModel {
					prefix = "★ "
				}
				listLines = append(listLines, centeredModalText(cursor+prefix+model, listWidth, style))
			}
			if end < len(filtered) {
				listLines = append(listLines, centeredModalText("↓ more", listWidth, lipgloss.NewStyle().Foreground(colorDim)))
			}
		}
		for len(listLines) < pmModelsModalMaxVisible+2 {
			listLines = append(listLines, "")
		}
		for _, line := range listLines {
			options = append(options, panelRow(line))
		}
		if m.modelEditMode {
			options = append(options, panelRow(""))
			options = append(options, panelRow(pmInputStyle.Copy().Width(listWidth).Render("Input: "+m.modelEditBuffer+"█")))
			for _, line := range renderModelModalHelpRows(true, false) {
				options = append(options, panelRow(line))
			}
		} else {
			options = append(options, panelRow(""))
			for _, line := range renderModelModalHelpRows(false, m.modelSearchFocused) {
				options = append(options, panelRow(line))
			}
		}
		if m.modelModalNote != "" {
			options = append(options, panelRow(""))
			options = append(options, panelRow(lipgloss.NewStyle().Foreground(colorDim).Render(m.modelModalNote)))
		}
	default:
		options = append(options, "Select Provider Type:")
		options = append(options, "")
		for i, opt := range m.providerOptions {
			prefix := "   "
			style := pmItemStyle
			if i == m.modalCursor {
				prefix = " ➤ "
				style = pmSelectedItemStyle
			}
			options = append(options, style.Render(prefix+opt.name))
		}
		options = append(options, "")
		options = append(options, "[Enter] Confirm  [Esc] Cancel")
	}

	modalContent := lipgloss.JoinVertical(lipgloss.Left, options...)
	modalWidth := 40
	if m.modalKind == pmModalKindOpenAIAPIType {
		modalWidth = 52
	} else if m.modalKind == pmModalKindModels {
		modalWidth = 78
	}
	modalBox := pmModalStyle.Width(modalWidth).Render(modalContent)
	m.modalW = lipgloss.Width(modalBox)
	m.modalH = lipgloss.Height(modalBox)
	m.modalX = (m.width - m.modalW) / 2
	m.modalY = (m.height - m.modalH) / 2

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		modalBox,
		lipgloss.WithWhitespaceChars(" "),
	)
}

func centeredModalText(text string, width int, style lipgloss.Style) string {
	pad := max(0, (width-lipgloss.Width(text))/2)
	return strings.Repeat(" ", pad) + style.Render(text)
}

func renderModelModalHelpRows(editing bool, searchFocused bool) []string {
	labelStyle := lipgloss.NewStyle().Foreground(colorLabel).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(colorMuted)
	row := func(label, text string) string {
		return lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render(fmt.Sprintf("%-8s", label)), textStyle.Render(text))
	}
	if editing {
		return []string{
			row("Input", "type model id"),
			row("Save", "Enter"),
			row("Cancel", "Esc"),
		}
	}
	navigation := "↑/↓ or wheel"
	if searchFocused {
		navigation += " · Tab pauses typing"
	} else {
		navigation += " · Tab focuses search"
	}
	return []string{
		row("Search", "type text · Backspace edit · Delete clear"),
		row("Move", navigation),
		row("Actions", "F5 Fetch · F6 Add · F7 Edit · F8 Delete"),
		row("Finish", "Enter Select · Esc Cancel"),
	}
}

func splitStatusText(status string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(status), "\n", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.TrimSpace(parts[1])
}

func styleStatusMain(s string) string {
	trimmed := strings.TrimSpace(strings.ToLower(s))
	switch {
	case strings.HasPrefix(trimmed, "error"), strings.HasPrefix(trimmed, "save failed"), strings.HasPrefix(trimmed, "✗"):
		return pmStatusErrStyle.Render(s)
	case strings.HasPrefix(trimmed, "configuration saved"), strings.HasPrefix(trimmed, "saved"), strings.HasPrefix(trimmed, "✓"):
		return pmStatusOkStyle.Render(s)
	case strings.HasPrefix(trimmed, "warning"), strings.HasPrefix(trimmed, "cannot"):
		return lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render(s)
	default:
		return pmStatusInfoStyle.Render(s)
	}
}

func (m *pmModel) helpText() string {
	switch m.focusArea {
	case pmFocusProfiles:
		return "Tab Focus · Enter Edit · F3 Add · F4 Copy · F6 Default · F7 Del"
	case pmFocusFields:
		return "Tab Focus · Enter Edit · F2 Save · Esc Back"
	case pmFocusActions:
		return "←/→ Pick Action · Enter Run · Esc Back"
	default:
		return "Tab Focus · Enter Activate"
	}
}

func (m *pmModel) decorateFieldValue(fieldIdx int, value string) string {
	switch fieldIdx {
	case pmFieldProviderType, pmFieldOpenAIAPIType:
		if value == "" {
			return "Select  ▼"
		}
		return value + "  ▼"
	case pmFieldModelsCSV:
		if value == "" {
			return "Choose models  >"
		}
		return value + "  >"
	default:
		return value
	}
}

func (m *pmModel) statusSummaryText() string {
	switch m.statusKind {
	case pmStatusTestSuccess, pmStatusTestError:
		if m.lastTestSummary != "" && m.lastTestOK {
			modelInfo := ""
			if len(m.modelsDraft) > 0 {
				modelInfo = fmt.Sprintf(" · %d models", len(m.modelsDraft))
			}
			return m.lastTestSummary + modelInfo
		}
		main, _ := splitStatusText(m.status)
		if summary := summarizeActivityStatus(main); summary != "" {
			return summary
		}
		if m.lastTestSummary != "" {
			return m.lastTestSummary
		}
	case pmStatusNeutral:
		main, _ := splitStatusText(m.status)
		if strings.EqualFold(strings.TrimSpace(main), "ready") || strings.HasPrefix(strings.TrimSpace(main), "Ready.") {
			return ""
		}
	}
	main, log := splitStatusText(m.status)
	if summary := summarizeActivityStatus(main); summary != "" {
		return summary
	}
	if log != "" {
		return main + " · " + log
	}
	return main
}

func (m *pmModel) leftSummaryLines(width int) []string {
	profile := m.cfg.Profiles[m.currentProfileName()]
	if profile == nil {
		return nil
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Current"),
		lipgloss.NewStyle().Foreground(colorText).Width(width).Render(truncateDisplay(m.currentProfileName(), width)),
	}
	if provider := strings.TrimSpace(m.fields[pmFieldProviderType].value); provider != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(truncateDisplay(provider, width)))
	}
	if summary := strings.TrimSpace(formatModelsSummary(m.modelsDraft, m.defaultModel)); summary != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(truncateDisplay(summary, width)))
	}
	return lines
}

func truncateDisplay(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}

	limit := width - 3
	out := strings.Builder{}
	used := 0
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 0 {
			break
		}
		part := value[:size]
		partW := lipgloss.Width(part)
		if used+partW > limit {
			break
		}
		out.WriteString(part)
		used += partW
		value = value[size:]
	}
	return out.String() + "..."
}

func (m *pmModel) renderStatusSummaryLine() (string, lipgloss.Style) {
	statusSummary := strings.TrimSpace(m.statusSummaryText())
	statusSummaryStyle := lipgloss.NewStyle().Foreground(colorMuted)
	switch m.statusKind {
	case pmStatusSuccess, pmStatusTestSuccess:
		statusSummaryStyle = statusSummaryStyle.Foreground(colorSuccess)
	case pmStatusError, pmStatusTestError:
		statusSummaryStyle = statusSummaryStyle.Foreground(colorError)
	case pmStatusWarning:
		statusSummaryStyle = statusSummaryStyle.Foreground(colorWarning)
	}
	if statusSummary == "" {
		statusSummary = "No recent activity"
		statusSummaryStyle = statusSummaryStyle.Foreground(colorTextSoft)
	}
	return statusSummary, statusSummaryStyle
}

func summarizeActivityStatus(main string) string {
	trimmed := strings.TrimSpace(main)
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(trimmed, "✗ Test failed · "):
		return "✗ " + strings.TrimPrefix(trimmed, "✗ Test failed · ")
	case strings.HasPrefix(trimmed, "Saved profile "):
		return "✓ Saved profile"
	case strings.HasPrefix(trimmed, "Saved "):
		return "✓ " + trimmed
	default:
		return trimmed
	}
}

func profileWindow(total, selected, slots int) (start, end int, showUp, showDown bool) {
	if total <= 0 {
		return 0, 0, false, false
	}
	if slots <= 0 || slots >= total {
		return 0, total, false, false
	}
	window := max(1, slots)
	start = selected - window/2
	if start < 0 {
		start = 0
	}
	end = start + window
	if end > total {
		end = total
		start = end - window
		if start < 0 {
			start = 0
		}
	}
	showUp = start > 0
	showDown = end < total
	if showUp && end-start > 1 {
		start++
	}
	if showDown && end-start > 1 {
		end--
	}
	if start > selected {
		start = selected
	}
	if end <= selected {
		end = selected + 1
	}
	if start < 0 {
		start = 0
	}
	if end > total {
		end = total
	}
	return start, end, start > 0, end < total
}

func joinTopAndBottom(topLines, bottomLines []string, height int) string {
	if len(topLines) == 0 && len(bottomLines) == 0 {
		return ""
	}
	if height <= 0 {
		return lipgloss.JoinVertical(lipgloss.Left, append(topLines, bottomLines...)...)
	}
	topBlock := lipgloss.JoinVertical(lipgloss.Left, topLines...)
	bottomBlock := lipgloss.JoinVertical(lipgloss.Left, bottomLines...)
	filler := height - lipgloss.Height(topBlock) - lipgloss.Height(bottomBlock)
	if filler < 0 {
		filler = 0
	}
	lines := append([]string{}, topLines...)
	lines = append(lines, blankLines(filler)...)
	lines = append(lines, bottomLines...)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func blankLines(count int) []string {
	if count <= 0 {
		return nil
	}
	lines := make([]string, count)
	return lines
}

func (m *pmModel) contextHintText() string {
	if m.focusArea != pmFocusFields || m.focusField < 0 || m.focusField >= len(m.fields) {
		return ""
	}
	switch m.focusField {
	case pmFieldProfileName:
		return "Rename this profile. Saving will also update integrations that reference the old name."
	case pmFieldProviderType:
		return "Choose a provider template for this profile."
	case pmFieldOpenAIBaseURL:
		return "Set the OpenAI-compatible endpoint Spark should talk to for this profile."
	case pmFieldOpenAIAPIKey:
		return "Stored API key used for test and launch operations."
	case pmFieldOpenAIAPIType:
		return "Choose which OpenAI-compatible API surface Spark should prefer for this provider."
	case pmFieldModelsCSV:
		return "Select available models and set which one should be used as the default."
	default:
		return ""
	}
}

func shortStatus(status string) string {
	main, _ := splitStatusText(status)
	return main
}

func fitToViewportHeight(s string, height int) string {
	if height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}
