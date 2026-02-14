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
	rightPane := m.renderRightPane()

	leftStyle := pmPanelStyle.Width(30)
	rightStyle := pmPanelStyle.Width(m.width - 36)

	if m.focusArea == pmFocusProfiles {
		leftStyle = pmFocusedPanelStyle.Width(30)
	} else if m.focusArea == pmFocusFields {
		rightStyle = pmFocusedPanelStyle.Width(m.width - 36)
	}

	leftRendered := leftStyle.Render(leftPane)
	rightRendered := rightStyle.Render(rightPane)
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftRendered, rightRendered)

	appMarginX := 1
	appMarginY := 1
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
	helpText := "Tab: Switch Area • ↑/↓: Move • Enter: Edit/Select • Ctrl+D: Set Default • Ctrl+S: Save"
	statusBar := pmStatusBarStyle.Width(m.width - 4).Render(
		lipgloss.JoinHorizontal(lipgloss.Center,
			lipgloss.NewStyle().Width(m.width/2).Render(statusText),
			lipgloss.NewStyle().Width(m.width/2-6).Align(lipgloss.Right).Foreground(colorDim).Render(helpText),
		),
	)

	ui := pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar))
	if m.modalOpen {
		return m.overlayModal(ui)
	}
	return ui
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
			lines = append(lines, pmSelectedItemStyle.Width(26).Render("➤ "+displayName))
		} else {
			lines = append(lines, pmItemStyle.Render("  "+displayName))
		}
	}
	lines = append(lines, "")

	addBtn := pmBtnStyle.Render("+ Add")
	delBtn := pmBtnStyle.Render("- Del")
	if m.focusArea == pmFocusActions {
		if m.actionIndex == pmActAdd {
			addBtn = pmActiveBtnStyle.Render("+ Add")
		} else if m.actionIndex == pmActDel {
			delBtn = pmActiveBtnStyle.Render("- Del")
		}
	}

	lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Left, addBtn, delBtn))
	m.leftButtonsRelY = len(lines) - 1
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
		if m.focusArea == pmFocusFields && i == m.focusField && !f.readOnly {
			if f.cursor >= len(val) {
				displayVal += "█"
			} else {
				r := []rune(val)
				displayVal = string(r[:f.cursor]) + "█" + string(r[f.cursor:])
			}
		}

		currentInputStyle := pmInputStyle
		if m.focusArea == pmFocusFields && i == m.focusField {
			currentInputStyle = pmFocusedInputStyle
		}
		if f.readOnly {
			currentInputStyle = pmInputStyle.Copy().Foreground(colorDim).BorderForeground(colorDim)
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
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *pmModel) overlayModal(bg string) string {
	_ = bg
	var options []string
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

	modalContent := lipgloss.JoinVertical(lipgloss.Left, options...)
	modalBox := pmModalStyle.Width(40).Render(modalContent)
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
