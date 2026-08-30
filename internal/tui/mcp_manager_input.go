package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *mcpManagerModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())
	if cmd, ok := quitOnScreenBack(key); ok {
		return cmd
	}
	switch key {
	case "tab":
		m.moveBrowseFocus(1)
		return nil
	case "shift+tab":
		m.moveBrowseFocus(-1)
		return nil
	case "up", "k":
		m.moveBrowseSelection(-1)
	case "down", "j":
		m.moveBrowseSelection(1)
	case "left":
		if m.browseFocus == mcpBrowseFocusActions {
			m.moveActionSelection(-1)
		}
	case "right":
		if m.browseFocus == mcpBrowseFocusActions {
			m.moveActionSelection(1)
		}
	case "enter":
		return m.activateFocusedItem()
	case " ":
		return m.toggleCurrentEnabled()
	case "p":
		return m.probeCurrent()
	case "r":
		return m.probeAll()
	case "e":
		m.startEditCurrent()
	case "a":
		m.startAddEditor("stdio")
	case "t":
		m.openTransferMenu()
	case "d", "x":
		if m.currentName() != "" {
			m.confirmDelete = true
			m.status = fmt.Sprintf("Delete %s? Press Y to confirm or N to cancel.", m.currentName())
		}
	}
	return nil
}

func (m *mcpManagerModel) moveBrowseFocus(delta int) {
	focuses := []mcpBrowseFocus{mcpBrowseFocusServers, mcpBrowseFocusActions, mcpBrowseFocusQuickAdd}
	if len(m.names) == 0 {
		focuses = []mcpBrowseFocus{mcpBrowseFocusQuickAdd, mcpBrowseFocusActions}
	}
	current := 0
	for i, focus := range focuses {
		if focus == m.browseFocus {
			current = i
			break
		}
	}
	next := (current + delta) % len(focuses)
	if next < 0 {
		next += len(focuses)
	}
	m.browseFocus = focuses[next]
}

func (m *mcpManagerModel) moveBrowseSelection(delta int) {
	switch m.browseFocus {
	case mcpBrowseFocusQuickAdd:
		if len(m.quickAddItems) == 0 {
			return
		}
		m.quickAddIndex = clampIndex(m.quickAddIndex+delta, len(m.quickAddItems))
	case mcpBrowseFocusServers:
		if len(m.names) == 0 {
			return
		}
		m.selected = clampIndex(m.selected+delta, len(m.names))
	case mcpBrowseFocusActions:
		actions := m.browseActions()
		if len(actions) == 0 {
			return
		}
		m.actionIndex = clampIndex(m.actionIndex+delta, len(actions))
	}
}

func (m *mcpManagerModel) moveActionSelection(delta int) {
	actions := m.browseActions()
	if len(actions) == 0 {
		return
	}
	m.actionIndex = clampIndex(m.actionIndex+delta, len(actions))
}

func (m *mcpManagerModel) activateFocusedItem() tea.Cmd {
	switch m.browseFocus {
	case mcpBrowseFocusQuickAdd:
		if len(m.quickAddItems) == 0 {
			return nil
		}
		item := m.quickAddItems[clampIndex(m.quickAddIndex, len(m.quickAddItems))]
		switch item.Key {
		case "add":
			m.startAddEditor(item.Transport)
		case "transfer":
			m.openTransferMenu()
		}
	case mcpBrowseFocusActions:
		return m.activateCurrentAction()
	case mcpBrowseFocusServers:
		if m.currentName() != "" {
			m.status = fmt.Sprintf("Selected %s.\nReview diagnostics or press Enter on Actions to continue.", m.currentName())
		}
	}
	return nil
}

func (m *mcpManagerModel) activateCurrentAction() tea.Cmd {
	actions := m.browseActions()
	if len(actions) == 0 {
		return nil
	}
	action := actions[clampIndex(m.actionIndex, len(actions))]
	switch action.Key {
	case "probe":
		return m.probeCurrent()
	case "edit":
		m.startEditCurrent()
	case "toggle":
		return m.toggleCurrentEnabled()
	case "delete":
		if m.currentName() != "" {
			m.confirmDelete = true
			m.status = fmt.Sprintf("Delete %s? Press Y to confirm or N to cancel.", m.currentName())
		}
	case "transfer":
		m.openTransferMenu()
	}
	return nil
}

