package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type compactFormRowOptions struct {
	Label      string
	Value      string
	Width      int
	Focused    bool
	ReadOnly   bool
	Required   bool
	Cursor     int
	ShowCursor bool
}

func renderCompactFormRow(opts compactFormRowOptions) string {
	width := max(1, opts.Width)
	value := truncateDisplay(opts.Value, width-2)
	if opts.ShowCursor {
		cursor := clampIndexInclusive(opts.Cursor, len([]rune(opts.Value)))
		r := []rune(opts.Value)
		value = truncateDisplay(string(r[:cursor])+"█"+string(r[cursor:]), width-2)
	}

	inputStyle := pmCompactInputStyle.Copy().Width(width)
	if opts.Focused {
		inputStyle = pmCompactFocusedInputStyle.Copy().Width(width)
	}
	if opts.ReadOnly {
		inputStyle = pmCompactReadOnlyInputStyle.Copy().Width(width)
		if opts.Focused {
			inputStyle = pmCompactFocusedInputStyle.Copy().Width(width)
		}
	}

	labelStyle := pmLabelStyle
	if opts.Focused {
		labelStyle = pmFocusedLabelStyle
	}
	label := opts.Label
	if opts.Required {
		label = "* " + label
	}
	divider := lipgloss.NewStyle().Foreground(colorBorder).Render("│")

	return lipgloss.JoinHorizontal(lipgloss.Center,
		labelStyle.Render(label),
		divider,
		inputStyle.Render(value),
	)
}

type selectModalOptions struct {
	Width         int
	Height        int
	Title         string
	Options       []string
	SelectedValue string
	Cursor        int
	EmptyText     string
	Help          string
}

func renderSelectModalOverlay(opts selectModalOptions) string {
	modalInnerWidth := 42
	panelRow := func(content string) string {
		return lipgloss.NewStyle().Background(colorPanelBg).Width(modalInnerWidth).Render(content)
	}
	help := opts.Help
	if strings.TrimSpace(help) == "" {
		help = "[Enter] Select  [Esc] Cancel"
	}
	empty := opts.EmptyText
	if strings.TrimSpace(empty) == "" {
		empty = "no options available"
	}

	lines := []string{
		panelRow(lipgloss.NewStyle().Bold(true).Render(opts.Title)),
		panelRow(""),
	}
	if len(opts.Options) == 0 {
		lines = append(lines, panelRow(lipgloss.NewStyle().Foreground(colorMuted).Render("  "+empty)))
	} else {
		cursor := clampIndex(opts.Cursor, len(opts.Options))
		for i, option := range opts.Options {
			prefix := "   "
			style := pmItemStyle
			if i == cursor {
				prefix = " ➤ "
				style = pmSelectedItemStyle
			}
			check := "   "
			if option == strings.TrimSpace(opts.SelectedValue) {
				check = "✓  "
			}
			lines = append(lines, panelRow(style.Render(prefix+check+option)))
		}
	}
	lines = append(lines, panelRow(""), panelRow(lipgloss.NewStyle().Foreground(colorMuted).Render(help)))
	modalBox := pmModalStyle.Width(48).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.Place(opts.Width, opts.Height, lipgloss.Center, lipgloss.Center, modalBox)
}
