package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *mcpManagerModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := strings.ToLower(msg.String())

	if m.modalKind != mcpModalNone {
		return m, m.handleModalKey(msg)
	}

	if m.searching {
		return m, m.handleSearchInput(msg)
	}

	// Global keys.
	if cmd, ok := quitOnScreenBack(key); ok && m.page == mcpPageList {
		return m, cmd
	}
	if key == "/" && m.page == mcpPageList {
		m.searching = true
		return m, nil
	}

	switch m.page {
	case mcpPageDetail:
		return m, m.handleDetailKey(msg)
	case mcpPageBinding:
		return m, m.handleBindingKey(msg)
	default:
		return m, m.handleListKey(msg)
	}
}

func (m *mcpManagerModel) handleListKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())

	switch key {
	case "up", "k":
		if len(m.filtered) > 0 {
			m.selected = clampIndex(m.selected-1, len(m.filtered))
		}
	case "down", "j":
		if len(m.filtered) > 0 {
			m.selected = clampIndex(m.selected+1, len(m.filtered))
		}
	case "left", "h":
		agents := m.agents()
		m.matrixCursor = (m.matrixCursor - 1 + len(agents)) % len(agents)
	case "right", "l":
		agents := m.agents()
		m.matrixCursor = (m.matrixCursor + 1) % len(agents)
	case " ":
		return m.toggleCurrentAgentBinding()
	case "enter":
		if m.currentName() != "" {
			m.page = mcpPageDetail
			m.detailFocus = mcpDetailFocusBindings
			m.detailCursor = 0
		}
	case "t":
		if name := m.currentName(); name != "" {
			return m.testServerCmd(name, true)
		}
	case "a":
		m.openAddModal()
	case "i":
		m.modalKind = mcpModalImportPeer
		m.importPeer = 0
	}
	return nil
}

func (m *mcpManagerModel) handleDetailKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())
	agents := m.agents()

	switch key {
	case "esc", "q":
		if m.detailFocus == mcpDetailFocusActions {
			m.detailFocus = mcpDetailFocusBindings
			m.detailCursor = 0
			return nil
		}
		m.page = mcpPageList
		return nil
	case "tab", "shift+tab":
		if m.detailFocus == mcpDetailFocusBindings {
			m.detailFocus = mcpDetailFocusActions
			m.detailCursor = 0
		} else {
			m.detailFocus = mcpDetailFocusBindings
			m.detailCursor = 0
		}
		return nil
	}

	if m.detailFocus == mcpDetailFocusActions {
		switch key {
		case "left", "h":
			m.detailCursor = (m.detailCursor - 1 + 4) % 4
		case "right", "l":
			m.detailCursor = (m.detailCursor + 1) % 4
		case "enter", " ":
			return m.runDetailAction(m.detailCursor)
		}
		return nil
	}

	switch key {
	case "up", "k":
		m.detailCursor = (m.detailCursor - 1 + len(agents)) % len(agents)
	case "down", "j":
		m.detailCursor = (m.detailCursor + 1) % len(agents)
	case " ":
		name := m.currentName()
		if name == "" {
			return nil
		}
		agent := agents[clampIndex(m.detailCursor, len(agents))]
		enabled := !m.cfg.McpAgentEnabled(name, agent)
		m.cfg.ToggleMcpAgent(name, agent, enabled)
		m.status = successStatus(fmt.Sprintf("%s %s for %s.", m.agentLabel(agent), ternary(enabled, "enabled", "disabled"), name))
		return m.saveCfgCmd(m.status)
	case "enter":
		agent := agents[clampIndex(m.detailCursor, len(agents))]
		return m.openBindingEditor(agent)
	}
	return nil
}

func (m *mcpManagerModel) runDetailAction(idx int) tea.Cmd {
	name := m.currentName()
	switch idx {
	case 0: // Test
		if name != "" {
			return m.testServerCmd(name, true)
		}
	case 1: // Edit binding (cursor agent on bindings list)
		agents := m.agents()
		return m.openBindingEditor(agents[clampIndex(m.detailCursor, len(agents))])
	case 2: // Edit raw
		if name != "" {
			return m.openExternalEditor("definition", marshalNamedMCPServerYAML(name, m.currentServer()), "yaml")
		}
	case 3: // Delete
		if name != "" {
			m.modalKind = mcpModalDeleteConfirm
		}
	}
	return nil
}

