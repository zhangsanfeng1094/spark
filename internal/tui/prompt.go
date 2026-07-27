package tui

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// 定义样式
var (
	// 标题样式：加粗，下划线，前景色
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			MarginBottom(1).
			Render

	// 普通选项样式
	itemStyle = lipgloss.NewStyle().
			PaddingLeft(2).
			Render

	// 选中选项样式：高亮背景，加粗
	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(lipgloss.Color("#FF6B6B")).
				Background(lipgloss.Color("#3C3C3C")).
				Bold(true).
				Render

	// 帮助提示样式：变灰，斜体
	helpStyle = lipgloss.NewStyle().
			Faint(true).
			Italic(true).
			MarginTop(1).
			Render

	// 输入框样式
	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7D56F4")).
				Bold(true).
				Render

	inputValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#4B4B4B")).
			Padding(0, 1).
			Render
)

// SelectOne 选择一个选项
func SelectOne(title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options")
	}
	m := &selectModel{
		title:   title,
		options: options,
	}
	// 启用鼠标支持
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithMouseCellMotion())
	out, err := p.Run()
	if err != nil {
		return "", err
	}
	result := out.(*selectModel)
	if result.canceled {
		return "", fmt.Errorf("aborted")
	}
	return result.choice, nil
}

// InputWithDefault 带默认值的输入
func InputWithDefault(prompt, def string) (string, error) {
	m := &inputModel{
		title: prompt,
		value: def,
	}
	// 启用鼠标支持 (虽然输入框主要靠键盘，但开启鼠标可以防止意外阻塞)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithMouseCellMotion())
	out, err := p.Run()
	if err != nil {
		return "", err
	}
	result := out.(*inputModel)
	if result.canceled {
		return "", fmt.Errorf("aborted")
	}
	line := strings.TrimSpace(result.value)
	if line == "" && def != "" {
		return def, nil
	}
	return line, nil
}

// Confirm is a simple Yes/No dialog. Prefer ConfirmDetails when the action
// needs context (paths, why, side effects).
func Confirm(prompt string, def bool) (bool, error) {
	return ConfirmDetails(ConfirmRequest{
		Title:          prompt,
		ConfirmLabel:   "Yes",
		CancelLabel:    "No",
		DefaultConfirm: def,
	})
}

// ConfirmRequest describes a richer confirmation dialog.
type ConfirmRequest struct {
	Title          string
	// Summary is a short lead line under the title (what is about to happen).
	Summary string
	// Details are bullet-style lines (paths, models, notes).
	Details []string
	// Footnote is a muted trailing note (e.g. backup location).
	Footnote string
	// ConfirmLabel / CancelLabel default to "Yes" / "No".
	ConfirmLabel   string
	CancelLabel    string
	DefaultConfirm bool
}

// ConfirmDetails shows a structured confirm dialog and returns whether the
// user chose the confirm action.
func ConfirmDetails(req ConfirmRequest) (bool, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Confirm"
	}
	confirmLabel := strings.TrimSpace(req.ConfirmLabel)
	if confirmLabel == "" {
		confirmLabel = "Yes"
	}
	cancelLabel := strings.TrimSpace(req.CancelLabel)
	if cancelLabel == "" {
		cancelLabel = "No"
	}
	cursor := 1
	if req.DefaultConfirm {
		cursor = 0
	}
	m := &confirmModel{
		title:        title,
		summary:      strings.TrimSpace(req.Summary),
		details:      append([]string(nil), req.Details...),
		footnote:     strings.TrimSpace(req.Footnote),
		options:      []string{confirmLabel, cancelLabel},
		cursor:       cursor,
		confirmLabel: confirmLabel,
	}
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithMouseCellMotion())
	out, err := p.Run()
	if err != nil {
		return false, err
	}
	result := out.(*confirmModel)
	if result.canceled {
		return false, fmt.Errorf("aborted")
	}
	return result.choice == result.confirmLabel, nil
}

