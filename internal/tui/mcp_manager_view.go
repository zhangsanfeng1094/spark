package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
	"spark/internal/mcp"
)

// View renders the MCP manager: list+matrix page, detail page, binding page,
// plus overlay modals.
func (m *mcpManagerModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	var body string
	switch m.page {
	case mcpPageDetail:
		body = m.renderDetailPage()
	case mcpPageBinding:
		body = m.renderBindingPage()
	default:
		body = m.renderListPage()
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(m.pageTitle())
	sub := lipgloss.NewStyle().Foreground(colorMuted).Render(m.headerSummary())
	headerRow := header + "    " + sub

	footer := lipgloss.NewStyle().Foreground(colorMuted).Render(m.footerHelp())
	ui := pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, headerRow, "", body, "", footer))

	if m.modalKind != mcpModalNone {
		return fitToViewportHeight(m.overlayModal(ui), m.height)
	}
	return fitToViewportHeight(ui, m.height)
}

func (m *mcpManagerModel) pageTitle() string {
	switch m.page {
	case mcpPageDetail:
		return "MCP · " + m.currentName()
	case mcpPageBinding:
		return fmt.Sprintf("MCP · %s · %s binding", m.currentName(), m.agentLabel(m.bindAgent))
	default:
		return "MCP"
	}
}

func (m *mcpManagerModel) footerHelp() string {
	if m.modalKind != mcpModalNone {
		return "Enter confirm · Esc cancel"
	}
	switch m.page {
	case mcpPageDetail:
		if m.detailFocus == mcpDetailFocusActions {
			return "←→ choose action · Enter run · Esc back to list"
		}
		return "↑↓ select agent · Space toggle binding · Enter edit binding · Esc back"
	case mcpPageBinding:
		return "↑↓ fields · Enter next field · Ctrl+S save · Esc back"
	default:
		if m.searching {
			return "type to search · Enter done · Esc cancel"
		}
		return "Enter details · Space toggle agent binding · T test · A add · I import · / search"
	}
}