func (m *mcpManagerModel) handleTransferKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "esc", "q":
		m.transferring = false
		m.status = infoStatus("Transfer canceled.")
		return nil
	case "up", "k":
		if len(m.transferItems) > 0 {
			m.transferIndex = clampIndex(m.transferIndex-1, len(m.transferItems))
		}
		return nil
	case "down", "j":
		if len(m.transferItems) > 0 {
			m.transferIndex = clampIndex(m.transferIndex+1, len(m.transferItems))
		}
		return nil
	case "enter":
		return m.activateTransferItem()
	}
	return nil
}

func (m *mcpManagerModel) handleEditorKey(msg tea.KeyMsg) tea.Cmd {
	if m.editorMode == mcpEditorModeForm && m.editFocus >= 0 && m.editFocus < len(m.editFields) {
		field := m.editFields[m.editFocus]
		if field.kind == mcpEditKindSelect {
			switch msg.String() {
			case "left", "h":
				m.cycleSelectedField(-1)
				return nil
			case "right", "l":
				m.cycleSelectedField(1)
				return nil
			case "enter", " ":
				m.cycleSelectedField(1)
				return nil
			}
		}
	}
	switch msg.String() {
	case "esc":
		m.editing = false
		m.status = "Edit canceled."
		return nil
	case "tab":
		if m.editorMode == mcpEditorModeRaw {
			m.editing = false
			m.status = "Exited raw editor."
			return nil
		}
		m.moveEditFocus(1)
		return nil
	case "shift+tab":
		if m.editorMode == mcpEditorModeForm {
			m.moveEditFocus(-1)
		}
		return nil
	case "f2":
		return m.saveEditor(false)
	case "f5":
		return m.saveEditor(true)
	case "up":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorVertical(-1)
			return nil
		}
		m.moveFocusedFieldCursorVertical(-1)
		return nil
	case "down":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorVertical(1)
			return nil
		}
		m.moveFocusedFieldCursorVertical(1)
		return nil
	case "left":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorHorizontal(-1)
			return nil
		}
		m.moveFocusedFieldCursorHorizontal(-1)
		return nil
	case "right":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorHorizontal(1)
			return nil
		}
		m.moveFocusedFieldCursorHorizontal(1)
		return nil
	case "home":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorToLineBoundary(false)
			return nil
		}
		m.moveFocusedFieldCursorToLineBoundary(false)
		return nil
	case "end":
		if m.editorMode == mcpEditorModeRaw {
			m.moveRawCursorToLineBoundary(true)
			return nil
		}
		m.moveFocusedFieldCursorToLineBoundary(true)
		return nil
	case "f3":
		m.switchEditorMode(mcpEditorModeForm)
		return nil
	case "f4":
		m.switchEditorMode(mcpEditorModeRaw)
		return nil
	case "enter":
		if m.editorMode == mcpEditorModeRaw {
			m.rawEditor += "\n"
			return nil
		}
		if m.editorMode == mcpEditorModeForm && m.editFields[m.editFocus].multiline {
			m.editFields[m.editFocus].value += "\n"
			return nil
		}
	}
	if m.editorMode == mcpEditorModeRaw {
		m.handleRawInput(msg)
		return nil
	}
	m.handleFormInput(msg)
	return nil
}

func (m *mcpManagerModel) handleFormInput(msg tea.KeyMsg) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := &m.editFields[m.editFocus]
	if field.kind == mcpEditKindSelect {
		return
	}
	switch msg.String() {
	case "backspace":
		field.value, m.editCursor[m.editFocus] = deleteBeforeCursor(field.value, m.editCursor[m.editFocus])
		return
	case "delete":
		field.value, m.editCursor[m.editFocus] = deleteAtCursor(field.value, m.editCursor[m.editFocus])
		return
	}
	if len(msg.Runes) == 0 {
		return
	}
	field.value, m.editCursor[m.editFocus] = insertAtCursor(field.value, m.editCursor[m.editFocus], filterPrintableRunes(msg.Runes))
}

func (m *mcpManagerModel) handleRawInput(msg tea.KeyMsg) {
	switch msg.String() {
	case "backspace":
		m.rawEditor, m.rawCursor = deleteBeforeCursor(m.rawEditor, m.rawCursor)
		return
	case "delete":
		m.rawEditor, m.rawCursor = deleteAtCursor(m.rawEditor, m.rawCursor)
		return
	}
	if len(msg.Runes) == 0 {
		return
	}
	m.rawEditor, m.rawCursor = insertAtCursor(m.rawEditor, m.rawCursor, filterPrintableRunes(msg.Runes))
}