// InputCSV 输入CSV
func InputCSV(prompt string, defaults []string) ([]string, error) {
	def := strings.Join(defaults, ",")
	line, err := InputWithDefault(prompt+" (comma separated)", def)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(line, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// --- Models ---

type selectModel struct {
	title    string
	options  []string
	cursor   int
	choice   string
	canceled bool
}

func (m *selectModel) Init() tea.Cmd { return nil }

func (m *selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.canceled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.choice = m.options[m.cursor]
			return m, tea.Quit
		}
	case tea.MouseMsg:
		// 部分 Windows 终端更稳定地上报 MouseLeft，而不是 MouseRelease。
		if isPrimaryClick(msg.Type) {
			// 计算点击的行数（减去标题和空行的偏移量）
			// View 渲染顺序: Title(1行) + 空行(1行) + Options...
			clickY := msg.Y - 2

			if clickY >= 0 && clickY < len(m.options) {
				m.cursor = clickY
				// 点击即选中并退出
				m.choice = m.options[m.cursor]
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *selectModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle(m.title) + "\n\n")
	for i, o := range m.options {
		if i == m.cursor {
			b.WriteString(selectedItemStyle("→ "+o) + "\n")
		} else {
			b.WriteString(itemStyle("  "+o) + "\n")
		}
	}
	b.WriteString("\n" + helpStyle("↑/↓ move • Enter select • q/esc cancel • 🖱️ click to select"))
	return b.String()
}

type inputModel struct {
	title    string
	value    string
	canceled bool
}

func (m *inputModel) Init() tea.Cmd { return nil }

func (m *inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.canceled = true
			return m, tea.Quit
		case "enter":
			return m, tea.Quit
		case "backspace":
			if len(m.value) > 0 {
				// 处理 UTF-8 字符删除
				runes := []rune(m.value)
				m.value = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				m.value += string(msg.Runes)
			}
		}
	}
	return m, nil
}

func (m *inputModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle(m.title) + "\n\n")
	// 输入框样式
	displayValue := m.value
	if displayValue == "" {
		displayValue = " " // 占位，保持高度
	}
	b.WriteString(inputPromptStyle("> ") + inputValueStyle(displayValue) + "\n")
	b.WriteString("\n" + helpStyle("Type to edit • Enter confirm • esc cancel"))
	return b.String()
}

type confirmModel struct {
	title        string
	summary      string
	details      []string
	footnote     string
	options      []string
	cursor       int
	choice       string
	confirmLabel string
	canceled     bool
}

func (m *confirmModel) Init() tea.Cmd { return nil }

func (m *confirmModel) bodyLineCount() int {
	n := 0
	if m.summary != "" {
		n++
	}
	n += len(m.details)
	if m.footnote != "" {
		n++
	}
	// Blank line before options when any body content is present.
	if n > 0 {
		n++
	}
	return n
}

func (m *confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.canceled = true
			return m, tea.Quit
		case "left", "h", "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "right", "l", "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "y", "Y":
			if m.confirmLabel != "" {
				m.choice = m.confirmLabel
				return m, tea.Quit
			}
		case "n", "N":
			if len(m.options) > 1 {
				m.choice = m.options[len(m.options)-1]
				return m, tea.Quit
			}
		case "enter":
			m.choice = m.options[m.cursor]
			return m, tea.Quit
		}
	case tea.MouseMsg:
		if isPrimaryClick(msg.Type) {
			// Title (+ optional margin) occupies the first visual block; body
			// content follows, then the option list.
			clickY := msg.Y - 2 - m.bodyLineCount()
			if clickY >= 0 && clickY < len(m.options) {
				m.cursor = clickY
				m.choice = m.options[m.cursor]
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func isPrimaryClick(t tea.MouseEventType) bool {
	if runtime.GOOS == "windows" {
		return t == tea.MouseLeft
	}
	return t == tea.MouseRelease
}

func (m *confirmModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle(m.title) + "\n")
	if m.summary != "" {
		b.WriteString("\n" + confirmSummaryStyle(m.summary))
	}
	for _, line := range m.details {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b.WriteString("\n" + confirmDetailStyle("  • "+line))
	}
	if m.footnote != "" {
		b.WriteString("\n" + confirmFootnoteStyle(m.footnote))
	}
	if m.summary != "" || len(m.details) > 0 || m.footnote != "" {
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for i, o := range m.options {
		if i == m.cursor {
			b.WriteString(selectedItemStyle("→ "+o) + "\n")
		} else {
			b.WriteString(itemStyle("  "+o) + "\n")
		}
	}
	b.WriteString("\n" + helpStyle("←/→ or j/k move • Enter confirm • y/n quick • esc cancel"))
	return b.String()
}

var (
	confirmSummaryStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E8E8E8")).
				Bold(true).
				Render
	confirmDetailStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#B8B8C8")).
				Render
	confirmFootnoteStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7A7A8A")).
				Italic(true).
				MarginTop(1).
				Render
)
