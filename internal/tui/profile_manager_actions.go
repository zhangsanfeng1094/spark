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
	m.modalKind = pmModalKindAddProfile
}

func (m *pmModel) openAPITypeModal() {
	m.modalOpen = true
	m.modalKind = pmModalKindOpenAIAPIType
	m.modalCursor = 0
	m.apiTypeSelected = map[string]bool{}
	current := config.ParseOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value)
	for _, apiType := range current {
		m.apiTypeSelected[apiType] = true
	}
	if len(m.apiTypeSelected) == 0 {
		m.apiTypeSelected[config.OpenAIAPITypeAuto] = true
	}
	for i, opt := range m.apiTypeOptions {
		if m.apiTypeSelected[opt] {
			m.modalCursor = i
			break
		}
	}
}

func (m *pmModel) openModelsModal() {
	m.modalOpen = true
	m.modalKind = pmModalKindModels
	m.modalCursor = 0
	m.modelItems = append([]string{}, m.modelsDraft...)
	m.modelEditMode = false
	m.modelEditIndex = -1
	m.modelEditBuffer = ""
	m.modelModalNote = ""
	if len(m.modelItems) > 0 {
		for i, mdl := range m.modelItems {
			if mdl == m.defaultModel {
				m.modalCursor = i
				break
			}
		}
	}
}

func (m *pmModel) createProfileFromModal() {
	opt := m.providerOptions[m.modalCursor]
	name := m.uniqueProfileName(pmSlug(opt.name))
	m.cfg.Profiles[name] = m.profileTemplate(opt.kind)
	m.refreshNames()
	m.selectByName(name)
	m.loadSelectedProfileFields()
	m.modalOpen = false
	m.modalKind = pmModalKindNone
	m.dirty = true
	m.status = fmt.Sprintf("Created '%s'. Edit fields and Save.", name)
}

func (m *pmModel) toggleAPITypeOptionAtCursor() {
	if m.modalCursor < 0 || m.modalCursor >= len(m.apiTypeOptions) {
		return
	}
	selected := m.apiTypeOptions[m.modalCursor]
	if selected == config.OpenAIAPITypeAuto {
		m.apiTypeSelected = map[string]bool{
			config.OpenAIAPITypeAuto: true,
		}
		return
	}
	delete(m.apiTypeSelected, config.OpenAIAPITypeAuto)
	if m.apiTypeSelected[selected] {
		delete(m.apiTypeSelected, selected)
	} else {
		m.apiTypeSelected[selected] = true
	}
	if len(m.apiTypeSelected) == 0 {
		m.apiTypeSelected[config.OpenAIAPITypeAuto] = true
	}
}

func (m *pmModel) startModelAdd() {
	m.modelEditMode = true
	m.modelEditIndex = -1
	m.modelEditBuffer = ""
	m.modelModalNote = "Input model id and press Enter to add."
}

func (m *pmModel) startModelEdit() {
	if m.modalCursor < 0 || m.modalCursor >= len(m.modelItems) {
		m.modelModalNote = "No model selected."
		return
	}
	m.modelEditMode = true
	m.modelEditIndex = m.modalCursor
	m.modelEditBuffer = m.modelItems[m.modalCursor]
	m.modelModalNote = "Editing selected model. Press Enter to save."
}

func (m *pmModel) confirmModelEdit() {
	value := strings.TrimSpace(m.modelEditBuffer)
	if value == "" {
		m.modelModalNote = "Model id cannot be empty."
		return
	}
	if m.modelEditIndex >= 0 && m.modelEditIndex < len(m.modelItems) {
		m.modelItems[m.modelEditIndex] = value
	} else {
		m.modelItems = append(m.modelItems, value)
		m.modalCursor = len(m.modelItems) - 1
	}
	m.modelItems = config.NormalizeModels(m.modelItems)
	if m.defaultModel == "" && len(m.modelItems) > 0 {
		m.defaultModel = m.modelItems[0]
	}
	if len(m.modelItems) > 0 && m.modalCursor >= len(m.modelItems) {
		m.modalCursor = len(m.modelItems) - 1
	}
	m.modelEditMode = false
	m.modelEditIndex = -1
	m.modelEditBuffer = ""
	m.modelModalNote = "Model list updated."
}