func (m *mcpManagerModel) handleConfirmKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.confirmDelete = false
		return m.deleteCurrent()
	case "n", "esc", "q":
		m.confirmDelete = false
		m.status = "Delete canceled."
	}
	return nil
}

func insertAtCursor(value string, cursor int, inserted []rune) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if len(inserted) == 0 {
		return value, cursor
	}
	updated := append(append(append([]rune{}, r[:cursor]...), inserted...), r[cursor:]...)
	return string(updated), cursor + len(inserted)
}

func deleteBeforeCursor(value string, cursor int) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if cursor == 0 {
		return value, cursor
	}
	updated := append(append([]rune{}, r[:cursor-1]...), r[cursor:]...)
	return string(updated), cursor - 1
}

func deleteAtCursor(value string, cursor int) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if cursor >= len(r) {
		return value, cursor
	}
	updated := append(append([]rune{}, r[:cursor]...), r[cursor+1:]...)
	return string(updated), cursor
}

func clampIndexInclusive(value, max int) int {
	if value < 0 {
		return 0
	}
	if value > max {
		return max
	}
	return value
}

func renderCursorText(value string, cursor int) string {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	cursorToken := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("|")
	if len(r) == 0 {
		return cursorToken
	}
	return string(r[:cursor]) + cursorToken + string(r[cursor:])
}

func cursorLineColumn(r []rune, cursor int) (lineStart, lineEnd, column int) {
	cursor = clampIndexInclusive(cursor, len(r))
	lineStart = cursor
	for lineStart > 0 && r[lineStart-1] != '\n' {
		lineStart--
	}
	lineEnd = cursor
	for lineEnd < len(r) && r[lineEnd] != '\n' {
		lineEnd++
	}
	column = cursor - lineStart
	return lineStart, lineEnd, column
}

func moveCursorHorizontal(value string, cursor, delta int) int {
	r := []rune(value)
	return clampIndexInclusive(cursor+delta, len(r))
}

func moveCursorToLineBoundary(value string, cursor int, end bool) int {
	r := []rune(value)
	lineStart, lineEnd, _ := cursorLineColumn(r, cursor)
	if end {
		return lineEnd
	}
	return lineStart
}

func moveCursorVertical(value string, cursor, delta int) int {
	r := []rune(value)
	lineStart, _, column := cursorLineColumn(r, cursor)
	if delta < 0 {
		if lineStart == 0 {
			return cursor
		}
		prevEnd := lineStart - 1
		prevStart := prevEnd
		for prevStart > 0 && r[prevStart-1] != '\n' {
			prevStart--
		}
		return min(prevStart+column, prevEnd)
	}
	lineEnd := moveCursorToLineBoundary(value, cursor, true)
	if lineEnd >= len(r) {
		return cursor
	}
	nextStart := lineEnd + 1
	nextEnd := nextStart
	for nextEnd < len(r) && r[nextEnd] != '\n' {
		nextEnd++
	}
	return min(nextStart+column, nextEnd)
}

func (m *mcpManagerModel) moveFocusedFieldCursorHorizontal(delta int) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := m.editFields[m.editFocus]
	if field.kind == mcpEditKindSelect {
		return
	}
	m.editCursor[m.editFocus] = moveCursorHorizontal(field.value, m.editCursor[m.editFocus], delta)
}

func (m *mcpManagerModel) moveFocusedFieldCursorVertical(delta int) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := m.editFields[m.editFocus]
	if field.kind == mcpEditKindSelect {
		return
	}
	m.editCursor[m.editFocus] = moveCursorVertical(field.value, m.editCursor[m.editFocus], delta)
}

func (m *mcpManagerModel) moveFocusedFieldCursorToLineBoundary(end bool) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := m.editFields[m.editFocus]
	if field.kind == mcpEditKindSelect {
		return
	}
	m.editCursor[m.editFocus] = moveCursorToLineBoundary(field.value, m.editCursor[m.editFocus], end)
}

func (m *mcpManagerModel) moveRawCursorHorizontal(delta int) {
	m.rawCursor = moveCursorHorizontal(m.rawEditor, m.rawCursor, delta)
}

func (m *mcpManagerModel) moveRawCursorVertical(delta int) {
	m.rawCursor = moveCursorVertical(m.rawEditor, m.rawCursor, delta)
}

func (m *mcpManagerModel) moveRawCursorToLineBoundary(end bool) {
	m.rawCursor = moveCursorToLineBoundary(m.rawEditor, m.rawCursor, end)
}

