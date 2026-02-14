package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *pmModel) handleMainMouse(msg tea.MouseMsg) {
	x, y := msg.X, msg.Y
	leftX1, leftY1 := m.leftContentX, m.leftContentY
	rightX1, rightY1 := m.rightContentX, m.rightContentY

	profileIdx := y - (leftY1 + 2)
	if x >= leftX1 && x <= leftX1+27 && profileIdx >= 0 && profileIdx < len(m.profileNames) {
		m.focusArea = pmFocusProfiles
		m.switchProfile(profileIdx)
		return
	}

	if y == leftY1+m.leftButtonsRelY && x >= leftX1 && x <= leftX1+20 {
		m.focusArea = pmFocusActions
		if x <= leftX1+9 {
			m.actionIndex = pmActAdd
			m.modalIgnoreNextClick = true
			m.runAction(pmActAdd)
		} else {
			m.actionIndex = pmActDel
			m.runAction(pmActDel)
		}
		return
	}

	fieldY := y - rightY1
	inputStartX := rightX1 + pmLabelWidth + 1
	inputEndX := inputStartX + pmInputWidth - 1
	if x >= inputStartX && x <= inputEndX {
		for i := range m.fields {
			if i < len(m.fieldStartRelY) && fieldY >= m.fieldStartRelY[i] && fieldY <= m.fieldEndRelY[i] {
				m.focusArea = pmFocusFields
				m.focusField = i
				return
			}
		}
	}

	if y == rightY1+m.rightButtonsRelY && x >= rightX1 && x <= rightX1+22 {
		m.focusArea = pmFocusActions
		if x <= rightX1+9 {
			m.actionIndex = pmActTest
			m.runAction(pmActTest)
		} else {
			m.actionIndex = pmActSave
			m.runAction(pmActSave)
		}
	}
}

func (m *pmModel) handleModalMouse(msg tea.MouseMsg) {
	x, y := msg.X, msg.Y
	if x < m.modalX || x >= m.modalX+m.modalW || y < m.modalY || y >= m.modalY+m.modalH {
		m.modalOpen = false
		return
	}

	optionStartY := m.modalY + 4
	idx := y - optionStartY
	if idx >= 0 && idx < len(m.providerOptions) {
		m.modalCursor = idx
		m.createProfileFromModal()
	}
}

func (m *pmModel) handleMainKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return tea.Quit, true
	case "ctrl+s":
		m.save()
		return nil, true
	case "ctrl+d":
		m.cfg.DefaultProfile = m.currentProfileName()
		m.dirty = true
		m.status = "Set '" + m.cfg.DefaultProfile + "' as default. Save to persist."
		return nil, true
	case "tab":
		m.focusArea = (m.focusArea + 1) % 3
		return nil, true
	case "shift+tab":
		m.focusArea--
		if m.focusArea < 0 {
			m.focusArea = 2
		}
		return nil, true
	case "up", "k":
		m.moveUp()
		return nil, true
	case "down", "j":
		m.moveDown()
		return nil, true
	case "left", "h":
		if m.focusArea == pmFocusActions {
			if m.actionIndex > 0 {
				m.actionIndex--
			}
			return nil, true
		}
		return nil, false
	case "right", "l":
		if m.focusArea == pmFocusActions {
			if m.actionIndex < pmActSave {
				m.actionIndex++
			}
			return nil, true
		}
		return nil, false
	case "enter":
		if m.focusArea == pmFocusActions {
			return m.runAction(m.actionIndex), true
		} else if m.focusArea == pmFocusProfiles {
			m.focusArea = pmFocusFields
		}
		return nil, true
	}
	return nil, false
}

func (m *pmModel) handleFieldEdit(msg tea.KeyMsg) {
	if m.focusArea != pmFocusFields || m.focusField < 0 || m.focusField >= len(m.fields) {
		return
	}
	f := &m.fields[m.focusField]
	if f.readOnly {
		return
	}

	switch msg.String() {
	case "left":
		if f.cursor > 0 {
			f.cursor--
		}
	case "right":
		if f.cursor < len([]rune(f.value)) {
			f.cursor++
		}
	case "home":
		f.cursor = 0
	case "end":
		f.cursor = len([]rune(f.value))
	case "backspace":
		r := []rune(f.value)
		if f.cursor > 0 && f.cursor <= len(r) {
			f.value = string(append(r[:f.cursor-1], r[f.cursor:]...))
			f.cursor--
			m.dirty = true
		}
	case "delete":
		r := []rune(f.value)
		if f.cursor >= 0 && f.cursor < len(r) {
			f.value = string(append(r[:f.cursor], r[f.cursor+1:]...))
			m.dirty = true
		}
	default:
		if len(msg.Runes) > 0 {
			r := []rune(f.value)
			ins := msg.Runes
			before := append([]rune{}, r[:f.cursor]...)
			after := append([]rune{}, r[f.cursor:]...)
			next := append(before, ins...)
			next = append(next, after...)
			f.value = string(next)
			f.cursor += len(ins)
			m.dirty = true
		}
	}
}

func (m *pmModel) moveUp() {
	switch m.focusArea {
	case pmFocusProfiles:
		if m.selected > 0 {
			m.switchProfile(m.selected - 1)
		}
	case pmFocusFields:
		if m.focusField > 0 {
			m.focusField--
		}
	case pmFocusActions:
		if m.actionIndex > 0 {
			m.actionIndex--
		}
	}
}

func (m *pmModel) moveDown() {
	switch m.focusArea {
	case pmFocusProfiles:
		if m.selected < len(m.profileNames)-1 {
			m.switchProfile(m.selected + 1)
		}
	case pmFocusFields:
		if m.focusField < len(m.fields)-1 {
			m.focusField++
		}
	case pmFocusActions:
		if m.actionIndex < pmActSave {
			m.actionIndex++
		}
	}
}

func (m *pmModel) handleModalKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "esc", "q":
		m.modalOpen = false
	case "up", "k":
		if m.modalCursor > 0 {
			m.modalCursor--
		}
	case "down", "j":
		if m.modalCursor < len(m.providerOptions)-1 {
			m.modalCursor++
		}
	case "enter":
		m.createProfileFromModal()
	}
}
