package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

func (m *pmModel) setStatus(kind pmStatusKind, status string) uint64 {
	m.statusSeq++
	m.statusKind = kind
	m.status = status
	return m.statusSeq
}

func (m *pmModel) setUserStatus(kind pmStatusKind, status string) {
	m.runningTestSeq = 0
	m.lastTestSummary = ""
	m.lastTestOK = false
	m.setStatus(kind, status)
}

func (m *pmModel) switchProfile(next int) {
	if next < 0 || next >= len(m.profileNames) {
		return
	}
	cur := m.currentProfileName()
	if err := m.applyFieldsToProfile(cur); err != nil {
		m.setUserStatus(pmStatusWarning, "Warning: failed to apply current fields: "+err.Error())
	}
	m.selected = next
	m.loadSelectedProfileFields()
}

func (m *pmModel) refreshNames() {
	m.profileNames = m.profileNames[:0]
	for name := range m.cfg.Profiles {
		m.profileNames = append(m.profileNames, name)
	}
	sort.Strings(m.profileNames)
	if len(m.profileNames) == 0 {
		m.cfg.Profiles["default"] = &config.Profile{OpenAIBaseURL: "https://api.openai.com/v1"}
		m.profileNames = append(m.profileNames, "default")
	}
}

func (m *pmModel) selectByName(name string) {
	for i, n := range m.profileNames {
		if n == name {
			m.selected = i
			return
		}
	}
	m.selected = 0
}

func (m *pmModel) currentProfileName() string {
	if len(m.profileNames) == 0 {
		return ""
	}
	if m.selected >= len(m.profileNames) {
		m.selected = len(m.profileNames) - 1
	}
	return m.profileNames[m.selected]
}

func (m *pmModel) loadSelectedProfileFields() {
	name := m.currentProfileName()
	p := m.cfg.Profiles[name]
	if p == nil {
		p = &config.Profile{OpenAIBaseURL: "https://api.openai.com/v1"}
		m.cfg.Profiles[name] = p
	}
	m.modelsDraft = config.NormalizeModels(p.Models)
	m.defaultModel = strings.TrimSpace(p.DefaultModel)
	if m.defaultModel == "" && len(m.modelsDraft) > 0 {
		m.defaultModel = m.modelsDraft[0]
	}
	if m.defaultModel != "" {
		found := false
		for _, mdl := range m.modelsDraft {
			if mdl == m.defaultModel {
				found = true
				break
			}
		}
		if !found {
			m.modelsDraft = append([]string{m.defaultModel}, m.modelsDraft...)
			m.modelsDraft = config.NormalizeModels(m.modelsDraft)
		}
	}

	apiKeyPlaceholder := "sk-..."
	if p.EffectiveAPIKey() == "" {
		prov := strings.TrimSpace(p.AuthProvider)
		if prov == "" && detectProviderType(p) == "Command Code" {
			prov = "commandcode"
		}
		if prov != "" {
			apiKeyPlaceholder = fmt.Sprintf("[managed: %s]", prov)
		}
	}

	m.fields = []pmField{
		{label: "Profile Name", value: name, required: true, placeholder: "e.g. work-profile"},
		{label: "Provider Type", value: detectProviderType(p), readOnly: true},
		{label: "Base URL", value: p.EffectiveEndpoint(), required: true, placeholder: "https://api.openai.com/v1"},
		{label: "API Key", value: p.EffectiveAPIKey(), masked: true, placeholder: apiKeyPlaceholder},
		{label: "API Type", value: displayOpenAIAPIType(p.OpenAIAPIType), readOnly: true},
		{label: "Models URL", value: p.ModelListURL, placeholder: "https://... (optional)"},
		{label: "Models", value: formatModelsSummary(m.modelsDraft, m.defaultModel), readOnly: true},
		{label: "Thinking Mode", value: thinkingModeValue(p), readOnly: true},
		{label: "Thinking Effort", value: thinkingEffortValue(p), readOnly: true},
		{label: "Thinking Budget", value: thinkingBudgetValue(p), placeholder: "e.g. 4096 (optional)"},
	}
	for i := range m.fields {
		f := &m.fields[i]
		f.cursor = len([]rune(f.value))
		if !f.readOnly {
			focused := m.focusArea == pmFocusFields && i == m.focusField
			f.input = newFieldTextInput(f.placeholder, f.value, f.masked, focused, m.inputWidth)
		}
	}
	if m.focusField >= len(m.fields) {
		m.focusField = len(m.fields) - 1
	}
}

