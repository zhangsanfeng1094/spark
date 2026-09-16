package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *mcpManagerModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := strings.ToLower(msg.String())

	// If a modal is open, dispatch directly to modal handlers
	if m.modalKind != mcpModalNone {
		return m, m.handleModalKey(msg)
	}

	// If in search mode on the list
	if m.searching {
		return m, m.handleSearchInput(msg)
	}

	// Global keybindings
	if cmd, ok := quitOnScreenBack(key); ok && m.focusArea == mcpFocusList {
		return m, cmd
	}

	switch key {
	case "tab":
		m.rotateFocus(1)
		return m, m.updateFocus()
	case "shift+tab":
		m.rotateFocus(-1)
		return m, m.updateFocus()
	}

	switch m.focusArea {
	case mcpFocusList:
		return m, m.handleListKey(msg)
	case mcpFocusFields:
		return m, m.handleFieldsKey(msg)
	case mcpFocusActions:
		return m, m.handleActionsKey(msg)
	}

	return m, nil
}

func (m *mcpManagerModel) rotateFocus(delta int) {
	areas := []mcpFocusArea{mcpFocusList, mcpFocusFields, mcpFocusActions}
	if len(m.filtered) == 0 {
		areas = []mcpFocusArea{mcpFocusList, mcpFocusActions}
	}
	current := 0
	for i, a := range areas {
		if a == m.focusArea {
			current = i
			break
		}
	}
	next := (current + delta) % len(areas)
	if next < 0 {
		next += len(areas)
	}
	m.focusArea = areas[next]
}

func (m *mcpManagerModel) handleListKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())

	switch key {
	case "up", "k":
		if len(m.filtered) > 0 {
			m.selected = clampIndex(m.selected-1, len(m.filtered))
			m.loadCurrentDraft()
		}
	case "down", "j":
		if len(m.filtered) > 0 {
			m.selected = clampIndex(m.selected+1, len(m.filtered))
			m.loadCurrentDraft()
		}
	case "enter":
		if len(m.filtered) > 0 {
			m.focusArea = mcpFocusFields
			m.focusField = 0
			return m.updateFocus()
		}
	case " ":
		return m.toggleCurrentEnabled()
	}
	return nil
}

func (m *mcpManagerModel) handleFieldsKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())

	switch key {
	case "esc":
		m.focusArea = mcpFocusList
		return m.updateFocus()
	case "up":
		m.moveFieldFocus(-1)
		return m.updateFocus()
	case "down":
		m.moveFieldFocus(1)
		return m.updateFocus()
	}

	visible := m.visibleFieldIndices()
	if len(visible) == 0 || m.focusField < 0 || m.focusField >= len(visible) {
		return nil
	}
	actualFieldIdx := visible[m.focusField]
	field := &m.draftFields[actualFieldIdx]

	if field.Kind == mcpFieldKindSelect {
		switch key {
		case "left", "h":
			m.cycleSelectField(field, -1)
			m.dirty = true
		case "right", "l", "enter", " ", "space":
			m.cycleSelectField(field, 1)
			m.dirty = true
		}
		return nil
	}

	// Text input navigation
	if key == "enter" {
		if m.focusField < len(visible)-1 {
			m.focusField++
			return m.updateFocus()
		}
		m.focusArea = mcpFocusActions
		m.actionIdx = 1 // default to Save
		return m.updateFocus()
	}

	// Forward key to textinput component
	if field.Input.Value() != field.Value {
		field.Input.SetValue(field.Value)
	}
	if !field.Input.Focused() {
		field.Input.Focus()
	}
	var cmd tea.Cmd
	field.Input, cmd = field.Input.Update(msg)
	newVal := field.Input.Value()
	if newVal != field.Value {
		field.Value = newVal
		m.dirty = true
	}
	m.fieldCursor[actualFieldIdx] = field.Input.Position()
	return cmd
}

func (m *mcpManagerModel) moveFieldFocus(delta int) {
	visible := m.visibleFieldIndices()
	if len(visible) == 0 {
		return
	}
	m.focusField = (m.focusField + delta) % len(visible)
	if m.focusField < 0 {
		m.focusField += len(visible)
	}
}

func (m *mcpManagerModel) clampFocusField() {
	visible := m.visibleFieldIndices()
	if len(visible) == 0 {
		m.focusField = 0
		return
	}
	if m.focusField >= len(visible) {
		m.focusField = len(visible) - 1
	}
}

