package tui

import (
	"fmt"
	"os"
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
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithMouseCellMotion())
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
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithMouseCellMotion())
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

// Confirm 确认对话框
func Confirm(prompt string, def bool) (bool, error) {
	choices := []string{"Yes", "No"}
	cursor := 1
	if def {
		cursor = 0
	}
	m := &confirmModel{
		title:   prompt,
		options: choices,
		cursor:  cursor,
	}
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithMouseCellMotion())
	out, err := p.Run()
	if err != nil {
		return false, err
	}
	result := out.(*confirmModel)
	if result.canceled {
		return false, fmt.Errorf("aborted")
	}
	return result.choice == "Yes", nil
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
		// 鼠标点击处理
		if msg.Type == tea.MouseRelease {
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
	title    string
	options  []string
	cursor   int
	choice   string
	canceled bool
}

func (m *confirmModel) Init() tea.Cmd { return nil }

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
		case "enter":
			m.choice = m.options[m.cursor]
			return m, tea.Quit
		}
	case tea.MouseMsg:
		if msg.Type == tea.MouseRelease {
			clickY := msg.Y - 2 // 标题偏移
			if clickY >= 0 && clickY < len(m.options) {
				m.cursor = clickY
				m.choice = m.options[m.cursor]
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *confirmModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle(m.title) + "\n\n")
	for i, o := range m.options {
		if i == m.cursor {
			b.WriteString(selectedItemStyle("→ "+o) + "\n")
		} else {
			b.WriteString(itemStyle("  "+o) + "\n")
		}
	}
	b.WriteString("\n" + helpStyle("←/→ move • Enter confirm • q/esc cancel • 🖱️ click"))
	return b.String()
}
