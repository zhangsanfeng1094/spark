package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *pmModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	header := pmTitleStyle.Render("Spark Profiles")
	leftPanelW := 30
	rightPanelW := m.width - 34
	if rightPanelW < 54 {
		rightPanelW = 54
	}
	inputW := rightPanelW - pmLabelWidth - 8
	if inputW < pmInputWidth {
		inputW = pmInputWidth
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
		paneInnerH = max(paneInnerH, availableOuterH-2)
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
		displayName := "  " + name
		if i == m.selected {
			displayName = "> " + name
		}
		if m.cfg.DefaultProfile == name {
			displayName += " " + pmBadgeStyle.Render("default")
		}

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

	addBtn := btnStyle.Render("[A] Add")
	copyBtn := btnStyle.Render("[C] Copy")
	defaultBtn := btnStyle.Render("[F] Default")
	delBtn := btnStyle.Render("[D] Del")
	if m.focusArea == pmFocusActions {
		if m.actionIndex == pmActAdd {
			addBtn = activeBtnStyle.Render("[A] Add")
		} else if m.actionIndex == pmActCopy {
			copyBtn = activeBtnStyle.Render("[C] Copy")
		} else if m.actionIndex == pmActDefault {
			defaultBtn = activeBtnStyle.Render("[F] Default")
		} else if m.actionIndex == pmActDel {
			delBtn = activeBtnStyle.Render("[D] Del")
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

		displayVal := m.decorateFieldValue(i, val)
		if m.focusArea == pmFocusFields && i == m.focusField && !f.readOnly {
			if f.cursor >= len(val) {
				displayVal += "█"
			} else {
				r := []rune(val)
				displayVal = string(r[:f.cursor]) + "█" + string(r[f.cursor:])
			}
		}

		currentInputStyle := pmInputStyle.Copy().Width(m.inputWidth)
		if m.focusArea == pmFocusFields && i == m.focusField {
			currentInputStyle = pmFocusedInputStyle.Copy().Width(m.inputWidth)
		}
		if f.readOnly {
			currentInputStyle = pmInputStyle.Copy().Width(m.inputWidth).Foreground(colorTextSoft).BorderForeground(colorBorder)
			if m.focusArea == pmFocusFields && i == m.focusField {
				currentInputStyle = pmFocusedInputStyle.Copy().Width(m.inputWidth)
			}
		}
		labelStyle := pmLabelStyle
		if m.focusArea == pmFocusFields && i == m.focusField {
			labelStyle = pmFocusedLabelStyle
		}

		row := lipgloss.JoinHorizontal(lipgloss.Center,
			labelStyle.Render(f.label),
			currentInputStyle.Render(displayVal),
		)
		rowH := lipgloss.Height(row)
		m.fieldStartRelY[i] = relY
		m.fieldEndRelY[i] = relY + rowH - 1
		topLines = append(topLines, row)
		relY += rowH
	}

	testBtn := pmBtnStyle.Render("[T] Test Connection")
	saveBtn := pmPrimaryBtnStyle.Render("[Ctrl+S] Save")
	if m.focusArea == pmFocusActions {
		if m.actionIndex == pmActTest {
			testBtn = pmActiveBtnStyle.Render("[T] Test Connection")
		} else if m.actionIndex == pmActSave {
			saveBtn = pmActiveBtnStyle.Render("[Ctrl+S] Save")
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
	bottomLines := []string{actionTitle, btnRow, ""}
	statusTitle := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Status")
	statusSummary, statusSummaryStyle := m.renderStatusSummaryLine()
	bottomLines = append(bottomLines, statusTitle, statusSummaryStyle.Render(statusSummary))
	if help := m.contextHintText(); help != "" {
		helpTitle := lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Help")
		helpText := lipgloss.NewStyle().Foreground(colorMuted).Width(contentWidth).Render(help)
		bottomLines = append(bottomLines, "", helpTitle, helpText)
	}

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
		for i, opt := range m.apiTypeOptions {
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
		options = append(options, "Edit Models:")
		searchLine := "Search: " + m.modelSearchQuery
		if m.modelSearchFocused && !m.modelEditMode {
			searchLine += "█"
		}
		options = append(options, pmInputStyle.Copy().Width(48).Render(searchLine))
		options = append(options, "")
		filtered := m.filteredModelIndices()
		if len(m.modelItems) == 0 {
			m.modelModalVisibleCount = 0
			m.modelModalScroll = 0
			options = append(options, pmItemStyle.Render("  (empty)"))
		} else if len(filtered) == 0 {
			m.modelModalVisibleCount = 0
			m.modelModalScroll = 0
			options = append(options, pmItemStyle.Render("  (no match)"))
		} else {
			visible := len(filtered)
			if visible > pmModelsModalMaxVisible {
				visible = pmModelsModalMaxVisible
			}
			m.modelModalVisibleCount = visible
			m.syncModelsModalScroll()
			if m.modelModalScroll > 0 {
				options = append(options, lipgloss.NewStyle().Foreground(colorDim).Render("  ↑ more"))
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
				options = append(options, style.Render(cursor+prefix+model))
			}
			if end < len(filtered) {
				options = append(options, lipgloss.NewStyle().Foreground(colorDim).Render("  ↓ more"))
			}
		}
		if m.modelEditMode {
			options = append(options, "")
			options = append(options, "Input: "+m.modelEditBuffer+"█")
			options = append(options, "[Enter] Save Input  [Esc] Cancel Edit")
		} else {
			options = append(options, "")
			options = append(options, "[Tab] Toggle Search/Action  [Type] Search  [Wheel/↑/↓] Move  [Ctrl+G] Fetch  [Ctrl+N] Add  [Ctrl+R] Edit  [Ctrl+K] Delete  [Ctrl+T] Default  [Ctrl+L] Clear")
			options = append(options, "[Enter] Confirm  [Esc] Cancel")
		}
		if m.modelModalNote != "" {
			options = append(options, "")
			options = append(options, lipgloss.NewStyle().Foreground(colorDim).Render(m.modelModalNote))
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
		return "Tab Focus · Enter Edit · A Add · C Copy · F Default · D Del"
	case pmFocusFields:
		return "Tab Focus · Enter Edit · Ctrl+S Save · Esc Back"
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
	if m.lastTestSummary != "" {
		if m.lastTestOK {
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
		return m.lastTestSummary
	}
	main, log := splitStatusText(m.status)
	if strings.EqualFold(strings.TrimSpace(main), "ready") || strings.HasPrefix(strings.TrimSpace(main), "Ready.") {
		return ""
	}
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
		lipgloss.NewStyle().Foreground(colorText).Width(width).Render(m.currentProfileName()),
	}
	if provider := strings.TrimSpace(m.fields[pmFieldProviderType].value); provider != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(provider))
	}
	if summary := strings.TrimSpace(formatModelsSummary(m.modelsDraft, m.defaultModel)); summary != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(summary))
	}
	return lines
}

func (m *pmModel) renderStatusSummaryLine() (string, lipgloss.Style) {
	statusSummary := strings.TrimSpace(m.statusSummaryText())
	statusSummaryStyle := lipgloss.NewStyle().Foreground(colorMuted)
	if m.lastTestSummary != "" {
		if m.lastTestOK {
			statusSummaryStyle = statusSummaryStyle.Foreground(colorSuccess)
		} else {
			statusSummaryStyle = statusSummaryStyle.Foreground(colorError)
		}
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
	case strings.HasPrefix(trimmed, "Saved. Detected API type:"):
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
		return "Provider type is derived from the current base URL."
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