func (m *mcpManagerModel) cycleSelectField(field *mcpFormField, delta int) {
	if len(field.Options) == 0 {
		return
	}
	curr := strings.ToLower(strings.TrimSpace(field.Value))
	for i, opt := range field.Options {
		if opt == curr {
			next := (i + delta) % len(field.Options)
			if next < 0 {
				next += len(field.Options)
			}
			field.Value = field.Options[next]
			m.clampFocusField()
			return
		}
	}
	field.Value = field.Options[0]
	m.clampFocusField()
}

func (m *mcpManagerModel) handleActionsKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())

	switch key {
	case "esc":
		m.focusArea = mcpFocusList
		return m.updateFocus()
	case "left", "h":
		m.actionIdx = (m.actionIdx - 1 + 4) % 4
	case "right", "l":
		m.actionIdx = (m.actionIdx + 1) % 4
	case "enter", " ", "space":
		return m.runAction(m.actionIdx)
	}
	return nil
}

func (m *mcpManagerModel) runAction(idx int) tea.Cmd {
	switch idx {
	case 0: // Probe
		return m.probeCurrent(true)
	case 1: // Save
		return m.saveDraft(false)
	case 2: // Vim
		return m.openExternalEditorForCurrent()
	case 3: // Delete
		if m.currentName() != "" {
			m.modalKind = mcpModalDeleteConfirm
		}
	}
	return nil
}

func (m *mcpManagerModel) handleMainMouse(msg tea.MouseMsg) tea.Cmd {
	x, y := msg.X, msg.Y
	leftX1, leftY1 := m.leftContentX, m.leftContentY
	rightX1, rightY1 := m.rightContentX, m.rightContentY

	leftW := m.leftPanelWidth
	if leftW <= 0 {
		leftW = 30
	}

	// 1. Check click in left list items
	if x >= leftX1 && x <= leftX1+leftW {
		for i, row := range m.leftVisibleRows {
			if y == leftY1+row && i < len(m.leftVisibleIdxs) {
				m.focusArea = mcpFocusList
				m.selected = m.leftVisibleIdxs[i]
				m.loadCurrentDraft()
				return nil
			}
		}

		// 2. Check click in left bottom buttons (Add, Import, Transfer, Refresh)
		leftBtnsY1 := leftY1 + m.leftButtonsRelY
		leftBtnsY2 := leftBtnsY1 + max(1, m.leftButtonsRelH) - 1
		leftBtns2Y1 := leftY1 + m.leftButtonsRow2Y
		leftBtns2Y2 := leftBtns2Y1 + max(1, m.leftButtonsRelH) - 1

		if y >= leftBtnsY1 && y <= leftBtnsY2 {
			addW := max(1, m.leftAddBtnW)
			if x >= leftX1 && x < leftX1+addW {
				m.openAddModal()
				return nil
			}
			if x >= leftX1+addW && x <= leftX1+m.leftButtonsRowW {
				m.openImportModal()
				return nil
			}
		}
		if y >= leftBtns2Y1 && y <= leftBtns2Y2 {
			transW := max(1, m.leftTransferBtnW)
			if x >= leftX1 && x < leftX1+transW {
				m.openTransferModal()
				return nil
			}
			if x >= leftX1+transW && x <= leftX1+m.leftButtonsRow2W {
				return m.probeAll()
			}
		}
	}

	// 3. Check click in right panel fields
	fieldY := y - rightY1
	inputStartX := rightX1 + pmLabelWidth + 1
	inputWidth := m.inputWidth
	if inputWidth <= 0 {
		inputWidth = pmInputWidth
	}
	inputEndX := inputStartX + inputWidth + 3

	if x >= inputStartX && x <= inputEndX {
		for i := range m.fieldStartRelY {
			if fieldY >= m.fieldStartRelY[i] && fieldY <= m.fieldEndRelY[i] {
				m.focusArea = mcpFocusFields
				m.focusField = i
				actualIdx := m.fieldActualIndices[i]
				field := &m.draftFields[actualIdx]
				if field.Kind == mcpFieldKindSelect {
					m.cycleSelectField(field, 1)
					m.dirty = true
					return nil
				}
				return m.updateFocus()
			}
		}
	}

	// 4. Check click in right panel action buttons ([P] Probe, [Ctrl+S] Save, [V] Vim, [D] Delete)
	rightBtnsY1 := rightY1 + m.rightButtonsRelY
	rightBtnsY2 := rightBtnsY1 + max(1, m.rightButtonsRelH) - 1
	if y >= rightBtnsY1 && y <= rightBtnsY2 {
		gap := 2
		btn0X1 := rightX1
		btn0X2 := btn0X1 + m.rightProbeBtnW
		btn1X1 := btn0X2 + gap
		btn1X2 := btn1X1 + m.rightSaveBtnW
		btn2X1 := btn1X2 + gap
		btn2X2 := btn2X1 + m.rightVimBtnW
		btn3X1 := btn2X2 + gap
		btn3X2 := btn3X1 + m.rightDeleteBtnW

		if x >= btn0X1 && x <= btn0X2 {
			m.focusArea = mcpFocusActions
			m.actionIdx = 0
			return m.runAction(0)
		}
		if x >= btn1X1 && x <= btn1X2 {
			m.focusArea = mcpFocusActions
			m.actionIdx = 1
			return m.runAction(1)
		}
		if x >= btn2X1 && x <= btn2X2 {
			m.focusArea = mcpFocusActions
			m.actionIdx = 2
			return m.runAction(2)
		}
		if x >= btn3X1 && x <= btn3X2 {
			m.focusArea = mcpFocusActions
			m.actionIdx = 3
			return m.runAction(3)
		}
	}

	return nil
}