// renderListPage renders the single server list with the agent matrix.
func (m *mcpManagerModel) renderListPage() string {
	agents := m.agents()
	listWidth := m.width - 4
	nameColW := 16
	if listWidth < 60 {
		nameColW = 12
	}

	var lines []string

	// Matrix header row.
	headFirst := "Servers"
	if m.searching {
		headFirst = "Search: " + m.searchQuery
	} else if len(m.filtered) != len(m.names) {
		headFirst = fmt.Sprintf("Servers %d/%d", len(m.filtered), len(m.names))
	}
	header := lipgloss.NewStyle().Width(nameColW).Foreground(colorLabel).Bold(true).Render(truncateDisplay(headFirst, nameColW))
	cells := make([]string, 0, len(agents))
	for _, agent := range agents {
		cells = append(cells, lipgloss.NewStyle().Width(7).Align(lipgloss.Center).Foreground(colorLabel).Render(truncateDisplay(m.agentLabel(agent), 6)))
	}
	lines = append(lines, header+strings.Join(cells, " "))
	lines = append(lines, lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", min(listWidth, m.width-2))))

	if len(m.filtered) == 0 {
		hint := "  No MCP servers configured"
		if m.searchQuery != "" {
			hint = "  No servers match " + m.searchQuery
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render(hint))
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Render("  Press A to add a server, I to import from existing agent configs."))
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	visibleSlots := m.height - 10
	if visibleSlots < 5 {
		visibleSlots = 5
	}
	if visibleSlots > len(m.filtered) {
		visibleSlots = len(m.filtered)
	}
	start, end, showUp, showDown := profileWindow(len(m.filtered), m.selected, visibleSlots)
	if showUp {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render("  ↑ more"))
	}
	for i := start; i < end; i++ {
		lines = append(lines, m.renderListRow(i, nameColW, agents))
	}
	if showDown {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render("  ↓ more"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderListRow renders one server row: name + per-agent cells.
func (m *mcpManagerModel) renderListRow(idx int, nameColW int, agents []string) string {
	name := m.filtered[idx]
	focused := idx == m.selected

	prefix := "  "
	if focused {
		prefix = "▶ "
	}

	nameStyle := lipgloss.NewStyle().Width(nameColW).Foreground(colorTextSoft)
	if focused {
		nameStyle = nameStyle.Foreground(colorText).Bold(true)
	}

	cells := make([]string, 0, len(agents))
	for _, agent := range agents {
		glyph := m.renderCell(name, agent)
		if m.isCursorAgent(agent) && focused {
			glyph = lipgloss.NewStyle().Foreground(colorFocus).Bold(true).Render(matrixCellGlyph(m.cellStatus(name, agent)))
		}
		cells = append(cells, lipgloss.NewStyle().Width(7).Align(lipgloss.Center).Render(glyph))
	}

	row := prefix + nameStyle.Render(truncateDisplay(name, nameColW)) + strings.Join(cells, " ")
	if focused {
		return lipgloss.NewStyle().Bold(true).Render(row)
	}
	return row
}

// isCursorAgent reports whether the agent column is under the toggle cursor.
func (m *mcpManagerModel) isCursorAgent(agent string) bool {
	return m.matrixCursor == m.agentIndex(agent)
}

func (m *mcpManagerModel) agentIndex(agent string) int {
	for i, a := range m.agents() {
		if a == agent {
			return i
		}
	}
	return -1
}

// renderDetailPage shows common definition, agent bindings, last test state,
// and tool count.
func (m *mcpManagerModel) renderDetailPage() string {
	name := m.currentName()
	server := m.cfg.GetMcpServer(name)
	if server == nil {
		return lipgloss.NewStyle().Foreground(colorMuted).Render("Server no longer exists. Press Esc.")
	}

	var lines []string
	contentW := m.width - 6

	// Definition section.
	lines = append(lines, renderFormSectionHeader("Definition", contentW))
	transport := "stdio · " + server.Command
	if strings.TrimSpace(server.URL) != "" {
		transport = "remote · " + server.URL
	}
	lines = append(lines, "  "+renderDetailRow("Transport", transport, contentW))
	if len(server.Args) > 0 {
		lines = append(lines, "  "+renderDetailRow("Args", strings.Join(server.Args, " "), contentW))
	}
	if len(server.Env) > 0 {
		lines = append(lines, "  "+renderDetailRow("Env", fmt.Sprintf("%d variable(s)", len(server.Env)), contentW))
	}

	// Agent bindings.
	lines = append(lines, "", renderFormSectionHeader("Agent bindings", contentW))
	for i, agent := range m.agents() {
		enabled := m.cfg.McpAgentEnabled(name, agent)
		status := m.cellStatus(name, agent)
		glyph := "○"
		glyphStyle := lipgloss.NewStyle().Foreground(colorDim)
		if enabled {
			switch status.Kind {
			case mcp.StatusOK:
				glyph, glyphStyle = "●", lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
			case mcp.StatusError:
				glyph, glyphStyle = "!", lipgloss.NewStyle().Foreground(colorError).Bold(true)
			default:
				glyph, glyphStyle = "?", lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
			}
		}
		statusText := "disabled"
		if enabled {
			statusText = status.Headline
			if status.Kind == mcp.StatusNotChecked {
				statusText = "Not checked"
			}
		}
		overrides := m.bindingOverrideSummary(name, agent)

		prefix := "  "
		rowStyle := lipgloss.NewStyle().Foreground(colorTextSoft)
		if m.detailFocus == mcpDetailFocusBindings && i == m.detailCursor {
			prefix = "▶ "
			rowStyle = lipgloss.NewStyle().Foreground(colorText).Bold(true)
		}
		row := fmt.Sprintf("%s%s %-7s %-18s %s", prefix, glyphStyle.Render(glyph), m.agentLabel(agent), statusText, lipgloss.NewStyle().Foreground(colorMuted).Render(overrides))
		lines = append(lines, rowStyle.Render(truncateDisplay(row, m.width-4)))
	}

	// Last test.
	lines = append(lines, "", renderFormSectionHeader("Last test", contentW))
	result := m.tests[name]
	if result == nil {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorMuted).Render("Not checked — press T to run a server-level test"))
	} else if result.Err != "" {
		status := mcp.Summarize(server, result)
		lines = append(lines,
			"  "+lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕ "+status.Headline+" ("+string(result.Stage)+" failed)"),
			"    "+lipgloss.NewStyle().Foreground(colorTextSoft).Render(truncateDisplay(result.Err, contentW-4)),
		)
		if len(status.Suggestions) > 0 {
			lines = append(lines, "    "+lipgloss.NewStyle().Foreground(colorWarning).Render("Tip: "+status.Suggestions[0]))
		}
	} else {
		lines = append(lines,
			"  "+lipgloss.NewStyle().Foreground(colorSuccess).Render(fmt.Sprintf("✓ OK · %d tool(s) · %s", result.ToolsCount, fmtTestAge(result.ProbedAt))),
		)
		if len(result.ToolNames) > 0 {
			toolList := strings.Join(result.ToolNames[:min(4, len(result.ToolNames))], ", ")
			if len(result.ToolNames) > 4 {
				toolList += fmt.Sprintf(" (+%d more)", len(result.ToolNames)-4)
			}
			lines = append(lines, "    "+lipgloss.NewStyle().Foreground(colorMuted).Render(toolList))
		}
	}

	// Actions.
	lines = append(lines, "", m.renderDetailActions())
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// bindingOverrideSummary lists non-inherited values on an agent binding.
func (m *mcpManagerModel) bindingOverrideSummary(name, agent string) string {
	binding := m.cfg.McpBinding(name, agent)
	if binding == nil {
		return "inherits definition"
	}
	var parts []string
	if binding.Command != "" {
		parts = append(parts, "command="+binding.Command)
	}
	if binding.URL != "" {
		parts = append(parts, "url="+binding.URL)
	}
	if len(binding.Args) > 0 {
		parts = append(parts, fmt.Sprintf("%d arg(s)", len(binding.Args)))
	}
	if len(binding.Env) > 0 {
		parts = append(parts, fmt.Sprintf("%d env", len(binding.Env)))
	}
	if hasCodexAdvanced(binding) {
		parts = append(parts, "advanced")
	}
	if len(parts) == 0 {
		return "no overrides"
	}
	return strings.Join(parts, ", ")
}

func hasCodexAdvanced(binding *config.McpAgentBinding) bool {
	return binding.StartupTimeout != nil || binding.ToolTimeout != nil ||
		len(binding.EnabledTools) > 0 || len(binding.DisabledTools) > 0 ||
		len(binding.Scopes) > 0 || binding.OAuthResource != nil || len(binding.Tools) > 0
}

func renderDetailRow(label, value string, width int) string {
	lw := 12
	vw := max(10, width-lw-2)
	return lipgloss.NewStyle().Width(lw).Foreground(colorLabel).Render(label) + " " +
		lipgloss.NewStyle().Width(vw).Foreground(colorTextSoft).Render(truncateDisplay(value, vw))
}

// renderDetailActions renders the action row on the detail page.
func (m *mcpManagerModel) renderDetailActions() string {
	actions := []string{"Test", "Edit binding", "Edit raw", "Delete"}
	cells := make([]string, 0, len(actions))
	for i, label := range actions {
		if m.detailFocus == mcpDetailFocusActions && i == m.detailCursor {
			cells = append(cells, pmCompactPrimaryBtnStyle.Copy().MarginRight(0).Render(label))
		} else {
			cells = append(cells, pmCompactBtnStyle.Copy().MarginRight(0).Render(label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// renderBindingPage edits one agent's binding for the selected server.
func (m *mcpManagerModel) renderBindingPage() string {
	var lines []string
	contentW := m.width - 6
	lines = append(lines, renderFormSectionHeader("Agent binding", contentW))

	for i, field := range m.bindFields {
		focused := i == m.bindCursor
		var inputView string
		if field.Kind == mcpFieldKindSelect {
			inputView = renderSegmentedPills(field.Options, field.Value, focused, contentW-pmLabelWidth-2)
		} else {
			if field.Input.Value() != field.Value {
				field.Input.SetValue(field.Value)
				field.Input.CursorEnd()
			}
			field.Input.Width = max(10, contentW-pmLabelWidth-6)
			field.Input.Placeholder = field.Placeholder
			if focused {
				field.Input.Focus()
			} else {
				field.Input.Blur()
			}
			inputView = field.Input.View()
		}
		row := renderCompactFormRow(compactFormRowOptions{
			Label:     field.Label,
			Value:     field.Value,
			Width:     contentW - pmLabelWidth - 4,
			Focused:   focused,
			InputView: inputView,
		})
		lines = append(lines, row)
	}

	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Render(
		"Empty values inherit the shared definition. Codex advanced fields apply to Codex only."))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// overlayModal renders modal popups over the page.
func (m *mcpManagerModel) overlayModal(bg string) string {
	var modalContent string
	modalWidth := 56
	switch m.modalKind {
	case mcpModalAdd:
		modalContent = m.renderAddModal()
		modalWidth = 54
	case mcpModalPaste:
		modalContent = m.renderPasteModal()
		modalWidth = max(56, min(90, m.width-20))
	case mcpModalImportPeer:
		modalContent = m.renderImportPeerModal()
		modalWidth = 54
	case mcpModalDeleteConfirm:
		modalContent = m.renderDeleteModal()
		modalWidth = 50
	case mcpModalTestDetail:
		modalContent = m.renderTestDetailModal()
		modalWidth = 64
	}

	modalBox := pmModalStyle.Width(modalWidth).Background(colorPanelBg).Render(modalContent)
	x := max(0, (m.width-lipgloss.Width(modalBox))/2)
	y := max(0, (m.height-lipgloss.Height(modalBox))/2)
	if bg != "" {
		return overlayBox(bg, modalBox, x, y)
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modalBox)
}

// renderAddModal: Paste config / Local command / Remote URL / Import existing.
func (m *mcpManagerModel) renderAddModal() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Add MCP server")
	options := []struct{ label, desc string }{
		{"Paste config", "JSON / YAML / TOML — auto-detected"},
		{"Local command", "stdio server launched from a command"},
		{"Remote URL", "HTTP / SSE endpoint"},
		{"Import existing", "pull servers from Codex / Claude configs"},
	}
	var lines []string
	lines = append(lines, title, "", lipgloss.NewStyle().Foreground(colorMuted).Render("Choose how to add:"), "")
	for i, opt := range options {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(colorTextSoft)
		if i == m.addChoice {
			prefix = "▶ "
			style = lipgloss.NewStyle().Foreground(colorText).Bold(true)
		}
		lines = append(lines, style.Render(prefix+opt.label)+"  "+lipgloss.NewStyle().Foreground(colorDim).Render(opt.desc))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Render("Enter continue · Esc cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderPasteModal: paste buffer; format detection stays out of the UI.
func (m *mcpManagerModel) renderPasteModal() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Paste MCP config")
	desc := lipgloss.NewStyle().Foreground(colorMuted).Render("Paste JSON / YAML / TOML or a file path — format is detected automatically")
	contentWidth := max(36, m.width-40)

	editorText := renderCursorText(m.pasteBuffer, m.pasteCursor)
	inputBox := pmFocusedInputStyle.Copy().Width(contentWidth).Height(max(8, m.height-14)).Render(editorText)

	vimBtn := pmCompactPrimaryBtnStyle.Copy().MarginRight(0).Render("Open in Editor")
	importBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("Import")
	cancelBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("Cancel")
	actionRow := lipgloss.JoinHorizontal(lipgloss.Top, vimBtn, "   ", importBtn, "   ", cancelBtn)
	return lipgloss.JoinVertical(lipgloss.Left, title, "", desc, "", inputBox, "", actionRow)
}

var mcpImportPeerSources = []struct{ key, desc string }{
	{"codex", "Codex (~/.codex/config.toml)"},
	{"claude", "Claude Code (~/.claude.json)"},
}

func (m *mcpManagerModel) renderImportPeerModal() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Import from existing configs")
	var lines []string
	lines = append(lines, title, "", lipgloss.NewStyle().Foreground(colorMuted).Render("Import servers you already configured elsewhere:"), "")
	for i, src := range mcpImportPeerSources {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(colorTextSoft)
		if i == m.importPeer {
			prefix = "▶ "
			style = lipgloss.NewStyle().Foreground(colorText).Bold(true)
		}
		lines = append(lines, style.Render(prefix+src.desc))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Render("Enter import · Esc cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *mcpManagerModel) renderDeleteModal() string {
	name := m.currentName()
	title := lipgloss.NewStyle().Bold(true).Foreground(colorError).Render("Delete MCP server")
	desc := fmt.Sprintf("Remove %q and all agent bindings?", name)
	deleteBtn := pmCompactPrimaryBtnStyle.Copy().MarginRight(0).Render("Confirm Delete")
	cancelBtn := pmCompactBtnStyle.Copy().MarginRight(0).Render("Cancel")
	actionRow := lipgloss.JoinHorizontal(lipgloss.Top, deleteBtn, "   ", cancelBtn)
	return lipgloss.JoinVertical(lipgloss.Left, title, "", desc, "", actionRow)
}

func (m *mcpManagerModel) renderTestDetailModal() string {
	name := m.currentName()
	result := m.tests[name]
	title := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Test result · " + name)
	if result == nil {
		return lipgloss.JoinVertical(lipgloss.Left, title, "", "No test data. Esc to close.")
	}
	lines := []string{title, ""}
	if result.Err != "" {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorError).Bold(true).Render(fmt.Sprintf("✕ %s failed", result.Stage)),
			"",
			"Error: "+result.Err,
		)
	} else {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("✓ Connection & initialize OK"),
			lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render(fmt.Sprintf("✓ tools/list: %d tool(s)", result.ToolsCount)),
			"",
		)
		for _, tool := range result.ToolNames {
			lines = append(lines, "  • "+lipgloss.NewStyle().Foreground(colorTextSoft).Render(tool))
		}
	}
	lines = append(lines, "", pmCompactBtnStyle.Copy().MarginRight(0).Render("Close"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
