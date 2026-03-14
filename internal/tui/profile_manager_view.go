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

	header := pmTitleStyle.Render("⚙ LLM Provider Manager")
	leftPane := m.renderLeftPane()
	rightPanelW := m.width - 36
	if rightPanelW < 54 {
		rightPanelW = 54
	}
	inputW := rightPanelW - pmLabelWidth - 22
	if inputW < pmInputWidth {
		inputW = pmInputWidth
	}
	m.inputWidth = inputW
	rightPane := m.renderRightPane()

	leftStyle := pmPanelStyle.Width(30)
	rightStyle := pmPanelStyle.Width(rightPanelW)

	if m.focusArea == pmFocusProfiles {
		leftStyle = pmFocusedPanelStyle.Width(30)
	} else if m.focusArea == pmFocusFields {
		rightStyle = pmFocusedPanelStyle.Width(rightPanelW)
	}

	leftRendered := leftStyle.Render(leftPane)
	rightRendered := rightStyle.Render(rightPane)
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftRendered, rightRendered)

	appMarginX := 1
	appMarginY := 0
	bodyX := appMarginX
	bodyY := appMarginY + lipgloss.Height(header)
	leftPanelW := lipgloss.Width(leftRendered)

	offsetX := pmBorderSize + pmPaddingH
	offsetY := pmBorderSize + pmPaddingV
	m.leftContentX = bodyX + offsetX
	m.leftContentY = bodyY + offsetY
	m.rightContentX = bodyX + leftPanelW + offsetX
	m.rightContentY = bodyY + offsetY

	statusText := m.status
	if m.dirty {
		statusText += "  ● Unsaved Changes"
	}
	statusMain, statusLog := splitStatusText(statusText)
	helpText := "Tab/Shift+Tab: Next/Prev • Enter: Activate • ↑/↓: Move • Ctrl+D: Set Default • Ctrl+S: Save"
	statusTopRow := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Width(m.width/2).Render(styleStatusMain(statusMain)),
		lipgloss.NewStyle().Width(m.width/2-6).Align(lipgloss.Right).Foreground(colorDim).Render(helpText),
	)
	statusLines := []string{statusTopRow}
	if statusLog != "" {
		statusLines = append(statusLines, pmStatusLogStyle.Render("↳ "+statusLog))
	}
	statusBar := pmStatusBarStyle.Width(m.width - 4).Render(
		lipgloss.JoinVertical(lipgloss.Left, statusLines...),
	)

	ui := pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar))
	if m.modalOpen {
		return fitToViewportHeight(m.overlayModal(ui), m.height)
	}
	return fitToViewportHeight(ui, m.height)
}

