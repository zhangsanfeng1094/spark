package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
	"spark/internal/probe"
)

func (m *pmModel) runAction(action int) tea.Cmd {
	switch action {
	case pmActAdd:
		m.openAddModal()
		return nil
	case pmActCopy:
		m.copySelectedProfile()
		return nil
	case pmActDel:
		if len(m.profileNames) <= 1 {
			m.setUserStatus(pmStatusWarning, "Cannot delete the last profile.")
			return nil
		}
		m.confirmDelete = true
		name := m.currentProfileName()
		m.setUserStatus(pmStatusWarning, fmt.Sprintf("Delete '%s'? Press Y to confirm or N to cancel.", name))
		return nil
	case pmActDefault:
		m.setCurrentProfileDefault()
		return nil
	case pmActTest:
		return m.testConnection()
	case pmActSave:
		m.save()
		return nil
	}
	return nil
}

func (m *pmModel) setCurrentProfileDefault() {
	name := m.currentProfileName()
	if m.dirty {
		if err := m.applyFieldsToProfile(name); err != nil {
			m.setUserStatus(pmStatusError, "Default failed: "+err.Error())
			return
		}
	}
	if err := m.cfg.SetDefaultProfile(name); err != nil {
		m.setUserStatus(pmStatusError, "Default failed: "+err.Error())
		return
	}
	if err := config.Save(m.cfg); err != nil {
		m.dirty = true
		m.setUserStatus(pmStatusError, "Default save failed: "+err.Error())
		return
	}
	m.dirty = false
	m.setUserStatus(pmStatusSuccess, "Set '"+name+"' as default.")
}

func (m *pmModel) openAddModal() {
	m.modalOpen = true
	m.modalCursor = 0
	m.modalKind = pmModalKindAddProfile
}

func (m *pmModel) openProviderTypeModal() {
	m.modalOpen = true
	m.modalCursor = 0
	m.modalKind = pmModalKindProviderType
	current := ""
	if pmFieldProviderType < len(m.fields) {
		current = strings.TrimSpace(m.fields[pmFieldProviderType].value)
	}
	for i, opt := range m.providerOptions {
		if opt.name == current {
			m.modalCursor = i
			break
		}
	}
}

func (m *pmModel) openAPITypeModal() {
	m.modalOpen = true
	m.modalKind = pmModalKindOpenAIAPIType
	m.modalCursor = 0
	m.apiTypeSelected = map[string]bool{}
	options := m.visibleAPITypeOptions()
	current := config.ParseOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value)
	for _, apiType := range current {
		for _, opt := range options {
			if apiType == opt {
				m.apiTypeSelected[apiType] = true
				break
			}
		}
	}
	if len(m.apiTypeSelected) == 0 && len(options) > 0 {
		m.apiTypeSelected[options[0]] = true
	}
	for i, opt := range options {
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
	m.modelSearchQuery = ""
	m.modelSearchInput = newFieldTextInput("type to filter models...", "", false, true, 48)
	m.modelSearchInput.Prompt = "Search: "
	m.modelSearchFocused = true
	m.modelModalScroll = 0
	m.modelModalVisibleCount = 0
	if len(m.modelItems) > 0 {
		for i, mdl := range m.modelItems {
			if mdl == m.defaultModel {
				m.modalCursor = i
				break
			}
		}
	}
	m.syncModelsModalScroll()
}

func (m *pmModel) openThinkingModal(kind int, field int) {
	m.modalOpen = true
	m.modalKind = kind
	m.modalCursor = 0
	current := strings.TrimSpace(m.fields[field].value)
	for i, option := range m.thinkingModalOptions() {
		if option == current {
			m.modalCursor = i
			break
		}
	}
}

func (m *pmModel) thinkingModalOptions() []string {
	if m.modalKind == pmModalKindThinkingMode {
		return []string{"client", "auto", "force", "off"}
	}
	return []string{"", "minimal", "low", "medium", "high", "xhigh", "max"}
}

