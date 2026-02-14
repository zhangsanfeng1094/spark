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
	_ = m.applyFieldsToProfile(cur)
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
	m.fields = []pmField{
		{label: "Profile Name", value: name},
		{label: "Provider Type", value: detectProviderType(p), readOnly: true},
		{label: "OpenAI Base URL", value: p.OpenAIBaseURL},
		{label: "OpenAI API Key", value: p.OpenAIAPIKey, masked: true},
		{label: "OpenAI Org", value: p.OpenAIOrg},
		{label: "OpenAI Project", value: p.OpenAIProject},
		{label: "Anthropic Base URL", value: p.AnthropicBaseURL},
		{label: "Anthropic Token", value: p.AnthropicAuthToken, masked: true},
		{label: "Models (CSV)", value: strings.Join(p.Models, ", ")},
		{label: "Default Model", value: p.DefaultModel},
	}
	for i := range m.fields {
		m.fields[i].cursor = len([]rune(m.fields[i].value))
	}
	if m.focusField >= len(m.fields) {
		m.focusField = len(m.fields) - 1
	}
}

func detectProviderType(p *config.Profile) string {
	base := strings.ToLower(strings.TrimSpace(p.OpenAIBaseURL))
	anth := strings.TrimSpace(p.AnthropicBaseURL)
	switch {
	case strings.Contains(base, "localhost:11434") || strings.Contains(base, "127.0.0.1:11434"):
		return "Ollama"
	case anth != "":
		return "Anthropic"
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
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
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