func (m *mcpManagerModel) handleModalMouse(msg tea.MouseMsg) {
	x, y := msg.X, msg.Y
	// Click outside closes modal
	if x < m.modalX || x >= m.modalX+m.modalW || y < m.modalY || y >= m.modalY+m.modalH {
		m.modalKind = mcpModalNone
		return
	}

	row := y - m.modalOptionStartY
	switch m.modalKind {
	case mcpModalAdd:
		if row >= 0 && row < 3 {
			m.addTransport = row
			m.confirmAddFromModal()
		}
	case mcpModalTransfer:
		if row >= 0 && row < len(m.transferItems) {
			m.transferIndex = row
			_ = m.activateTransferItem()
		}
	case mcpModalDeleteConfirm:
		btnRowY := m.modalY + m.modalH - 2
		if y == btnRowY {
			if x < m.modalX+m.modalW/2 {
				_ = m.deleteCurrent()
			} else {
				m.modalKind = mcpModalNone
			}
		}
	case mcpModalProbeDetail:
		m.modalKind = mcpModalNone
	case mcpModalImport:
		btnRowY := m.modalY + m.modalH - 2
		if y == btnRowY {
			third := m.modalW / 3
			if x < m.modalX+third {
				_ = m.openExternalEditorForImport()
			} else if x < m.modalX+2*third {
				_ = m.saveImportModal()
			} else {
				m.modalKind = mcpModalNone
			}
		}
	}
}

func (m *mcpManagerModel) handleModalKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())

	switch m.modalKind {
	case mcpModalDeleteConfirm:
		switch key {
		case "y", "d", "enter":
			return m.deleteCurrent()
		case "n", "esc", "q":
			m.modalKind = mcpModalNone
			m.status = "Delete canceled."
		}
	case mcpModalAdd:
		switch key {
		case "esc", "q":
			m.modalKind = mcpModalNone
		case "up", "k":
			m.addTransport = (m.addTransport - 1 + 3) % 3
		case "down", "j":
			m.addTransport = (m.addTransport + 1) % 3
		case "enter":
			m.confirmAddFromModal()
		}
	case mcpModalImport:
		switch {
		case key == "esc":
			m.modalKind = mcpModalNone
			m.status = "Import canceled."
		case key == "v" || key == "ctrl+e" || key == "ctrl+o":
			return m.openExternalEditorForImport()
		case key == "ctrl+s" || key == "f2" || (key == "enter" && !strings.Contains(m.importBuffer, "\n") && len(strings.TrimSpace(m.importBuffer)) > 0):
			return m.saveImportModal()
		case key == "up":
			m.moveImportCursorVertical(-1)
		case key == "down":
			m.moveImportCursorVertical(1)
		case key == "left":
			m.importCursor = clampIndexInclusive(m.importCursor-1, len([]rune(m.importBuffer)))
		case key == "right":
			m.importCursor = clampIndexInclusive(m.importCursor+1, len([]rune(m.importBuffer)))
		case key == "home":
			m.importCursor = 0
		case key == "end":
			m.importCursor = len([]rune(m.importBuffer))
		case key == "backspace":
			m.importBuffer, m.importCursor = DeleteBeforeCursor(m.importBuffer, m.importCursor)
		case key == "delete":
			m.importBuffer, m.importCursor = DeleteAtCursor(m.importBuffer, m.importCursor)
		case key == "enter":
			m.importBuffer, m.importCursor = InsertAtCursor(m.importBuffer, m.importCursor, []rune("\n"))
		default:
			if len(msg.Runes) > 0 {
				m.importBuffer, m.importCursor = InsertAtCursor(m.importBuffer, m.importCursor, filterPrintableRunes(msg.Runes))
			}
		}
	case mcpModalTransfer:
		switch key {
		case "esc", "q":
			m.modalKind = mcpModalNone
			m.status = "Transfer canceled."
		case "up", "k":
			if len(m.transferItems) > 0 {
				m.transferIndex = clampIndex(m.transferIndex-1, len(m.transferItems))
			}
		case "down", "j":
			if len(m.transferItems) > 0 {
				m.transferIndex = clampIndex(m.transferIndex+1, len(m.transferItems))
			}
		case "enter":
			return m.activateTransferItem()
		}
	case mcpModalProbeDetail:
		switch key {
		case "esc", "q", "enter":
			m.modalKind = mcpModalNone
		case "p", "r":
			return m.probeCurrent(true)
		case "e":
			m.modalKind = mcpModalNone
			m.focusArea = mcpFocusFields
			m.focusField = 0
		}
	}
	return nil
}