func (m *pmModel) confirmThinkingSelection() {
	options := m.thinkingModalOptions()
	if m.modalCursor < 0 || m.modalCursor >= len(options) {
		return
	}
	field := pmFieldThinkingMode
	if m.modalKind == pmModalKindThinkingEffort {
		field = pmFieldThinkingEffort
	}
	m.fields[field].value = options[m.modalCursor]
	m.fields[field].cursor = len([]rune(options[m.modalCursor]))
	m.modalOpen = false
	m.modalKind = pmModalKindNone
	m.dirty = true
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
	m.setUserStatus(pmStatusInfo, fmt.Sprintf("Created '%s'. Edit fields, then save.", name))
}

func (m *pmModel) applyProviderOption(opt pmProviderOption) {
	template := m.profileTemplate(opt.kind)
	apiKey := ""
	if pmFieldOpenAIAPIKey < len(m.fields) {
		apiKey = m.fields[pmFieldOpenAIAPIKey].value
	}
	m.fields[pmFieldProviderType].value = opt.name
	m.fields[pmFieldProviderType].cursor = len([]rune(opt.name))
	m.fields[pmFieldOpenAIAPIKey].value = apiKey
	m.fields[pmFieldOpenAIAPIKey].cursor = len([]rune(apiKey))
	apiType := displayOpenAIAPIType(template.OpenAIAPIType)
	m.fields[pmFieldOpenAIAPIType].value = apiType
	m.fields[pmFieldOpenAIAPIType].cursor = len([]rune(apiType))
	if strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value) == "" && template.EffectiveEndpoint() != "" {
		m.fields[pmFieldOpenAIBaseURL].value = template.EffectiveEndpoint()
		m.fields[pmFieldOpenAIBaseURL].cursor = len([]rune(template.EffectiveEndpoint()))
	}
	m.modelsDraft = config.NormalizeModels(template.Models)
	m.defaultModel = strings.TrimSpace(template.DefaultModel)
	if m.defaultModel == "" && len(m.modelsDraft) > 0 {
		m.defaultModel = m.modelsDraft[0]
	}
	m.syncModelFieldViews()
	m.dirty = true
	m.setUserStatus(pmStatusInfo, fmt.Sprintf("Provider type set to %s. Save to persist.", opt.name))
}

func (m *pmModel) confirmProviderTypeSelection() {
	if m.modalCursor < 0 || m.modalCursor >= len(m.providerOptions) {
		return
	}
	m.applyProviderOption(m.providerOptions[m.modalCursor])
	m.modalOpen = false
	m.modalKind = pmModalKindNone
}

func (m *pmModel) cycleProviderType(forward bool) {
	if len(m.providerOptions) == 0 {
		return
	}
	curName := m.fields[pmFieldProviderType].value
	curIdx := 0
	for i, opt := range m.providerOptions {
		if strings.EqualFold(opt.name, curName) {
			curIdx = i
			break
		}
	}
	if forward {
		curIdx = (curIdx + 1) % len(m.providerOptions)
	} else {
		curIdx = (curIdx - 1 + len(m.providerOptions)) % len(m.providerOptions)
	}
	m.applyProviderOption(m.providerOptions[curIdx])
}

func (m *pmModel) cycleThinkingMode(forward bool) {
	modes := []string{"client", "auto", "force", "off"}
	cur := strings.TrimSpace(m.fields[pmFieldThinkingMode].value)
	curIdx := 0
	for i, v := range modes {
		if v == cur {
			curIdx = i
			break
		}
	}
	if forward {
		curIdx = (curIdx + 1) % len(modes)
	} else {
		curIdx = (curIdx - 1 + len(modes)) % len(modes)
	}
	m.fields[pmFieldThinkingMode].value = modes[curIdx]
	m.fields[pmFieldThinkingMode].cursor = len([]rune(modes[curIdx]))
	m.dirty = true
	m.setUserStatus(pmStatusInfo, fmt.Sprintf("Thinking mode: %s. Save to persist.", modes[curIdx]))
}

