package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *pmModel) filteredModelIndices() []int {
	if len(m.modelItems) == 0 {
		return nil
	}
	q := strings.TrimSpace(strings.ToLower(m.modelSearchQuery))
	if q == "" {
		idxs := make([]int, len(m.modelItems))
		for i := range m.modelItems {
			idxs[i] = i
		}
		return idxs
	}
	idxs := make([]int, 0, len(m.modelItems))
	for i, model := range m.modelItems {
		if strings.Contains(strings.ToLower(model), q) {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

func (m *pmModel) modelsModalVisibleCount() int {
	count := len(m.filteredModelIndices())
	if count == 0 {
		return 0
	}
	if m.modelModalVisibleCount > 0 && m.modelModalVisibleCount <= count {
		return m.modelModalVisibleCount
	}
	if count < pmModelsModalMaxVisible {
		return count
	}
	return pmModelsModalMaxVisible
}

func (m *pmModel) ensureModalCursorInFiltered() {
	if m.modalKind != pmModalKindModels {
		return
	}
	filtered := m.filteredModelIndices()
	if len(filtered) == 0 {
		m.modalCursor = 0
		return
	}
	if m.modalCursor >= 0 && m.modalCursor < len(m.modelItems) {
		for _, idx := range filtered {
			if idx == m.modalCursor {
				return
			}
		}
	}
	m.modalCursor = filtered[0]
}

func (m *pmModel) syncModelsModalScroll() {
	if m.modalKind != pmModalKindModels {
		return
	}
	filtered := m.filteredModelIndices()
	if len(filtered) == 0 {
		m.modalCursor = 0
		m.modelModalScroll = 0
		m.modelModalVisibleCount = 0
		return
	}
	m.ensureModalCursorInFiltered()
	visible := m.modelsModalVisibleCount()
	if visible <= 0 {
		visible = 1
	}
	cursorPos := 0
	for i, idx := range filtered {
		if idx == m.modalCursor {
			cursorPos = i
			break
		}
	}
	maxScroll := len(filtered) - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.modelModalScroll > maxScroll {
		m.modelModalScroll = maxScroll
	}
	if m.modelModalScroll < 0 {
		m.modelModalScroll = 0
	}
	if cursorPos < m.modelModalScroll {
		m.modelModalScroll = cursorPos
	}
	if cursorPos >= m.modelModalScroll+visible {
		m.modelModalScroll = cursorPos - visible + 1
	}
	if m.modelModalScroll < 0 {
		m.modelModalScroll = 0
	}
	if m.modelModalScroll > maxScroll {
		m.modelModalScroll = maxScroll
	}
}

func (m *pmModel) handleModelSearchKey(msg tea.KeyMsg) bool {
	if m.modalKind != pmModalKindModels || m.modelEditMode || !m.modelSearchFocused {
		return false
	}
	switch msg.String() {
	case "backspace":
		r := []rune(m.modelSearchQuery)
		if len(r) == 0 {
			return false
		}
		m.modelSearchQuery = string(r[:len(r)-1])
		m.syncModelsModalScroll()
		return true
	case "delete":
		if m.modelSearchQuery == "" {
			return false
		}
		m.modelSearchQuery = ""
		m.syncModelsModalScroll()
		return true
	}
	if len(msg.Runes) > 0 {
		for _, r := range msg.Runes {
			if r < 32 {
				continue
			}
			m.modelSearchQuery += string(r)
		}
		m.syncModelsModalScroll()
		return true
	}
	return false
}

func (m *pmModel) handleModalWheel(msg tea.MouseMsg) bool {
	if m.modalKind != pmModalKindModels || m.modelEditMode {
		return false
	}
	x, y := msg.X, msg.Y
	if x < m.modalX || x >= m.modalX+m.modalW || y < m.modalY || y >= m.modalY+m.modalH {
		return false
	}
	filtered := m.filteredModelIndices()
	if len(filtered) == 0 {
		return false
	}
	cursorPos := 0
	for i, idx := range filtered {
		if idx == m.modalCursor {
			cursorPos = i
			break
		}
	}
	switch msg.Type {
	case tea.MouseWheelUp:
		if cursorPos > 0 {
			m.modalCursor = filtered[cursorPos-1]
			m.syncModelsModalScroll()
		}
		return true
	case tea.MouseWheelDown:
		if cursorPos < len(filtered)-1 {
			m.modalCursor = filtered[cursorPos+1]
			m.syncModelsModalScroll()
		}
		return true
	default:
		return false
	}
}

func (m *pmModel) handleMainMouse(msg tea.MouseMsg) tea.Cmd {
	x, y := msg.X, msg.Y
	leftX1, leftY1 := m.leftContentX, m.leftContentY
	rightX1, rightY1 := m.rightContentX, m.rightContentY

	if x >= leftX1 && x <= leftX1+27 {
		for i, row := range m.leftVisibleRows {
			if y == leftY1+row && i < len(m.leftVisibleIdxs) {
				m.focusArea = pmFocusProfiles
				m.switchProfile(m.leftVisibleIdxs[i])
				return nil
			}
		}
	}

	leftBtnH := m.leftButtonsRelH
	if leftBtnH <= 0 {
		leftBtnH = 1
	}
	leftAddW := m.leftAddBtnW
	if leftAddW <= 0 {
		leftAddW = 10
	}
	leftCopyW := m.leftCopyBtnW
	if leftCopyW <= 0 {
		leftCopyW = 11
	}
	leftDefaultW := m.leftDefaultBtnW
	if leftDefaultW <= 0 {
		leftDefaultW = 14
	}
	leftBtnsY1 := leftY1 + m.leftButtonsRelY
	leftBtnsY2 := leftBtnsY1 + leftBtnH - 1
	leftBtns2Y1 := leftY1 + m.leftButtonsRow2Y
	leftBtns2Y2 := leftBtns2Y1 + leftBtnH - 1
	if leftBtnH >= 3 {
		leftBtnsY1++
		leftBtnsY2--
		leftBtns2Y1++
		leftBtns2Y2--
	}
	if (leftBtnsY2 >= leftBtnsY1 && y >= leftBtnsY1 && y <= leftBtnsY2) || (leftBtns2Y2 >= leftBtns2Y1 && y >= leftBtns2Y1 && y <= leftBtns2Y2) {
		m.focusArea = pmFocusActions
		if y >= leftBtnsY1 && y <= leftBtnsY2 {
			addHitW := max(1, leftAddW-2)
			copyHitW := max(1, leftCopyW-2)
			addX1 := leftX1
			addX2 := addX1 + addHitW - 1
			copyX1 := leftX1 + leftAddW
			copyX2 := copyX1 + copyHitW - 1
			if x >= addX1 && x <= addX2 {
				m.actionIndex = pmActAdd
				m.modalIgnoreNextClick = true
				return m.runAction(pmActAdd)
			}
			if x >= copyX1 && x <= copyX2 {
				m.actionIndex = pmActCopy
				return m.runAction(pmActCopy)
			}
		}
		if y >= leftBtns2Y1 && y <= leftBtns2Y2 {
			defaultHitW := max(1, leftDefaultW-2)
			delHitW := max(1, m.leftButtonsRow2W-leftDefaultW-2)
			defaultX1 := leftX1
			defaultX2 := defaultX1 + defaultHitW - 1
			delX1 := leftX1 + leftDefaultW
			delX2 := delX1 + delHitW - 1
			if x >= defaultX1 && x <= defaultX2 {
				m.actionIndex = pmActDefault
				return m.runAction(pmActDefault)
			}
			if x >= delX1 && x <= delX2 {
				m.actionIndex = pmActDel
				return m.runAction(pmActDel)
			}
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
				m.openFieldModalIfNeeded()
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
	rightGapW := m.rightButtonsGapW
	if rightGapW < 0 {
		rightGapW = 0
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
		saveTotalW := rightRowW - rightTestW - rightGapW
		saveHitW := saveTotalW
		if saveHitW > 0 {
			saveHitW-- // strip trailing row margin on the right button
		}
		testX1 := rightX1
		testX2 := testX1 + testHitW - 1
		saveX1 := rightX1 + rightTestW + rightGapW
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
	if m.modalKind == pmModalKindModels {
		optionStartY++ // account for always-visible search input row
	}
	idx := y - optionStartY
	switch m.modalKind {
	case pmModalKindAddProfile:
		if idx >= 0 && idx < len(m.providerOptions) {
			m.modalCursor = idx
			m.createProfileFromModal()
		}
	case pmModalKindProviderType:
		if idx >= 0 && idx < len(m.providerOptions) {
			m.modalCursor = idx
			m.confirmProviderTypeSelection()
		}
	case pmModalKindOpenAIAPIType:
		if idx >= 0 && idx < len(m.apiTypeOptions) {
			m.modalCursor = idx
			m.toggleAPITypeOptionAtCursor()
		}
	case pmModalKindModels:
		filtered := m.filteredModelIndices()
		visible := m.modelsModalVisibleCount()
		if len(filtered) == 0 || visible <= 0 {
			return
		}
		row := idx
		if m.modelModalScroll > 0 {
			if row == 0 {
				return
			}
			row--
		}
		if row < 0 || row >= visible {
			return
		}
		actualPos := m.modelModalScroll + row
		if actualPos >= 0 && actualPos < len(filtered) {
			m.modalCursor = filtered[actualPos]
			m.syncModelsModalScroll()
		}
	}
}

func (m *pmModel) openFieldModalIfNeeded() bool {
	if m.focusArea != pmFocusFields {
		return false
	}
	if m.focusField == pmFieldProviderType {
		m.openProviderTypeModal()
		return true
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
	if m.focusField != pmFieldProviderType && m.focusField != pmFieldOpenAIAPIType && m.focusField != pmFieldModelsCSV {
		return false
	}
	switch msg.String() {
	case " ", "space":
		if m.focusField == pmFieldProviderType {
			m.openProviderTypeModal()
		} else if m.focusField == pmFieldOpenAIAPIType {
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
	case "ctrl+c":
		return tea.Quit, true
	case "esc":
		switch m.focusArea {
		case pmFocusFields, pmFocusActions:
			m.focusArea = pmFocusProfiles
			return nil, true
		default:
			return tea.Quit, true
		}
	case "f2":
		m.save()
		return nil, true
	case "f3":
		m.focusArea = pmFocusActions
		m.actionIndex = pmActAdd
		return m.runAction(pmActAdd), true
	case "f4":
		m.focusArea = pmFocusActions
		m.actionIndex = pmActCopy
		return m.runAction(pmActCopy), true
	case "f6":
		m.focusArea = pmFocusActions
		m.actionIndex = pmActDefault
		return m.runAction(pmActDefault), true
	case "f7":
		m.focusArea = pmFocusActions
		m.actionIndex = pmActDel
		return m.runAction(pmActDel), true
	case "f8":
		m.focusArea = pmFocusActions
		m.actionIndex = pmActTest
		return m.runAction(pmActTest), true
	case "tab":
		m.focusNextByTab()
		return nil, true
	case "shift+tab":
		m.focusPrevByTab()
		return nil, true
	case "up":
		m.moveUp()
		return nil, true
	case "k":
		if m.focusArea == pmFocusFields {
			return nil, false
		}
		m.moveUp()
		return nil, true
	case "down":
		m.moveDown()
		return nil, true
	case "j":
		if m.focusArea == pmFocusFields {
			return nil, false
		}
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
	if m.modalKind == pmModalKindModels {
		switch msg.String() {
		case "tab", "shift+tab":
			m.modelSearchFocused = !m.modelSearchFocused
			return nil
		case "f5":
			return m.fetchModelsFromAPI()
		case "f6":
			m.startModelAdd()
			return nil
		case "f7":
			m.startModelEdit()
			return nil
		case "f8":
			m.deleteModelAtCursor()
			return nil
		}
		if m.modelSearchFocused {
			if m.handleModelSearchKey(msg) {
				return nil
			}
		}
	}
	switch msg.String() {
	case "esc", "q":
		m.modalOpen = false
		m.modalKind = pmModalKindNone
		return nil
	case "up", "k":
		if m.modalKind == pmModalKindModels {
			filtered := m.filteredModelIndices()
			for i, idx := range filtered {
				if idx == m.modalCursor {
					if i > 0 {
						m.modalCursor = filtered[i-1]
						m.syncModelsModalScroll()
					}
					break
				}
			}
			return nil
		}
		if m.modalCursor > 0 {
			m.modalCursor--
		}
		return nil
	case "down", "j":
		if m.modalKind == pmModalKindModels {
			filtered := m.filteredModelIndices()
			for i, idx := range filtered {
				if idx == m.modalCursor {
					if i < len(filtered)-1 {
						m.modalCursor = filtered[i+1]
						m.syncModelsModalScroll()
					}
					break
				}
			}
			return nil
		}
		maxItems := len(m.providerOptions)
		if m.modalKind == pmModalKindOpenAIAPIType {
			maxItems = len(m.apiTypeOptions)
		}
		if m.modalCursor < maxItems-1 {
			m.modalCursor++
		}
		return nil
	case "tab":
		if m.modalKind == pmModalKindModels {
			filtered := m.filteredModelIndices()
			if len(filtered) > 0 {
				pos := 0
				for i, idx := range filtered {
					if idx == m.modalCursor {
						pos = i
						break
					}
				}
				pos = (pos + 1) % len(filtered)
				m.modalCursor = filtered[pos]
				m.syncModelsModalScroll()
			}
			return nil
		}
		maxItems := m.modalItemCount()
		if maxItems > 0 {
			m.modalCursor = (m.modalCursor + 1) % maxItems
		}
		return nil
	case "shift+tab":
		if m.modalKind == pmModalKindModels {
			filtered := m.filteredModelIndices()
			if len(filtered) > 0 {
				pos := len(filtered) - 1
				for i, idx := range filtered {
					if idx == m.modalCursor {
						pos = i - 1
						break
					}
				}
				if pos < 0 {
					pos = len(filtered) - 1
				}
				m.modalCursor = filtered[pos]
				m.syncModelsModalScroll()
			}
			return nil
		}
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
	case "enter":
		switch m.modalKind {
		case pmModalKindAddProfile:
			m.createProfileFromModal()
		case pmModalKindProviderType:
			m.confirmProviderTypeSelection()
		case pmModalKindOpenAIAPIType:
			m.confirmAPITypeSelection()
		case pmModalKindModels:
			if m.modalCursor >= 0 && m.modalCursor < len(m.modelItems) {
				m.defaultModel = m.modelItems[m.modalCursor]
			}
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
		return len(m.filteredModelIndices())
	default:
		return len(m.providerOptions)
	}
}
