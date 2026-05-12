package tui

import (
	"fmt"
	"sort"
	"strings"

	"spark/internal/config"
)

func (m *pmModel) switchProfile(next int) {
	if next < 0 || next >= len(m.profileNames) {
		return
	}
	cur := m.currentProfileName()
	if err := m.applyFieldsToProfile(cur); err != nil {
		m.status = "Warning: failed to apply current fields: " + err.Error()
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

	m.fields = []pmField{
		{label: "Profile Name", value: name},
		{label: "Provider Type", value: detectProviderType(p), readOnly: true},
		{label: "Base URL", value: p.OpenAIBaseURL},
		{label: "API Key", value: p.OpenAIAPIKey, masked: true},
		{label: "API Type", value: displayOpenAIAPIType(p.OpenAIAPIType), readOnly: true},
		{label: "Models", value: formatModelsSummary(m.modelsDraft, m.defaultModel), readOnly: true},
	}
	for i := range m.fields {
		m.fields[i].cursor = len([]rune(m.fields[i].value))
	}
	if m.focusField >= len(m.fields) {
		m.focusField = len(m.fields) - 1
	}
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
		return config.OpenAIAPITypeAuto
	}
	return canonical
}

func detectProviderType(p *config.Profile) string {
	base := strings.ToLower(strings.TrimSpace(p.OpenAIBaseURL))
	switch {
	case strings.Contains(base, "localhost:11434") || strings.Contains(base, "127.0.0.1:11434"):
		return "Ollama"
	case base == "https://api.openai.com/v1" || base == "":
		return "OpenAI"
	default:
		return "OpenAI Compatible"
	}
}

func (m *pmModel) profileTemplate(kind string) *config.Profile {
	switch kind {
	case "anthropic":
		return &config.Profile{
			OpenAIBaseURL:    "https://api.openai.com/v1",
			AnthropicBaseURL: "https://api.anthropic.com",
		}
	case "ollama":
		return &config.Profile{OpenAIBaseURL: "http://localhost:11434/v1"}
	default:
		return &config.Profile{OpenAIBaseURL: "https://api.openai.com/v1"}
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