func (m *pmModel) cycleThinkingEffort(forward bool) {
	efforts := []string{"", "minimal", "low", "medium", "high", "xhigh", "max"}
	cur := strings.TrimSpace(m.fields[pmFieldThinkingEffort].value)
	curIdx := 0
	for i, v := range efforts {
		if v == cur {
			curIdx = i
			break
		}
	}
	if forward {
		curIdx = (curIdx + 1) % len(efforts)
	} else {
		curIdx = (curIdx - 1 + len(efforts)) % len(efforts)
	}
	m.fields[pmFieldThinkingEffort].value = efforts[curIdx]
	m.fields[pmFieldThinkingEffort].cursor = len([]rune(efforts[curIdx]))
	m.dirty = true
	label := efforts[curIdx]
	if label == "" {
		label = "default"
	}
	m.setUserStatus(pmStatusInfo, fmt.Sprintf("Thinking effort: %s. Save to persist.", label))
}

func (m *pmModel) cycleAPIType(forward bool) {
	options := m.visibleAPITypeOptions()
	if len(options) == 0 {
		return
	}
	cur := strings.TrimSpace(m.fields[pmFieldOpenAIAPIType].value)
	curIdx := 0
	for i, v := range options {
		if strings.EqualFold(v, cur) {
			curIdx = i
			break
		}
	}
	if forward {
		curIdx = (curIdx + 1) % len(options)
	} else {
		curIdx = (curIdx - 1 + len(options)) % len(options)
	}
	m.fields[pmFieldOpenAIAPIType].value = options[curIdx]
	m.fields[pmFieldOpenAIAPIType].cursor = len([]rune(options[curIdx]))
	m.apiTypeSelected = map[string]bool{options[curIdx]: true}
	m.dirty = true
	m.setUserStatus(pmStatusInfo, fmt.Sprintf("API type: %s. Save to persist.", options[curIdx]))
}

func (m *pmModel) cycleSelectField(forward bool) bool {
	if m.focusArea != pmFocusFields {
		return false
	}
	switch m.focusField {
	case pmFieldProviderType:
		m.cycleProviderType(forward)
		return true
	case pmFieldOpenAIAPIType:
		m.cycleAPIType(forward)
		return true
	case pmFieldThinkingMode:
		m.cycleThinkingMode(forward)
		return true
	case pmFieldThinkingEffort:
		m.cycleThinkingEffort(forward)
		return true
	default:
		return false
	}
}

func (m *pmModel) toggleAPITypeOptionAtCursor() {
	options := m.visibleAPITypeOptions()
	if m.modalCursor < 0 || m.modalCursor >= len(options) {
		return
	}
	selected := options[m.modalCursor]
	if m.apiTypeSelected[selected] {
		delete(m.apiTypeSelected, selected)
	} else {
		m.apiTypeSelected[selected] = true
	}
	if len(m.apiTypeSelected) == 0 && len(options) > 0 {
		m.apiTypeSelected[options[0]] = true
	}
}

func (m *pmModel) startModelAdd() {
	m.modelEditMode = true
	m.modelEditIndex = -1
	m.modelEditBuffer = ""
	m.modelEditInput = newFieldTextInput("e.g. gpt-4o, claude-3-7-sonnet", "", false, true, 48)
	m.modelEditInput.Prompt = "Input: "
	m.modelEditInput.Focus()
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
	m.modelEditInput = newFieldTextInput("model id", m.modelEditBuffer, false, true, 48)
	m.modelEditInput.Prompt = "Input: "
	m.modelEditInput.Focus()
	m.modelEditInput.CursorEnd()
	m.modelModalNote = "Editing selected model. Press Enter to save."
}