func newFieldTextInput(placeholder, value string, masked, focused bool, width int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(colorFocus)
	ti.CharLimit = 0
	ti.Width = max(10, width-2)
	if masked {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	ti.SetValue(value)
	if focused {
		ti.Focus()
		ti.CursorEnd()
	} else {
		ti.Blur()
	}
	return ti
}

func (m *pmModel) updateFocus() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.fields {
		f := &m.fields[i]
		if !f.readOnly {
			if m.focusArea == pmFocusFields && i == m.focusField {
				cmd := f.input.Focus()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else {
				f.input.Blur()
			}
		}
	}
	return tea.Batch(cmds...)
}

func (m *pmModel) syncInputs() {
	for i := range m.fields {
		f := &m.fields[i]
		if !f.readOnly {
			if f.input.Value() != f.value {
				f.input.SetValue(f.value)
				if f.cursor >= 0 && f.cursor <= len([]rune(f.value)) {
					f.input.SetCursor(f.cursor)
				} else {
					f.input.CursorEnd()
				}
			}
			f.input.Width = max(10, m.inputWidth-2)
			f.input.Placeholder = f.placeholder
			if m.focusArea == pmFocusFields && i == m.focusField {
				f.input.Focus()
			} else {
				f.input.Blur()
			}
		}
	}
}

func thinkingModeValue(p *config.Profile) string {
	if p == nil || p.Thinking == nil || strings.TrimSpace(p.Thinking.Mode) == "" {
		return "client"
	}
	return p.Thinking.Mode
}

func thinkingEffortValue(p *config.Profile) string {
	if p == nil || p.Thinking == nil {
		return ""
	}
	return p.Thinking.Effort
}

func thinkingBudgetValue(p *config.Profile) string {
	if p == nil || p.Thinking == nil || p.Thinking.BudgetTokens == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p.Thinking.BudgetTokens)
}

func formatModelsSummary(models []string, defaultModel string) string {
	if len(models) == 0 {
		return "0 models"
	}
	if defaultModel == "" {
		return fmt.Sprintf("%d models", len(models))
	}
	return fmt.Sprintf("%d models · %s", len(models), truncateSummaryValue(defaultModel, 18))
}

func (m *pmModel) syncModelFieldViews() {
	if pmFieldModelsCSV < len(m.fields) {
		m.fields[pmFieldModelsCSV].value = formatModelsSummary(m.modelsDraft, m.defaultModel)
		m.fields[pmFieldModelsCSV].cursor = len([]rune(m.fields[pmFieldModelsCSV].value))
	}
}

func displayOpenAIAPIType(v string) string {
	canonical := config.CanonicalizeOpenAIAPITypes(v)
	if canonical == "" {
		return config.DefaultOpenAIAPIType
	}
	return canonical
}

func (m *pmModel) visibleAPITypeOptions() []string {
	provider := ""
	if pmFieldProviderType < len(m.fields) {
		provider = strings.TrimSpace(m.fields[pmFieldProviderType].value)
	}
	switch provider {
	case "Anthropic":
		return []string{config.OpenAIAPITypeAnthropicMessages}
	case "Gemini":
		return []string{config.OpenAIAPITypeGeminiGenerateContent}
	default:
		return []string{
			config.OpenAIAPITypeResponses,
			config.OpenAIAPITypeChatCompletions,
		}
	}
}

