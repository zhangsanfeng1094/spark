package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *pmModel) handleMainMouse(msg tea.MouseMsg) tea.Cmd {
	x, y := msg.X, msg.Y
	leftX1, leftY1 := m.leftContentX, m.leftContentY
	rightX1, rightY1 := m.rightContentX, m.rightContentY

	profileIdx := y - (leftY1 + 2)
	if x >= leftX1 && x <= leftX1+27 && profileIdx >= 0 && profileIdx < len(m.profileNames) {
		m.focusArea = pmFocusProfiles
		m.switchProfile(profileIdx)
		return nil
	}

	leftBtnH := m.leftButtonsRelH
	if leftBtnH <= 0 {
		leftBtnH = 1
	}
	leftRowW := m.leftButtonsRowW
	if leftRowW <= 0 {
		leftRowW = 21
	}
	leftAddW := m.leftAddBtnW
	if leftAddW <= 0 {
		leftAddW = 10
	}
	leftBtnsY1 := leftY1 + m.leftButtonsRelY
	leftBtnsY2 := leftBtnsY1 + leftBtnH - 1
	// Restrict clicks to inside button borders (exclude top/bottom border rows).
	if leftBtnH >= 3 {
		leftBtnsY1++
		leftBtnsY2--
	}
	if leftBtnsY2 >= leftBtnsY1 {
		m.focusArea = pmFocusActions
		addHitW := leftAddW
		if addHitW > 0 {
			addHitW-- // strip right margin from styled button
		}
		delTotalW := leftRowW - leftAddW
		delHitW := delTotalW
		if delHitW > 0 {
			delHitW-- // strip trailing row margin on the right button
		}
		addX1 := leftX1
		addX2 := addX1 + addHitW - 1
		delX1 := leftX1 + leftAddW
		delX2 := delX1 + delHitW - 1
		if y >= leftBtnsY1 && y <= leftBtnsY2 && x >= addX1 && x <= addX2 {
			m.actionIndex = pmActAdd
			m.modalIgnoreNextClick = true
			return m.runAction(pmActAdd)
		}
		if y >= leftBtnsY1 && y <= leftBtnsY2 && x >= delX1 && x <= delX2 {
			m.actionIndex = pmActDel
			return m.runAction(pmActDel)
		}
	}

	fieldY := y - rightY1
	inputStartX := rightX1 + pmLabelWidth + 1
	inputWidth := m.inputWidth
	if inputWidth <= 0 {
		inputWidth = pmInputWidth
	}
	// input content width + horizontal padding (2) + borders (2)
	inputEndX := inputStartX + inputWidth + 3
	if x >= inputStartX && x <= inputEndX {
		for i := range m.fields {
			if i < len(m.fieldStartRelY) && fieldY >= m.fieldStartRelY[i] && fieldY <= m.fieldEndRelY[i] {
				m.focusArea = pmFocusFields
				m.focusField = i
				if i == pmFieldOpenAIAPIType {
					m.openAPITypeModal()
				} else if i == pmFieldModelsCSV {
					m.openModelsModal()
				}
				return nil
			}
		}
	}

	rightBtnH := m.rightButtonsRelH
	if rightBtnH <= 0 {
		rightBtnH = 1
	}
	rightRowW := m.rightButtonsRowW
	if rightRowW <= 0 {
		rightRowW = 23
	}
	rightTestW := m.rightTestBtnW
	if rightTestW <= 0 {
		rightTestW = 10
	}
	rightBtnsY1 := rightY1 + m.rightButtonsRelY
	rightBtnsY2 := rightBtnsY1 + rightBtnH - 1
	// Restrict clicks to inside button borders (exclude top/bottom border rows).
	if rightBtnH >= 3 {
		rightBtnsY1++
		rightBtnsY2--
	}
	if rightBtnsY2 >= rightBtnsY1 {
		m.focusArea = pmFocusActions
		testHitW := rightTestW
		if testHitW > 0 {
			testHitW-- // strip right margin from styled button
		}
		saveTotalW := rightRowW - rightTestW
		saveHitW := saveTotalW
		if saveHitW > 0 {
			saveHitW-- // strip trailing row margin on the right button
		}
		testX1 := rightX1
		testX2 := testX1 + testHitW - 1
		saveX1 := rightX1 + rightTestW
		saveX2 := saveX1 + saveHitW - 1
		if y >= rightBtnsY1 && y <= rightBtnsY2 && x >= testX1 && x <= testX2 {
			m.actionIndex = pmActTest
			return m.runAction(pmActTest)
		}
		if y >= rightBtnsY1 && y <= rightBtnsY2 && x >= saveX1 && x <= saveX2 {
			m.actionIndex = pmActSave
			return m.runAction(pmActSave)
		}
	}
	return nil
}

func (m *pmModel) handleModalMouse(msg tea.MouseMsg) {
	x, y := msg.X, msg.Y
	if x < m.modalX || x >= m.modalX+m.modalW || y < m.modalY || y >= m.modalY+m.modalH {
		m.modalOpen = false
		m.modalKind = pmModalKindNone
		return
	}

	optionStartY := m.modalY + 4
	idx := y - optionStartY
	switch m.modalKind {
	case pmModalKindAddProfile:
		if idx >= 0 && idx < len(m.providerOptions) {
			m.modalCursor = idx
			m.createProfileFromModal()
		}
	case pmModalKindOpenAIAPIType:
		if idx >= 0 && idx < len(m.apiTypeOptions) {
			m.modalCursor = idx
			m.toggleAPITypeOptionAtCursor()
		}
	case pmModalKindModels:
		if idx >= 0 && idx < len(m.modelItems) {
			m.modalCursor = idx
		}
	}
}