func (m *pmModel) confirmModelEdit() {
	if m.modelEditInput.Value() != "" || m.modelEditBuffer == "" {
		m.modelEditBuffer = m.modelEditInput.Value()
	}
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
	m.syncModelsModalScroll()
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
	m.syncModelsModalScroll()
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
	m.setUserStatus(pmStatusInfo, "Models updated. Save to persist.")
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
		APIKey:        strings.TrimSpace(m.fields[pmFieldOpenAIAPIKey].value),
		OpenAIAPIType: config.CanonicalizeOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value),
		ModelListURL:  strings.TrimSpace(m.fields[pmFieldModelListURL].value),
		OpenAIOrg:     "",
		OpenAIProject: "",
	}
	if config.SupportsOpenAIAPIType(profileCopy.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) {
		profileCopy.AnthropicBaseURL = profileCopy.OpenAIBaseURL
	}
	name := m.currentProfileName()
	var authProvider string
	if p := m.cfg.Profiles[name]; p != nil {
		profileCopy.OpenAIOrg = strings.TrimSpace(p.OpenAIOrg)
		profileCopy.OpenAIProject = strings.TrimSpace(p.OpenAIProject)
	}
	switch m.fields[pmFieldProviderType].value {
	case "Command Code":
		authProvider = "commandcode"
	case "Claude", "Anthropic":
		authProvider = "claude"
	case "Codex":
		authProvider = "codex"
	case "Gemini":
		authProvider = "gemini"
	default:
		if strings.Contains(profileCopy.OpenAIBaseURL, ":3050") || strings.Contains(profileCopy.OpenAIBaseURL, "commandcode") {
			authProvider = "commandcode"
		}
	}
	profileCopy.AuthProvider = authProvider
	profileCopy.Endpoint = profileCopy.OpenAIBaseURL
	profileCopy.Protocol = config.NormalizeAPIProtocol(profileCopy.OpenAIAPIType)
	profileCopy.Credential.APIKey = profileCopy.APIKey
	if profileCopy.Credential.AuthRef == "" && authProvider != "" {
		profileCopy.Credential.AuthRef = authProvider + ":default"
	}
	if profileCopy.Credential.Mode == "" {
		profileCopy.Credential.Mode = config.CredentialModeAuto
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
	m.syncModelsModalScroll()
	m.modelModalNote = fmt.Sprintf("Fetched %d models.", len(msg.models))
}

func (m *pmModel) confirmAPITypeSelection() {
	options := m.visibleAPITypeOptions()
	selected := make([]string, 0, len(options))
	for _, opt := range options {
		if m.apiTypeSelected[opt] {
			selected = append(selected, opt)
		}
	}
	value := config.DefaultOpenAIAPIType
	if len(selected) > 0 {
		value = config.CanonicalizeOpenAIAPITypes(strings.Join(selected, ","))
	}
	m.fields[pmFieldOpenAIAPIType].value = value
	m.fields[pmFieldOpenAIAPIType].cursor = len([]rune(value))
	m.modalOpen = false
	m.modalKind = pmModalKindNone
	m.dirty = true
	m.setUserStatus(pmStatusInfo, "API type updated. Save to persist.")
}

func (m *pmModel) deleteSelectedProfile() {
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
	if m.cfg.History.LastProfile == name {
		m.cfg.History.LastProfile = m.cfg.DefaultProfile
	}

	if m.selected >= len(m.profileNames) {
		m.selected = len(m.profileNames) - 1
	}
	m.loadSelectedProfileFields()
	m.dirty = true
	m.confirmDelete = false
	m.setUserStatus(pmStatusInfo, fmt.Sprintf("Deleted '%s'.", name))
}

func (m *pmModel) handleConfirmDeleteKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.deleteSelectedProfile()
		return nil
	case "n", "esc":
		m.confirmDelete = false
		m.setUserStatus(pmStatusInfo, "Delete canceled.")
		return nil
	}
	return nil
}