func (m *pmModel) renderLeftPane() string {
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Underline(true).Render("Profiles"))
	lines = append(lines, "")

	for i, name := range m.profileNames {
		displayName := name
		if m.cfg.DefaultProfile == name {
			displayName += " ★"
		}

		if i == m.selected {
			if m.focusArea == pmFocusProfiles {
				lines = append(lines, pmSelectedItemStyle.Width(26).Render("➤ "+displayName))
			} else {
				lines = append(lines, pmFocusedItemStyle.Width(26).Render("◆ "+displayName))
			}
		} else {
			lines = append(lines, pmItemStyle.Width(26).Render("  "+displayName))
		}
	}
	lines = append(lines, "")

	addBtn := pmLeftBtnStyle.Render("Add")
	copyBtn := pmLeftBtnStyle.Render("Copy")
	delBtn := pmLeftBtnStyle.Render("Del")
	if m.focusArea == pmFocusActions {
		if m.actionIndex == pmActAdd {
			addBtn = pmLeftActiveBtnStyle.Render("+ Add")
		} else if m.actionIndex == pmActCopy {
			copyBtn = pmLeftActiveBtnStyle.Render("> Copy")
		} else if m.actionIndex == pmActDel {
			delBtn = pmLeftActiveBtnStyle.Render("- Del")
		}
	}

	btnRow := lipgloss.JoinHorizontal(lipgloss.Left, addBtn, copyBtn, delBtn)
	lines = append(lines, btnRow)
	m.leftButtonsRelY = len(lines) - 1
	m.leftButtonsRelH = lipgloss.Height(btnRow)
	m.leftButtonsRowW = lipgloss.Width(btnRow)
	m.leftAddBtnW = lipgloss.Width(addBtn)
	m.leftCopyBtnW = lipgloss.Width(copyBtn)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *pmModel) renderRightPane() string {
	var lines []string
	relY := 0

	title := fmt.Sprintf("Config: %s", m.currentProfileName())
	titleLine := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
	lines = append(lines, titleLine)
	relY += lipgloss.Height(titleLine)
	lines = append(lines, "")
	relY++
	m.fieldStartRelY = make([]int, len(m.fields))
	m.fieldEndRelY = make([]int, len(m.fields))

	for i, f := range m.fields {
		val := f.value
		if f.masked && val != "" {
			val = strings.Repeat("*", len(val))
		}

		displayVal := val
		if (i == pmFieldOpenAIAPIType || i == pmFieldModelsCSV) && val != "" {
			displayVal += "  [Enter]"
		}
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
			currentInputStyle = pmInputStyle.Copy().Width(m.inputWidth).Foreground(colorDim).BorderForeground(colorDim)
			if m.focusArea == pmFocusFields && i == m.focusField {
				currentInputStyle = pmFocusedInputStyle.Copy().Width(m.inputWidth)
			}
		}

		row := lipgloss.JoinHorizontal(lipgloss.Center,
			pmLabelStyle.Render(f.label),
			currentInputStyle.Render(displayVal),
		)
		rowH := lipgloss.Height(row)
		m.fieldStartRelY[i] = relY
		m.fieldEndRelY[i] = relY + rowH - 1
		lines = append(lines, row)
		relY += rowH
	}
	lines = append(lines, "")
	relY++

	testBtn := pmBtnStyle.Render("Test")
	saveBtn := pmBtnStyle.Render("Save")
	if m.focusArea == pmFocusActions {
		if m.actionIndex == pmActTest {
			testBtn = pmActiveBtnStyle.Render("Test")
		} else if m.actionIndex == pmActSave {
			saveBtn = pmActiveBtnStyle.Render("Save")
		}
	}

	btnRow := lipgloss.JoinHorizontal(lipgloss.Left, testBtn, saveBtn)
	lines = append(lines, btnRow)
	m.rightButtonsRelY = relY
	m.rightButtonsRelH = lipgloss.Height(btnRow)
	m.rightButtonsRowW = lipgloss.Width(btnRow)
	m.rightTestBtnW = lipgloss.Width(testBtn)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
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
		options = append(options, "")
		if len(m.modelItems) == 0 {
			options = append(options, pmItemStyle.Render("  (empty)"))
		} else {
			for i, model := range m.modelItems {
				cursor := "   "
				style := pmItemStyle
				if i == m.modalCursor {
					cursor = " ➤ "
					style = pmSelectedItemStyle
				}
				prefix := "  "
				if model == m.defaultModel {
					prefix = "★ "
				}
				options = append(options, style.Render(cursor+prefix+model))
			}
		}
		if m.modelEditMode {
			options = append(options, "")
			options = append(options, "Input: "+m.modelEditBuffer+"█")
			options = append(options, "[Enter] Save Input  [Esc] Cancel Edit")
		} else {
			options = append(options, "")
			options = append(options, "[F] Fetch API  [A] Add  [E] Edit  [D] Delete  [S] Set Default")
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
	switch {
	case strings.HasPrefix(s, "✓"):
		return pmStatusOkStyle.Render(s)
	case strings.HasPrefix(s, "✗"):
		return pmStatusErrStyle.Render(s)
	default:
		return pmStatusInfoStyle.Render(s)
	}
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