func (m *mcpManagerModel) handleBindingKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())

	switch key {
	case "esc", "q":
		m.page = mcpPageDetail
		return nil
	case "up":
		m.bindCursor = (m.bindCursor - 1 + len(m.bindFields)) % len(m.bindFields)
		return nil
	case "down":
		m.bindCursor = (m.bindCursor + 1) % len(m.bindFields)
		return nil
	case "ctrl+s":
		return m.saveBinding()
	case "enter":
		if m.bindFields[m.bindCursor].Kind == mcpFieldKindSelect {
			m.cycleBindingSelect(1)
			return nil
		}
		if m.bindCursor < len(m.bindFields)-1 {
			m.bindCursor++
		}
		return nil
	}

	field := &m.bindFields[m.bindCursor]
	if field.Kind == mcpFieldKindSelect {
		switch key {
		case "left", "h":
			m.cycleBindingSelect(-1)
		case "right", "l", " ":
			m.cycleBindingSelect(1)
		}
		return nil
	}

	if field.Input.Value() != field.Value {
		field.Input.SetValue(field.Value)
	}
	if !field.Input.Focused() {
		field.Input.Focus()
	}
	var cmd tea.Cmd
	field.Input, cmd = field.Input.Update(msg)
	field.Value = field.Input.Value()
	return cmd
}

func (m *mcpManagerModel) cycleBindingSelect(delta int) {
	field := &m.bindFields[m.bindCursor]
	if len(field.Options) == 0 {
		return
	}
	curr := strings.ToLower(strings.TrimSpace(field.Value))
	for i, opt := range field.Options {
		if opt == curr {
			next := (i + delta + len(field.Options)) % len(field.Options)
			field.Value = field.Options[next]
			return
		}
	}
	field.Value = field.Options[0]
}

func (m *mcpManagerModel) handleSearchInput(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())
	switch key {
	case "esc", "enter":
		m.searching = false
		return nil
	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.refreshFiltered()
		}
		return nil
	case "ctrl+u":
		m.searchQuery = ""
		m.refreshFiltered()
		return nil
	}
	if len(msg.Runes) > 0 {
		m.searchQuery += string(filterPrintableRunes(msg.Runes))
		m.refreshFiltered()
	}
	return nil
}

func (m *mcpManagerModel) handleModalKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())

	switch m.modalKind {
	case mcpModalAdd:
		switch key {
		case "esc", "q":
			m.modalKind = mcpModalNone
		case "up", "k":
			m.addChoice = (m.addChoice - 1 + 4) % 4
		case "down", "j":
			m.addChoice = (m.addChoice + 1) % 4
		case "enter":
			return m.activateAddChoice()
		}
	case mcpModalPaste:
		switch {
		case key == "esc":
			m.modalKind = mcpModalNone
			m.status = "Import canceled."
		case key == "v" || key == "ctrl+e" || key == "ctrl+o":
			return m.openExternalEditor("paste", m.pasteInitial(), "toml")
		case key == "ctrl+s" || key == "f2" || (key == "enter" && !strings.Contains(m.pasteBuffer, "\n") && len(strings.TrimSpace(m.pasteBuffer)) > 0):
			return m.savePasteImport()
		case key == "up":
			m.movePasteCursorVertical(-1)
		case key == "down":
			m.movePasteCursorVertical(1)
		case key == "left":
			m.pasteCursor = clampIndexInclusive(m.pasteCursor-1, len([]rune(m.pasteBuffer)))
		case key == "right":
			m.pasteCursor = clampIndexInclusive(m.pasteCursor+1, len([]rune(m.pasteBuffer)))
		case key == "home":
			m.pasteCursor = 0
		case key == "end":
			m.pasteCursor = len([]rune(m.pasteBuffer))
		case key == "backspace":
			m.pasteBuffer, m.pasteCursor = DeleteBeforeCursor(m.pasteBuffer, m.pasteCursor)
		case key == "delete":
			m.pasteBuffer, m.pasteCursor = DeleteAtCursor(m.pasteBuffer, m.pasteCursor)
		case key == "enter":
			m.pasteBuffer, m.pasteCursor = InsertAtCursor(m.pasteBuffer, m.pasteCursor, []rune("\n"))
		default:
			if len(msg.Runes) > 0 {
				m.pasteBuffer, m.pasteCursor = InsertAtCursor(m.pasteBuffer, m.pasteCursor, filterPrintableRunes(msg.Runes))
			}
		}
	case mcpModalImportPeer:
		switch key {
		case "esc", "q":
			m.modalKind = mcpModalNone
			m.status = "Import canceled."
		case "up", "k":
			m.importPeer = (m.importPeer - 1 + len(mcpImportPeerSources)) % len(mcpImportPeerSources)
		case "down", "j":
			m.importPeer = (m.importPeer + 1) % len(mcpImportPeerSources)
		case "enter":
			return m.importFromPeer(mcpImportPeerSources[m.importPeer].key)
		}
	case mcpModalDeleteConfirm:
		switch key {
		case "y", "d", "enter":
			return m.deleteCurrent()
		case "n", "esc", "q":
			m.modalKind = mcpModalNone
			m.status = "Delete canceled."
		}
	case mcpModalTestDetail:
		switch key {
		case "esc", "q", "enter":
			m.modalKind = mcpModalNone
		case "t", "r":
			if name := m.currentName(); name != "" {
				return m.testServerCmd(name, true)
			}
		}
	}
	return nil
}