func (m *pmModel) copySelectedProfile() {
	name := m.currentProfileName()
	if m.dirty {
		_ = m.applyFieldsToProfile(name)
	}
	profile, ok := m.cfg.Profiles[name]
	if !ok {
		m.setUserStatus(pmStatusError, "Profile not found.")
		return
	}

	// Generate a unique name for the copied profile
	newName := m.uniqueProfileName(name + "-copy")

	// Deep copy the profile including v2 and thinking config
	var thinkingCopy *config.ThinkingConfig
	if profile.Thinking != nil {
		tc := *profile.Thinking
		if profile.Thinking.BudgetTokens != nil {
			budget := *profile.Thinking.BudgetTokens
			tc.BudgetTokens = &budget
		}
		thinkingCopy = &tc
	}

	newProfile := &config.Profile{
		Endpoint:         profile.EffectiveEndpoint(),
		Protocol:         profile.EffectiveProtocol(),
		Credential:       profile.Credential,
		Thinking:         thinkingCopy,
		OpenAIBaseURL:    profile.EffectiveEndpoint(),
		APIKey:           profile.EffectiveAPIKey(),
		OpenAIAPIType:    profile.OpenAIAPIType,
		OpenAIOrg:        profile.OpenAIOrg,
		OpenAIProject:    profile.OpenAIProject,
		ModelListURL:     profile.ModelListURL,
		AnthropicBaseURL: profile.AnthropicBaseURL,
		Models:           append([]string{}, profile.Models...),
		DefaultModel:     profile.DefaultModel,
		AuthProvider:     profile.AuthProvider,
		AuthRef:          profile.EffectiveAuthRef(),
	}
	if newProfile.Credential.APIKey == "" {
		newProfile.Credential.APIKey = newProfile.APIKey
	}
	if newProfile.Credential.AuthRef == "" {
		newProfile.Credential.AuthRef = newProfile.AuthRef
	}
	if newProfile.Credential.Mode == "" {
		newProfile.Credential.Mode = newProfile.EffectiveCredentialMode()
	}

	m.cfg.Profiles[newName] = newProfile
	m.refreshNames()
	m.selectByName(newName)
	m.loadSelectedProfileFields()
	m.dirty = true
	m.setUserStatus(pmStatusInfo, fmt.Sprintf("Copied '%s' to '%s'.", name, newName))
}

