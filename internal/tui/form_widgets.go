package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type compactFormRowOptions struct {
	Label       string
	Value       string
	Placeholder string
	Badge       string
	Width       int
	Focused     bool
	ReadOnly    bool
	Required    bool
	Cursor      int
	ShowCursor  bool
	InputView   string
}

func renderCompactFormRow(opts compactFormRowOptions) string {
	width := max(1, opts.Width)
	value := opts.Value
	isPlaceholder := false
	var inputStyle lipgloss.Style

	if opts.InputView != "" {
		value = opts.InputView
		inputStyle = lipgloss.NewStyle().Padding(0, 1).Width(width)
	} else {
		if value == "" && opts.Placeholder != "" {
			isPlaceholder = true
			if opts.ShowCursor {
				value = "█ " + opts.Placeholder
			} else {
				value = opts.Placeholder
			}
			value = truncateDisplay(value, width-2)
		} else if opts.ShowCursor {
			cursor := clampIndexInclusive(opts.Cursor, len([]rune(opts.Value)))
			r := []rune(opts.Value)
			value = truncateDisplay(string(r[:cursor])+"█"+string(r[cursor:]), width-2)
		} else {
			value = truncateDisplay(opts.Value, width-2)
		}

		inputStyle = pmCompactInputStyle.Copy().Width(width)
		if isPlaceholder {
			inputStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Padding(0, 1).Width(width)
			if opts.Focused {
				inputStyle = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1).Width(width)
			}
		} else if opts.Focused {
			if opts.ReadOnly {
				inputStyle = pmCompactInputStyle.Copy().Foreground(colorFocus).Bold(true).Width(width)
			} else {
				inputStyle = pmCompactInputStyle.Copy().Foreground(colorText).Bold(true).Width(width)
			}
		} else if opts.ReadOnly {
			inputStyle = pmCompactReadOnlyInputStyle.Copy().Width(width)
		}
	}

	labelStyle := pmLabelStyle
	if opts.Focused {
		labelStyle = pmFocusedLabelStyle.Copy().Bold(true)
	}
	label := opts.Label
	if opts.Required {
		reqMark := lipgloss.NewStyle().Foreground(colorError).Render("*")
		label = reqMark + " " + label
	}

	var divider string
	if opts.Focused {
		divider = lipgloss.NewStyle().Foreground(colorFocus).Bold(true).Render("▌")
	} else {
		divider = lipgloss.NewStyle().Foreground(colorBorder).Render("│")
	}

	return lipgloss.JoinHorizontal(lipgloss.Center,
		labelStyle.Render(label),
		divider,
		inputStyle.Render(value),
	)
}

func renderFormSectionHeader(title string, width int) string {
	width = max(10, width)
	titleStyled := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
	titleW := lipgloss.Width(titleStyled)
	lineW := max(2, width-titleW-4)
	bar := lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", lineW))
	lead := lipgloss.NewStyle().Foreground(colorBorder).Render("── ")
	return lead + titleStyled + " " + bar
}

type selectModalOptions struct {
	Width         int
	Height        int
	Title         string
	Options       []string
	SelectedValue string
	IsSelected    func(string) bool
	Cursor        int
	EmptyText     string
	Help          string
	AnchorX       int
	AnchorY       int
	Background    string
}

type selectModalLayout struct {
	X            int
	Y            int
	W            int
	H            int
	OptionStartY int
}