func (m *mcpManagerModel) moveImportCursorVertical(delta int) {
	r := []rune(m.importBuffer)
	if len(r) == 0 {
		m.importCursor = 0
		return
	}
	lineStart, _, col := cursorLineColumn(r, m.importCursor)
	if delta < 0 {
		if lineStart == 0 {
			return
		}
		prevLineEnd := lineStart - 1
		prevLineStart, _, _ := cursorLineColumn(r, prevLineEnd)
		prevLineLen := prevLineEnd - prevLineStart
		m.importCursor = prevLineStart + min(col, prevLineLen)
		return
	}
	_, lineEnd, _ := cursorLineColumn(r, m.importCursor)
	if lineEnd >= len(r) {
		return
	}
	nextLineStart := lineEnd + 1
	if nextLineStart > len(r) {
		return
	}
	_, nextLineEnd, _ := cursorLineColumn(r, nextLineStart)
	nextLineLen := nextLineEnd - nextLineStart
	m.importCursor = nextLineStart + min(col, nextLineLen)
}

func (m *mcpManagerModel) handleSearchInput(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())
	switch key {
	case "esc", "enter":
		m.searching = false
		m.status = fmt.Sprintf("Search finished. Showing %d of %d servers.", len(m.filtered), len(m.names))
		return nil
	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.refreshFiltered()
			m.loadCurrentDraft()
		}
		return nil
	case "ctrl+u":
		m.searchQuery = ""
		m.refreshFiltered()
		m.loadCurrentDraft()
		return nil
	}
	if len(msg.Runes) > 0 {
		m.searchQuery += string(filterPrintableRunes(msg.Runes))
		m.refreshFiltered()
		m.loadCurrentDraft()
	}
	return nil
}

func (m *mcpManagerModel) cycleFilter() {
	m.filterOption = (m.filterOption + 1) % 7
	m.refreshFiltered()
	m.loadCurrentDraft()
	m.status = fmt.Sprintf("Filter: %s (%d servers)", filterOptionLabel(m.filterOption), len(m.filtered))
}

func (m *mcpManagerModel) openAddModal() {
	m.modalKind = mcpModalAdd
	m.addTransport = 0
	m.addName = ""
}

func (m *mcpManagerModel) openImportModal() {
	m.modalKind = mcpModalImport
	m.importBuffer = ""
	m.importCursor = 0
	m.status = "Paste snippet / enter file path / press [V] for Vim. [Ctrl+S] Import  [Esc] Cancel"
}

func (m *mcpManagerModel) openTransferModal() {
	m.modalKind = mcpModalTransfer
	m.transferIndex = 0
	m.status = "Choose a transfer action. [Enter] Select  [Esc] Cancel"
}

func filterOptionLabel(opt mcpFilterOption) string {
	switch opt {
	case mcpFilterHealthy:
		return "Healthy"
	case mcpFilterError:
		return "Error"
	case mcpFilterUnknown:
		return "Unknown"
	case mcpFilterDisabled:
		return "Disabled"
	case mcpFilterStdio:
		return "stdio"
	case mcpFilterHTTP:
		return "HTTP/SSE"
	default:
		return "All"
	}
}