func (m *pmModel) save() {
	oldName := m.currentProfileName()
	if err := m.applyFieldsToProfile(oldName); err != nil {
		m.setUserStatus(pmStatusError, "Error: "+err.Error())
		return
	}
	newName := strings.TrimSpace(m.fields[pmFieldProfileName].value)
	if newName == "" {
		m.setUserStatus(pmStatusWarning, "Profile Name cannot be empty.")
		return
	}
	if newName != oldName {
		if _, exists := m.cfg.Profiles[newName]; exists {
			m.setUserStatus(pmStatusWarning, "Profile name already exists.")
			return
		}
		m.cfg.Profiles[newName] = m.cfg.Profiles[oldName]
		delete(m.cfg.Profiles, oldName)
		if m.cfg.DefaultProfile == oldName {
			m.cfg.DefaultProfile = newName
		}
		if m.cfg.History.LastProfile == oldName {
			m.cfg.History.LastProfile = newName
		}
		for _, ic := range m.cfg.Integrations {
			if ic != nil && ic.Profile == oldName {
				ic.Profile = newName
			}
		}
	}
	if err := config.Save(m.cfg); err != nil {
		m.setUserStatus(pmStatusError, "Save failed: "+err.Error())
		return
	}
	m.refreshNames()
	m.selectByName(newName)
	m.loadSelectedProfileFields()
	m.dirty = false
	m.setUserStatus(pmStatusSuccess, "Saved profile "+newName+".")
}
func (m *pmModel) applyFieldsToProfile(name string) error {
	p := m.cfg.Profiles[name]
	if p == nil {
		return fmt.Errorf("profile not found")
	}
	baseURL := strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value)
	p.OpenAIBaseURL = baseURL
	p.Endpoint = baseURL
	p.APIKey = strings.TrimSpace(m.fields[pmFieldOpenAIAPIKey].value)
	p.OpenAIAPIKey = p.APIKey
	p.Credential.APIKey = p.APIKey
	p.AnthropicAuthToken = ""
	p.OpenAIAPIType = config.CanonicalizeOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value)
	p.Protocol = config.NormalizeAPIProtocol(p.OpenAIAPIType)
	p.ModelListURL = strings.TrimSpace(m.fields[pmFieldModelListURL].value)
	thinkingConfig, err := thinkingConfigFromFields(m.fields)
	if err != nil {
		return err
	}
	p.Thinking = thinkingConfig
	if config.SupportsOpenAIAPIType(p.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) {
		p.AnthropicBaseURL = p.OpenAIBaseURL
	} else {
		p.AnthropicBaseURL = ""
	}
	p.Models = append([]string{}, m.modelsDraft...)
	p.DefaultModel = strings.TrimSpace(m.defaultModel)
	switch m.fields[pmFieldProviderType].value {
	case "Command Code":
		p.AuthProvider = "commandcode"
	case "Claude", "Anthropic":
		p.AuthProvider = "claude"
	case "Codex":
		p.AuthProvider = "codex"
	case "Gemini":
		p.AuthProvider = "gemini"
	default:
		if strings.Contains(p.OpenAIBaseURL, ":3050") || strings.Contains(p.OpenAIBaseURL, "commandcode") {
			p.AuthProvider = "commandcode"
		} else {
			p.AuthProvider = ""
			p.AuthRef = ""
			p.Credential.AuthRef = ""
		}
	}
	if p.Credential.AuthRef == "" && p.AuthProvider != "" {
		p.Credential.AuthRef = p.AuthProvider + ":default"
	}
	if p.AuthRef == "" && p.AuthProvider != "" {
		p.AuthRef = p.Credential.AuthRef
	}
	if p.Credential.Mode == "" {
		p.Credential.Mode = config.CredentialModeAuto
	}
	return nil
}

func thinkingConfigFromFields(fields []pmField) (*config.ThinkingConfig, error) {
	if pmFieldThinkingBudget >= len(fields) {
		return nil, nil
	}
	mode := strings.ToLower(strings.TrimSpace(fields[pmFieldThinkingMode].value))
	if mode == "" || mode == "client" {
		return nil, nil
	}
	if mode != "auto" && mode != "force" && mode != "off" {
		return nil, fmt.Errorf("thinking mode must be client, auto, force, or off")
	}
	cfg := &config.ThinkingConfig{Mode: mode}
	if mode == "off" {
		return cfg, nil
	}
	cfg.Effort = strings.ToLower(strings.TrimSpace(fields[pmFieldThinkingEffort].value))
	if cfg.Effort != "" && !validThinkingEffort(cfg.Effort) {
		return nil, fmt.Errorf("thinking effort must be minimal, low, medium, high, xhigh, or max")
	}
	if raw := strings.TrimSpace(fields[pmFieldThinkingBudget].value); raw != "" {
		budget, err := strconv.Atoi(raw)
		if err != nil || budget <= 0 {
			return nil, fmt.Errorf("thinking budget must be a positive integer")
		}
		cfg.BudgetTokens = &budget
	}
	return cfg, nil
}