func (m *pmModel) deleteModelAtCursor() {
	if m.modalCursor < 0 || m.modalCursor >= len(m.modelItems) {
		m.modelModalNote = "No model selected."
		return
	}
	removed := m.modelItems[m.modalCursor]
	m.modelItems = append(m.modelItems[:m.modalCursor], m.modelItems[m.modalCursor+1:]...)
	if m.modalCursor >= len(m.modelItems) && len(m.modelItems) > 0 {
		m.modalCursor = len(m.modelItems) - 1
	}
	if len(m.modelItems) == 0 {
		m.modalCursor = 0
		m.defaultModel = ""
	} else if removed == m.defaultModel {
		m.defaultModel = m.modelItems[0]
	}
	m.modelModalNote = "Deleted selected model."
}

func (m *pmModel) setDefaultModelAtCursor() {
	if m.modalCursor < 0 || m.modalCursor >= len(m.modelItems) {
		m.modelModalNote = "No model selected."
		return
	}
	m.defaultModel = m.modelItems[m.modalCursor]
	m.modelModalNote = "Default model set."
}

func (m *pmModel) confirmModelsSelection() {
	m.modelsDraft = config.NormalizeModels(m.modelItems)
	if m.defaultModel != "" {
		found := false
		for _, mdl := range m.modelsDraft {
			if mdl == m.defaultModel {
				found = true
				break
			}
		}
		if !found {
			m.defaultModel = ""
		}
	}
	if m.defaultModel == "" && len(m.modelsDraft) > 0 {
		m.defaultModel = m.modelsDraft[0]
	}
	m.syncModelFieldViews()
	m.modalOpen = false
	m.modalKind = pmModalKindNone
	m.modelEditMode = false
	m.modelEditIndex = -1
	m.modelEditBuffer = ""
	m.modelModalNote = ""
	m.dirty = true
	m.status = "Models updated. Save to persist."
}

// fetchModelsResultMsg is sent when model list fetching completes
type fetchModelsResultMsg struct {
	models []string
	err    error
}

func (m *pmModel) fetchModelsFromAPI() tea.Cmd {
	m.modelModalNote = "Fetching models from API..."
	profileCopy := &config.Profile{
		OpenAIBaseURL: strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value),
		OpenAIAPIKey:  strings.TrimSpace(m.fields[pmFieldOpenAIAPIKey].value),
		OpenAIOrg:     "",
		OpenAIProject: "",
	}
	name := m.currentProfileName()
	if p := m.cfg.Profiles[name]; p != nil {
		profileCopy.OpenAIOrg = strings.TrimSpace(p.OpenAIOrg)
		profileCopy.OpenAIProject = strings.TrimSpace(p.OpenAIProject)
	}
	return func() tea.Msg {
		models, err := FetchOpenAIModels(profileCopy)
		return fetchModelsResultMsg{models: models, err: err}
	}
}

func (m *pmModel) handleFetchModelsResult(msg fetchModelsResultMsg) {
	if m.modalKind != pmModalKindModels {
		return
	}
	if msg.err != nil {
		m.modelModalNote = "Fetch failed: " + msg.err.Error()
		return
	}
	if len(msg.models) == 0 {
		m.modelModalNote = "No models returned by API."
		return
	}
	merged := append([]string{}, m.modelItems...)
	merged = append(merged, msg.models...)
	m.modelItems = config.NormalizeModels(merged)
	if m.defaultModel == "" && len(m.modelItems) > 0 {
		m.defaultModel = m.modelItems[0]
	}
	if len(m.modelItems) > 0 && m.modalCursor >= len(m.modelItems) {
		m.modalCursor = len(m.modelItems) - 1
	}
	m.modelModalNote = fmt.Sprintf("Fetched %d models.", len(msg.models))
}