func renderSelectModalOverlay(opts selectModalOptions) (string, selectModalLayout) {
	isAnchored := opts.AnchorX > 0 || opts.AnchorY > 0
	modalInnerWidth := 38
	if isAnchored {
		modalInnerWidth = 42
	}
	if opts.Width > 0 && modalInnerWidth > opts.Width-8 {
		modalInnerWidth = max(20, opts.Width-8)
	}
	panelRow := func(content string) string {
		return lipgloss.NewStyle().Width(modalInnerWidth).Render(content)
	}
	help := opts.Help
	if strings.TrimSpace(help) == "" {
		help = "[Enter] Select  [Esc] Cancel"
	}
	empty := opts.EmptyText
	if strings.TrimSpace(empty) == "" {
		empty = "no options available"
	}

	var lines []string
	var modalBox string
	var optionStartYRel int

	if isAnchored {
		if opts.Title != "" {
			lines = append(lines, panelRow(lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(opts.Title)))
			optionStartYRel = 2 // 1 border + 1 title
		} else {
			optionStartYRel = 1 // 1 border
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
				selected := option == strings.TrimSpace(opts.SelectedValue)
				if opts.IsSelected != nil {
					selected = opts.IsSelected(option)
				}
				if selected {
					check = "✓  "
				}
				label := option
				if label == "" {
					label = "(unset)"
				}
				lines = append(lines, panelRow(style.Render(prefix+check+label)))
			}
		}
		div := lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", modalInnerWidth))
		lines = append(lines, panelRow(div), panelRow(lipgloss.NewStyle().Foreground(colorDim).Render(help)))

		modalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorFocus).
			Background(colorPanelBg).
			Padding(0, 1).
			Width(modalInnerWidth + 4).
			Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	} else {
		optionStartYRel = 4 // 1 border + 1 padding + 1 title + 1 blank
		lines = append(lines,
			panelRow(lipgloss.NewStyle().Bold(true).Render(opts.Title)),
			panelRow(""),
		)
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
				selected := option == strings.TrimSpace(opts.SelectedValue)
				if opts.IsSelected != nil {
					selected = opts.IsSelected(option)
				}
				if selected {
					check = "✓  "
				}
				label := option
				if label == "" {
					label = "(unset)"
				}
				lines = append(lines, panelRow(style.Render(prefix+check+label)))
			}
		}
		lines = append(lines, panelRow(""), panelRow(lipgloss.NewStyle().Foreground(colorMuted).Render(help)))
		modalBox = pmModalStyle.Width(modalInnerWidth + 6).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	layout := selectModalLayout{W: lipgloss.Width(modalBox), H: lipgloss.Height(modalBox)}
	if isAnchored {
		layout.X = opts.AnchorX
		if opts.Width > 0 && layout.X+layout.W > opts.Width-1 {
			layout.X = max(0, opts.Width-layout.W-1)
		}
		layout.Y = opts.AnchorY
		if opts.Height > 0 && layout.Y+layout.H > opts.Height-1 {
			layout.Y = max(0, opts.Height-layout.H-1)
		}
	} else {
		layout.X = (opts.Width - layout.W) / 2
		layout.Y = (opts.Height - layout.H) / 2
	}
	layout.OptionStartY = layout.Y + optionStartYRel

	if opts.Background != "" {
		return overlayBox(opts.Background, modalBox, layout.X, layout.Y), layout
	}
	return lipgloss.Place(opts.Width, opts.Height, lipgloss.Center, lipgloss.Center, modalBox), layout
}

// spliceAnsiLine splices overlay onto bgLine starting at column x,
// respecting ANSI formatting and wide characters.
func spliceAnsiLine(bgLine, overlay string, x int) string {
	if x < 0 {
		x = 0
	}
	overlayW := lipgloss.Width(overlay)
	bgW := lipgloss.Width(bgLine)

	left := ""
	if x > 0 {
		if x <= bgW {
			left = ansi.Cut(bgLine, 0, x)
		} else {
			left = bgLine + strings.Repeat(" ", x-bgW)
		}
	}

	right := ""
	endX := x + overlayW
	if endX < bgW {
		right = ansi.Cut(bgLine, endX, bgW)
	}

	return left + "\x1b[0m" + overlay + "\x1b[0m" + right
}

// overlayBox overlays a multi-line box string onto a multi-line background string at (x, y).
func overlayBox(bg, box string, x, y int) string {
	if bg == "" {
		return box
	}
	if box == "" {
		return bg
	}
	bgLines := strings.Split(bg, "\n")
	boxLines := strings.Split(box, "\n")

	for i, bLine := range boxLines {
		targetY := y + i
		if targetY < 0 {
			continue
		}
		for targetY >= len(bgLines) {
			bgLines = append(bgLines, "")
		}
		bgLines[targetY] = spliceAnsiLine(bgLines[targetY], bLine, x)
	}
	return strings.Join(bgLines, "\n")
}

// EditTextWithKey provides a unified, reusable keyboard handler for text input buffers,
// handling cursor movement, home/end, backspace, delete, and character insertion.
func EditTextWithKey(val string, cursor int, key string) (newVal string, newCursor int, handled bool) {
	r := []rune(val)
	cursor = clampIndexInclusive(cursor, len(r))
	switch key {
	case "left":
		if cursor > 0 {
			cursor--
		}
		return val, cursor, true
	case "right":
		if cursor < len(r) {
			cursor++
		}
		return val, cursor, true
	case "home", "ctrl+a":
		return val, 0, true
	case "end", "ctrl+e":
		return val, len(r), true
	case "backspace":
		if cursor > 0 && cursor <= len(r) {
			res := string(append(r[:cursor-1], r[cursor:]...))
			return res, cursor - 1, true
		}
		return val, cursor, true
	case "delete":
		if cursor >= 0 && cursor < len(r) {
			res := string(append(r[:cursor], r[cursor+1:]...))
			return res, cursor, true
		}
		return val, cursor, true
	default:
		if len(key) == 1 {
			ins := []rune(key)
			before := append([]rune{}, r[:cursor]...)
			after := append([]rune{}, r[cursor:]...)
			combined := append(before, append(ins, after...)...)
			return string(combined), cursor + len(ins), true
		}
	}
	return val, cursor, false
}