func validThinkingEffort(value string) bool {
	switch value {
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

// testResultMsg is sent when a connection test completes
type testResultMsg struct {
	statusSeq uint64
	result    probe.TestResult
}

func (m *pmModel) testConnection() tea.Cmd {
	m.lastTestSummary = ""
	m.lastTestOK = false
	logPath := probe.AppendModelConnectionTestLogf(
		"ui trigger profile=%q base_url=%q api_type=%q default_model=%q models=%q",
		m.currentProfileName(),
		strings.TrimSpace(m.fields[pmFieldOpenAIBaseURL].value),
		config.CanonicalizeOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value),
		strings.TrimSpace(m.defaultModel),
		strings.Join(m.modelsDraft, ","),
	)
	status := "Testing connection..."
	if strings.TrimSpace(logPath) != "" {
		status = fmt.Sprintf("Testing connection... (log: %s)", logPath)
	}
	statusSeq := m.setStatus(pmStatusTestRunning, status)
	m.runningTestSeq = statusSeq
	name := m.currentProfileName()
	if _, ok := m.cfg.Profiles[name]; !ok {
		m.setUserStatus(pmStatusError, "Profile not found")
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
		APIKey:        strings.TrimSpace(m.fields[pmFieldOpenAIAPIKey].value),
		OpenAIAPIType: config.CanonicalizeOpenAIAPITypes(m.fields[pmFieldOpenAIAPIType].value),
		ModelListURL:  strings.TrimSpace(m.fields[pmFieldModelListURL].value),
		Models:        append([]string{}, m.modelsDraft...),
		DefaultModel:  model,
	}
	if config.SupportsOpenAIAPIType(profileCopy.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) {
		profileCopy.AnthropicBaseURL = profileCopy.OpenAIBaseURL
	}
	var authProvider string
	if p := m.cfg.Profiles[name]; p != nil {
		profileCopy.OpenAIOrg = strings.TrimSpace(p.OpenAIOrg)
		profileCopy.OpenAIProject = strings.TrimSpace(p.OpenAIProject)
	}
	switch m.fields[pmFieldProviderType].value {
	case "Command Code":
		authProvider = "commandcode"
	case "Claude", "Anthropic":
		authProvider = "claude"
	case "Codex":
		authProvider = "codex"
	case "Gemini":
		authProvider = "gemini"
	default:
		if strings.Contains(profileCopy.OpenAIBaseURL, ":3050") || strings.Contains(profileCopy.OpenAIBaseURL, "commandcode") {
			authProvider = "commandcode"
		}
	}
	profileCopy.AuthProvider = authProvider
	profileCopy.Endpoint = profileCopy.OpenAIBaseURL
	profileCopy.Protocol = config.NormalizeAPIProtocol(profileCopy.OpenAIAPIType)
	profileCopy.Credential.APIKey = profileCopy.APIKey
	if profileCopy.Credential.AuthRef == "" && authProvider != "" {
		profileCopy.Credential.AuthRef = authProvider + ":default"
	}
	if profileCopy.Credential.Mode == "" {
		profileCopy.Credential.Mode = config.CredentialModeAuto
	}
	profileCopy.Thinking, _ = thinkingConfigFromFields(m.fields)
	return func() tea.Msg {
		result := probe.TestModelConnection(profileCopy, model)
		return testResultMsg{statusSeq: statusSeq, result: result}
	}
}
func (m *pmModel) handleTestResult(msg testResultMsg) {
	if msg.statusSeq == 0 || msg.statusSeq != m.runningTestSeq {
		return
	}
	m.runningTestSeq = 0
	r := msg.result
	logPath := strings.TrimSpace(r.LogPath)
	statusKind := pmStatusTestError
	status := fmt.Sprintf("✗ Test failed · %s", r.Message)
	if r.Success {
		statusKind = pmStatusTestSuccess
		status = fmt.Sprintf("✓ Connected · %s · %dms", r.Message, r.Latency.Milliseconds())
		m.lastTestSummary = fmt.Sprintf("Connected · %dms", r.Latency.Milliseconds())
		m.lastTestOK = true
	} else {
		m.lastTestSummary = "Connection failed"
		m.lastTestOK = false
	}
	if logPath != "" {
		status += "\nlog: " + logPath
	}
	m.setStatus(statusKind, status)
}