func (m *pmModel) openFieldModalIfNeeded() bool {
	if m.focusArea != pmFocusFields {
		return false
	}
	if m.focusField == pmFieldOpenAIAPIType {
		m.openAPITypeModal()
		return true
	}
	if m.focusField == pmFieldModelsCSV {
		m.openModelsModal()
		return true
	}
	return false
}

func (m *pmModel) handleFieldShortcut(msg tea.KeyMsg) bool {
	if m.focusArea != pmFocusFields {
		return false
	}
	if m.focusField != pmFieldOpenAIAPIType && m.focusField != pmFieldModelsCSV {
		return false
	}
	switch msg.String() {
	case " ", "space":
		if m.focusField == pmFieldOpenAIAPIType {
			m.openAPITypeModal()
		} else {
			m.openModelsModal()
		}
		return true
	}
	return false
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
		m.focusNextByTab()
		return nil, true
	case "shift+tab":
		m.focusPrevByTab()
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
		}
		if m.focusArea == pmFocusProfiles {
			m.focusArea = pmFocusFields
			return nil, true
		}
		if m.openFieldModalIfNeeded() {
			return nil, true
		}
		return nil, true
	}
	if m.handleFieldShortcut(msg) {
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

func (m *pmModel) handleModalKey(msg tea.KeyMsg) tea.Cmd {
	if m.modalKind == pmModalKindModels && m.modelEditMode {
		return m.handleModelsEditKey(msg)
	}
	switch msg.String() {
	case "esc", "q":
		m.modalOpen = false
		m.modalKind = pmModalKindNone
		return nil
	case "up", "k":
		if m.modalCursor > 0 {
			m.modalCursor--
		}
		return nil
	case "down", "j":
		maxItems := len(m.providerOptions)
		if m.modalKind == pmModalKindOpenAIAPIType {
			maxItems = len(m.apiTypeOptions)
		} else if m.modalKind == pmModalKindModels {
			maxItems = len(m.modelItems)
		}
		if m.modalCursor < maxItems-1 {
			m.modalCursor++
		}
		return nil
	case "tab":
		maxItems := m.modalItemCount()
		if maxItems > 0 {
			m.modalCursor = (m.modalCursor + 1) % maxItems
		}
		return nil
	case "shift+tab":
		maxItems := m.modalItemCount()
		if maxItems > 0 {
			m.modalCursor--
			if m.modalCursor < 0 {
				m.modalCursor = maxItems - 1
			}
		}
		return nil
	case " ", "space":
		if m.modalKind == pmModalKindOpenAIAPIType {
			m.toggleAPITypeOptionAtCursor()
		}
		return nil
	case "f":
		if m.modalKind == pmModalKindModels {
			return m.fetchModelsFromAPI()
		}
		return nil
	case "a":
		if m.modalKind == pmModalKindModels {
			m.startModelAdd()
		}
		return nil
	case "e":
		if m.modalKind == pmModalKindModels {
			m.startModelEdit()
		}
		return nil
	case "d":
		if m.modalKind == pmModalKindModels {
			m.deleteModelAtCursor()
		}
		return nil
	case "s":
		if m.modalKind == pmModalKindModels {
			m.setDefaultModelAtCursor()
		}
		return nil
	case "enter":
		switch m.modalKind {
		case pmModalKindAddProfile:
			m.createProfileFromModal()
		case pmModalKindOpenAIAPIType:
			m.confirmAPITypeSelection()
		case pmModalKindModels:
			m.confirmModelsSelection()
		}
		return nil
	}
	return nil
}

func (m *pmModel) handleModelsEditKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.modelEditMode = false
		m.modelEditIndex = -1
		m.modelEditBuffer = ""
		m.modelModalNote = "Edit canceled."
	case "enter":
		m.confirmModelEdit()
	case "left":
		// no cursor support in modal input currently
	case "right":
		// no cursor support in modal input currently
	case "backspace":
		r := []rune(m.modelEditBuffer)
		if len(r) > 0 {
			m.modelEditBuffer = string(r[:len(r)-1])
		}
	case "delete":
		m.modelEditBuffer = ""
	default:
		if len(msg.Runes) > 0 {
			m.modelEditBuffer += string(msg.Runes)
		}
	}
	return nil
}

func (m *pmModel) focusNextByTab() {
	switch m.focusArea {
	case pmFocusProfiles:
		m.focusArea = pmFocusFields
		m.focusField = 0
	case pmFocusFields:
		if m.focusField < len(m.fields)-1 {
			m.focusField++
			return
		}
		m.focusArea = pmFocusActions
		m.actionIndex = pmActAdd
	case pmFocusActions:
		if m.actionIndex < pmActSave {
			m.actionIndex++
			return
		}
		m.focusArea = pmFocusProfiles
	}
}

func (m *pmModel) focusPrevByTab() {
	switch m.focusArea {
	case pmFocusProfiles:
		m.focusArea = pmFocusActions
		m.actionIndex = pmActSave
	case pmFocusFields:
		if m.focusField > 0 {
			m.focusField--
			return
		}
		m.focusArea = pmFocusProfiles
	case pmFocusActions:
		if m.actionIndex > pmActAdd {
			m.actionIndex--
			return
		}
		m.focusArea = pmFocusFields
		if len(m.fields) > 0 {
			m.focusField = len(m.fields) - 1
		}
	}
}

func (m *pmModel) modalItemCount() int {
	switch m.modalKind {
	case pmModalKindOpenAIAPIType:
		return len(m.apiTypeOptions)
	case pmModalKindModels:
		return len(m.modelItems)
	default:
		return len(m.providerOptions)
	}
}