func (m *mcpManagerModel) movePasteCursorVertical(delta int) {
	r := []rune(m.pasteBuffer)
	if len(r) == 0 {
		m.pasteCursor = 0
		return
	}
	lineStart, _, col := cursorLineColumn(r, m.pasteCursor)
	if delta < 0 {
		if lineStart == 0 {
			return
		}
		prevLineEnd := lineStart - 1
		prevLineStart, _, _ := cursorLineColumn(r, prevLineEnd)
		prevLineLen := prevLineEnd - prevLineStart
		m.pasteCursor = prevLineStart + min(col, prevLineLen)
		return
	}
	_, lineEnd, _ := cursorLineColumn(r, m.pasteCursor)
	if lineEnd >= len(r) {
		return
	}
	nextLineStart := lineEnd + 1
	if nextLineStart > len(r) {
		return
	}
	_, nextLineEnd, _ := cursorLineColumn(r, nextLineStart)
	nextLineLen := nextLineEnd - nextLineStart
	m.pasteCursor = nextLineStart + min(col, nextLineLen)
}

// Mouse support: clicking a matrix cell selects the row and toggles that
// agent binding; clicking elsewhere in a row just selects it.
func (m *mcpManagerModel) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if !isPrimaryClick(msg.Type) {
		return nil
	}
	if m.modalKind != mcpModalNone {
		return nil // modals are keyboard-driven in this rewrite
	}
	if m.page != mcpPageList {
		return nil
	}
	row := msg.Y - m.listFirstRowY()
	if row < 0 || row >= len(m.filtered) {
		return nil
	}
	m.selected = row
	agents := m.agents()
	col := m.matrixColAtX(msg.X)
	if col >= 0 && col < len(agents) {
		return m.toggleAgentBinding(m.filtered[row], agents[col])
	}
	return nil
}

// listFirstRowY is the absolute Y of the first server row in the list page.
func (m *mcpManagerModel) listFirstRowY() int {
	// header row (1) + separator (1) + possible "↑ more" handled by offset.
	offset := 2
	if len(m.filtered) > 0 {
		visibleSlots := m.height - 10
		if visibleSlots < 5 {
			visibleSlots = 5
		}
		if visibleSlots > len(m.filtered) {
			visibleSlots = len(m.filtered)
		}
		_, _, showUp, _ := profileWindow(len(m.filtered), m.selected, visibleSlots)
		if showUp {
			offset++
		}
	}
	return offset + 3 // app margin/header lines above body
}

// matrixColAtX maps screen X to agent column index, or -1.
func (m *mcpManagerModel) matrixColAtX(x int) int {
	nameColW := 16
	if m.width-4 < 60 {
		nameColW = 12
	}
	start := nameColW + 2 // prefix(2) + name col
	for i := range m.agents() {
		if x >= start && x < start+7 {
			return i
		}
		start += 8
	}
	return -1
}

func (m *mcpManagerModel) openAddModal() {
	m.modalKind = mcpModalAdd
	m.addChoice = 0
}

func (m *mcpManagerModel) pasteInitial() string {
	if strings.TrimSpace(m.pasteBuffer) != "" {
		return m.pasteBuffer
	}
	return "# Paste MCP Server config in TOML, JSON, or YAML format\n# Examples:\n# [mcp_servers.my-server]\n# command = \"npx\"\n# args = [\"-y\", \"@modelcontextprotocol/server-sqlite\"]\n\n"
}