func detectProviderType(p *config.Profile) string {
	if p == nil {
		return "OpenAI Compatible"
	}
	if p.AuthProvider == "commandcode" || strings.Contains(strings.ToLower(p.EffectiveEndpoint()), ":3050") || strings.Contains(strings.ToLower(p.EffectiveEndpoint()), "commandcode") {
		return "Command Code"
	}
	if strings.TrimSpace(p.AnthropicBaseURL) != "" || p.AuthProvider == "claude" {
		return "Anthropic"
	}
	base := strings.ToLower(strings.TrimSpace(p.EffectiveEndpoint()))
	switch {
	case strings.Contains(base, "localhost:11434") || strings.Contains(base, "127.0.0.1:11434"):
		return "Ollama"
	case p.AuthProvider == "gemini" || strings.Contains(base, "generativelanguage.googleapis.com") || strings.Contains(base, "ai.google.dev"):
		return "Gemini"
	case base == "https://api.openai.com/v1" || base == "" || p.AuthProvider == "codex":
		return "OpenAI"
	default:
		return "OpenAI Compatible"
	}
}

func (m *pmModel) profileTemplate(kind string) *config.Profile {
	switch kind {
	case "anthropic":
		return &config.Profile{
			Endpoint: "https://api.anthropic.com",
			Protocol: config.ProtocolAnthropic,
			Credential: config.CredentialConfig{
				Mode:    config.CredentialModeAuto,
				AuthRef: "claude:default",
			},
			OpenAIBaseURL:    "https://api.anthropic.com",
			OpenAIAPIType:    config.OpenAIAPITypeAnthropicMessages,
			AnthropicBaseURL: "https://api.anthropic.com",
			Models:           []string{"claude-sonnet-4-20250514"},
			DefaultModel:     "claude-sonnet-4-20250514",
			AuthProvider:     "claude",
		}
	case "commandcode":
		return &config.Profile{
			Endpoint: "https://api.commandcode.ai/provider/v1",
			Protocol: config.ProtocolOpenAIChat,
			Credential: config.CredentialConfig{
				Mode:    config.CredentialModeAuto,
				AuthRef: "commandcode:default",
			},
			OpenAIBaseURL: "https://api.commandcode.ai/provider/v1",
			OpenAIAPIType: config.OpenAIAPITypeChatCompletions,
			AuthProvider:  "commandcode",
			ModelListURL:  "https://api.commandcode.ai/provider/v1/models",
			Models:        []string{"claude-sonnet-4-6", "claude-3-7-sonnet", "gpt-4o"},
			DefaultModel:  "claude-sonnet-4-6",
		}
	case "ollama":
		return &config.Profile{
			Endpoint:      "http://localhost:11434/v1",
			Protocol:      config.ProtocolOpenAIChat,
			OpenAIBaseURL: "http://localhost:11434/v1",
		}
	case "gemini":
		return &config.Profile{
			Endpoint: "https://generativelanguage.googleapis.com/v1beta",
			Protocol: config.ProtocolGemini,
			Credential: config.CredentialConfig{
				Mode:    config.CredentialModeAuto,
				AuthRef: "gemini:default",
			},
			OpenAIBaseURL: "https://generativelanguage.googleapis.com/v1beta",
			OpenAIAPIType: config.OpenAIAPITypeGeminiGenerateContent,
			Models:        []string{"gemini-2.5-flash"},
			DefaultModel:  "gemini-2.5-flash",
			AuthProvider:  "gemini",
		}
	default:
		return &config.Profile{
			Endpoint:      "https://api.openai.com/v1",
			Protocol:      config.ProtocolOpenAIChat,
			OpenAIBaseURL: "https://api.openai.com/v1",
			OpenAIAPIType: config.DefaultOpenAIAPIType,
		}
	}
}

func pmSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer(" ", "-", "(", "", ")", "", "/", "-", "_", "-")
	s = replacer.Replace(s)
	s = strings.Trim(s, "-")
	if s == "" {
		s = "profile"
	}
	return s
}

func parseCSVModels(csv string) []string {
	return config.ParseModelsCSV(csv)
}

func truncateSummaryValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func (m *pmModel) uniqueProfileName(base string) string {
	if _, ok := m.cfg.Profiles[base]; !ok {
		return base
	}
	for i := 2; i < 1000; i++ {
		name := fmt.Sprintf("%s-%d", base, i)
		if _, ok := m.cfg.Profiles[name]; !ok {
			return name
		}
	}
	return fmt.Sprintf("%s-%d", base, len(m.cfg.Profiles)+1)
}