func (m *pmModel) confirmAPITypeSelection() {
	selected := make([]string, 0, 2)
	if m.apiTypeSelected[config.OpenAIAPITypeResponses] {
		selected = append(selected, config.OpenAIAPITypeResponses)
	}
	if m.apiTypeSelected[config.OpenAIAPITypeChatCompletions] {
		selected = append(selected, config.OpenAIAPITypeChatCompletions)
	}
	value := config.OpenAIAPITypeAuto
	if len(selected) == 1 {
		value = selected[0]
	} else if len(selected) == 2 {
		value = strings.Join(selected, ",")
	}
	m.fields[pmFieldOpenAIAPIType].value = value
	m.fields[pmFieldOpenAIAPIType].cursor = len([]rune(value))
	m.modalOpen = false
	m.modalKind = pmModalKindNone
	m.dirty = true
	m.status = "OpenAI API Type updated. Save to persist."
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
	detectedAPIType := ""
	if p := m.cfg.Profiles[oldName]; p != nil && config.CanonicalizeOpenAIAPITypes(p.OpenAIAPIType) == config.OpenAIAPITypeAuto {
		detected, err := DetectOpenAIAPIType(p, strings.TrimSpace(m.defaultModel))
		if err == nil && detected != "" {
			p.OpenAIAPIType = detected
			detectedAPIType = detected
			m.fields[pmFieldOpenAIAPIType].value = detected
			m.fields[pmFieldOpenAIAPIType].cursor = len([]rune(detected))
		}
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
	if detectedAPIType != "" {
		m.status = fmt.Sprintf("Configuration saved successfully. Detected OpenAI API Type: %s.", detectedAPIType)
		return
	}
	m.status = "Configuration saved successfully."
}

func (m *pmModel) applyFieldsToProfile(name string) error {
	p := m.cfg.Profiles[name]
	if p == nil {
		return fmt.Errorf("profile not found")
	}
	p.OpenAIBaseURL = strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value)
	p.OpenAIAPIKey = strings.TrimSpace(m.fields[pmFieldOpenAIAPIKey].value)
	p.OpenAIAPIType = config.CanonicalizeOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value)
	p.Models = append([]string{}, m.modelsDraft...)
	p.DefaultModel = strings.TrimSpace(m.defaultModel)
	return nil
}

// testResultMsg is sent when a connection test completes
type testResultMsg struct {
	result TestResult
}

func (m *pmModel) testConnection() tea.Cmd {
	logPath := appendModelConnectionTestLogf(
		"ui trigger profile=%q base_url=%q api_type=%q default_model=%q models=%q",
		m.currentProfileName(),
		strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value),
		config.CanonicalizeOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value),
		strings.TrimSpace(m.defaultModel),
		strings.Join(m.modelsDraft, ","),
	)
	m.status = "Testing connection..."
	if strings.TrimSpace(logPath) != "" {
		m.status = fmt.Sprintf("Testing connection... (log: %s)", logPath)
	}

	name := m.currentProfileName()
	if _, ok := m.cfg.Profiles[name]; !ok {
		m.status = "Profile not found"
		return nil
	}

	model := strings.TrimSpace(m.defaultModel)
	if model == "" {
		if len(m.modelsDraft) > 0 {
			model = m.modelsDraft[0]
		}
	}

	profileCopy := &config.Profile{
		OpenAIBaseURL: strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value),
		OpenAIAPIKey:  strings.TrimSpace(m.fields[pmFieldOpenAIAPIKey].value),
		OpenAIAPIType: config.CanonicalizeOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value),
		Models:        append([]string{}, m.modelsDraft...),
		DefaultModel:  model,
	}

	return func() tea.Msg {
		result := TestModelConnection(profileCopy, model)
		return testResultMsg{result: result}
	}
}

func (m *pmModel) handleTestResult(msg testResultMsg) {
	r := msg.result
	logPath := strings.TrimSpace(r.LogPath)
	if r.Success {
		m.status = fmt.Sprintf("✓ Test passed · %s · %dms", r.Message, r.Latency.Milliseconds())
	} else {
		m.status = fmt.Sprintf("✗ Test failed · %s", r.Message)
	}
	if logPath != "" {
		m.status += "\nlog: " + logPath
	}
}