// InsertAtCursor inserts runes at a given cursor position into a string.
func InsertAtCursor(value string, cursor int, inserted []rune) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if len(inserted) == 0 {
		return value, cursor
	}
	updated := append(append(append([]rune{}, r[:cursor]...), inserted...), r[cursor:]...)
	return string(updated), cursor + len(inserted)
}

// DeleteBeforeCursor removes the rune preceding the cursor (backspace behavior).
func DeleteBeforeCursor(value string, cursor int) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if cursor == 0 {
		return value, cursor
	}
	updated := append(append([]rune{}, r[:cursor-1]...), r[cursor:]...)
	return string(updated), cursor - 1
}

// DeleteAtCursor removes the rune at the cursor (delete key behavior).
func DeleteAtCursor(value string, cursor int) (string, int) {
	r := []rune(value)
	cursor = clampIndexInclusive(cursor, len(r))
	if cursor >= len(r) {
		return value, cursor
	}
	updated := append(append([]rune{}, r[:cursor]...), r[cursor+1:]...)
	return string(updated), cursor
}

func renderCursorText(text string, cursor int) string {
	r := []rune(text)
	cursor = clampIndexInclusive(cursor, len(r))
	if cursor >= len(r) {
		return text + "█"
	}
	return string(r[:cursor]) + "█" + string(r[cursor+1:])
}

func filterPrintableRunes(runes []rune) []rune {
	out := make([]rune, 0, len(runes))
	for _, r := range runes {
		if r >= 32 || r == '\t' || r == '\n' {
			out = append(out, r)
		}
	}
	return out
}

func cursorLineColumn(r []rune, cursor int) (lineStart, lineEnd, col int) {
	cursor = clampIndexInclusive(cursor, len(r))
	lineStart = 0
	for i := cursor - 1; i >= 0; i-- {
		if r[i] == '\n' {
			lineStart = i + 1
			break
		}
	}
	lineEnd = len(r)
	for i := cursor; i < len(r); i++ {
		if r[i] == '\n' {
			lineEnd = i
			break
		}
	}
	col = cursor - lineStart
	return lineStart, lineEnd, col
}

func clampIndex(value, max int) int {
	if max <= 0 {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value >= max {
		return max - 1
	}
	return value
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

func renderSegmentedPills(options []string, selected string, focused bool, width int) string {
	selected = strings.TrimSpace(selected)
	var renderedPills []string
	for _, opt := range options {
		isSel := strings.EqualFold(opt, selected)
		if isSel {
			if focused {
				renderedPills = append(renderedPills, lipgloss.NewStyle().
					Foreground(colorText).
					Background(colorFocus).
					Bold(true).
					Padding(0, 1).
					Render(opt))
			} else {
				renderedPills = append(renderedPills, lipgloss.NewStyle().
					Foreground(colorFocus).
					Bold(true).
					Render("["+opt+"]"))
			}
		} else {
			renderedPills = append(renderedPills, lipgloss.NewStyle().
				Foreground(colorMuted).
				Render(opt))
		}
	}
	content := strings.Join(renderedPills, "  ")
	if focused && width > lipgloss.Width(content)+8 {
		hint := lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("  (←/→)")
		content += hint
	}
	return content
}

func renderInlineStepper(value string, options []string, focused bool, width int) string {
	val := strings.TrimSpace(value)
	displayVal := val
	if displayVal == "" {
		displayVal = "(unset)"
	}
	var leftArrow, rightArrow, valStyled string
	if focused {
		leftArrow = lipgloss.NewStyle().Foreground(colorFocus).Bold(true).Render("◀ ")
		rightArrow = lipgloss.NewStyle().Foreground(colorFocus).Bold(true).Render(" ▶")
		valStyled = lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(displayVal)
	} else {
		leftArrow = lipgloss.NewStyle().Foreground(colorMuted).Render("‹ ")
		rightArrow = lipgloss.NewStyle().Foreground(colorMuted).Render(" ›")
		valStyled = lipgloss.NewStyle().Foreground(colorTextSoft).Render(displayVal)
	}
	content := leftArrow + valStyled + rightArrow
	if focused && width > lipgloss.Width(content)+8 {
		hint := lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("  (←/→)")
		content += hint
	}
	return content
}
