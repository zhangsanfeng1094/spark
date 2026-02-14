package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
)

func (m *pmModel) runAction(action int) tea.Cmd {
	switch action {
	case pmActAdd:
		m.openAddModal()
		return nil
	case pmActDel:
		m.deleteSelectedProfile()
		return nil
	case pmActTest:
		return m.testConnection()
	case pmActSave:
		m.save()
		return nil
	}
	return nil
}

func (m *pmModel) openAddModal() {
	m.modalOpen = true
	m.modalCursor = 0
}

func (m *pmModel) createProfileFromModal() {
	opt := m.providerOptions[m.modalCursor]
	name := m.uniqueProfileName(pmSlug(opt.name))
	m.cfg.Profiles[name] = m.profileTemplate(opt.kind)
	m.refreshNames()
	m.selectByName(name)
	m.loadSelectedProfileFields()
	m.modalOpen = false
	m.dirty = true
	m.status = fmt.Sprintf("Created '%s'. Edit fields and Save.", name)
}

func (m *pmModel) deleteSelectedProfile() {
	if len(m.profileNames) <= 1 {
		m.status = "Cannot delete the last profile."
		return
	}
	name := m.currentProfileName()
	delete(m.cfg.Profiles, name)

	if m.cfg.DefaultProfile == name {
		m.refreshNames()
		m.cfg.DefaultProfile = m.profileNames[0]
	} else {
		m.refreshNames()
	}

	for _, ic := range m.cfg.Integrations {
		if ic != nil && ic.Profile == name {
			ic.Profile = m.cfg.DefaultProfile
		}
	}

	if m.selected >= len(m.profileNames) {
		m.selected = len(m.profileNames) - 1
	}
	m.loadSelectedProfileFields()
	m.dirty = true
	m.status = fmt.Sprintf("Deleted '%s'.", name)
}

func (m *pmModel) save() {
	oldName := m.currentProfileName()
	if err := m.applyFieldsToProfile(oldName); err != nil {
		m.status = "Error: " + err.Error()
		return
	}

	newName := strings.TrimSpace(m.fields[pmFieldProfileName].value)
	if newName == "" {
		m.status = "Profile Name cannot be empty."
		return
	}

	if newName != oldName {
		if _, exists := m.cfg.Profiles[newName]; exists {
			m.status = "Profile name already exists."
			return
		}
		m.cfg.Profiles[newName] = m.cfg.Profiles[oldName]
		delete(m.cfg.Profiles, oldName)

		if m.cfg.DefaultProfile == oldName {
			m.cfg.DefaultProfile = newName
		}
		for _, ic := range m.cfg.Integrations {
			if ic != nil && ic.Profile == oldName {
				ic.Profile = newName
			}
		}
	}

	if err := config.Save(m.cfg); err != nil {
		m.status = "Save failed: " + err.Error()
		return
	}

	m.refreshNames()
	m.selectByName(newName)
	m.loadSelectedProfileFields()
	m.dirty = false
	m.status = "Configuration saved successfully."
}

func (m *pmModel) applyFieldsToProfile(name string) error {
	p := m.cfg.Profiles[name]
	if p == nil {
		return fmt.Errorf("profile not found")
	}
	p.OpenAIBaseURL = strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value)
	p.OpenAIAPIKey = strings.TrimSpace(m.fields[pmFieldOpenAIAPIKey].value)
	p.Models = parseCSVModels(m.fields[pmFieldModelsCSV].value)
	p.DefaultModel = strings.TrimSpace(m.fields[pmFieldDefaultModel].value)
	return nil
}

// testResultMsg is sent when a connection test completes
type testResultMsg struct {
	result TestResult
}

func (m *pmModel) testConnection() tea.Cmd {
	m.status = "Testing connection..."

	name := m.currentProfileName()
	if _, ok := m.cfg.Profiles[name]; !ok {
		m.status = "Profile not found"
		return nil
	}

	model := strings.TrimSpace(m.fields[pmFieldDefaultModel].value)
	if model == "" {
		models := parseCSVModels(m.fields[pmFieldModelsCSV].value)
		if len(models) > 0 {
			model = models[0]
		}
	}

	profileCopy := &config.Profile{
		OpenAIBaseURL: strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value),
		OpenAIAPIKey:  strings.TrimSpace(m.fields[pmFieldOpenAIAPIKey].value),
		Models:        parseCSVModels(m.fields[pmFieldModelsCSV].value),
		DefaultModel:  model,
	}

	return func() tea.Msg {
		result := TestModelConnection(profileCopy, model)
		return testResultMsg{result: result}
	}
}

func (m *pmModel) handleTestResult(msg testResultMsg) {
	r := msg.result
	if r.Success {
		m.status = fmt.Sprintf("✓ Test passed: %s (%dms)", r.Message, r.Latency.Milliseconds())
	} else {
		m.status = fmt.Sprintf("✗ Test failed: %s", r.Message)
	}
}
