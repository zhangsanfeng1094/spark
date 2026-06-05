package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

type promptSection int

const (
	promptSectionPresets promptSection = iota
	promptSectionBindings
	promptSectionSettings
)

type promptFocus int

const (
	promptFocusList promptFocus = iota
	promptFocusActions
	promptFocusFields
)

type promptEditKind int

const (
	promptEditPreset promptEditKind = iota
	promptEditBinding
	promptEditSettings
)

type promptFieldKind int

const (
	promptFieldInput promptFieldKind = iota
	promptFieldSelect
)

type promptAction struct {
	Key   string
	Label string
}

type promptField struct {
	Label   string
	Value   string
	Kind    promptFieldKind
	Options []string
}

type promptManagerModel struct {
	source *config.RootConfig
	cfg    *config.RootConfig
	width  int
	height int
	status string
	dirty  bool

	section       promptSection
	presetNames   []string
	presetIndex   int
	bindingIndex  int
	focus         promptFocus
	actionIndex   int
	confirmDelete bool
	confirmQuit   bool

	editing       bool
	adding        bool
	editKind      promptEditKind
	editOriginal  string
	editBindingIx int
	editFocus     int
	editFields    []promptField
	editCursor    map[int]int
	selectOpen    bool
	selectField   int
	selectCursor  int
}

func ManagePromptsDashboard(cfg *config.RootConfig) error {
	m := newPromptManagerModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newPromptManagerModel(cfg *config.RootConfig) *promptManagerModel {
	config.Normalize(cfg)
	draft := clonePromptRootConfig(cfg)
	m := &promptManagerModel{
		source:        cfg,
		cfg:           draft,
		status:        "Ready.",
		section:       promptSectionPresets,
		focus:         promptFocusList,
		editBindingIx: -1,
	}
	m.refresh()
	if len(m.presetNames) == 0 && len(m.cfg.Prompts.Bindings) > 0 {
		m.section = promptSectionBindings
	}
	return m
}

func (m *promptManagerModel) Init() tea.Cmd { return nil }

func (m *promptManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.confirmQuit {
			return m, m.handleConfirmQuitKey(msg)
		}
		if m.confirmDelete {
			return m, m.handleConfirmKey(msg)
		}
		if m.selectOpen {
			return m, m.handleSelectKey(msg)
		}
		if m.editing {
			return m, m.handleEditorKey(msg)
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *promptManagerModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "ctrl+c", "q":
		if m.dirty {
			m.confirmQuit = true
			m.status = "Unsaved changes. Save before quitting? Y save, N discard, Esc cancel."
			return nil
		}
		return tea.Quit
	case "f2", "s":
		return m.saveDraft()
	case "tab":
		m.moveFocus(1)
	case "shift+tab":
		m.moveFocus(-1)
	case "left", "h":
		if m.focus == promptFocusList {
			m.moveSection(-1)
		} else if m.focus == promptFocusActions {
			m.moveAction(-1)
		}
	case "right", "l":
		if m.focus == promptFocusList {
			m.moveSection(1)
		} else if m.focus == promptFocusActions {
			m.moveAction(1)
		}
	case "up", "k":
		if m.focus == promptFocusActions {
			m.moveAction(-1)
		} else {
			m.moveSelection(-1)
		}
	case "down", "j":
		if m.focus == promptFocusActions {
			m.moveAction(1)
		} else {
			m.moveSelection(1)
		}
	case "enter":
		if m.focus == promptFocusActions {
			return m.activateAction()
		}
		m.startEditCurrent()
	case "a":
		m.startAddCurrentSection()
	case "e":
		m.startEditCurrent()
	case "c":
		if m.section == promptSectionPresets {
			m.copyCurrentPreset()
		}
	case "d", "x":
		m.startDeleteCurrent()
	case "v":
		m.validate()
	case "t":
		m.toggleCurrentBinding()
	case " ":
		m.toggleEnabled()
	}
	return nil
}

func (m *promptManagerModel) handleEditorKey(msg tea.KeyMsg) tea.Cmd {
	if m.editFocus >= 0 && m.editFocus < len(m.editFields) && m.editFields[m.editFocus].Kind == promptFieldSelect {
		switch msg.String() {
		case "enter", " ", "space", "right", "l":
			m.openFieldSelect()
			return nil
		}
	}
	switch msg.String() {
	case "esc":
		m.editing = false
		m.status = "Edit canceled."
	case "tab":
		m.moveEditFocus(1)
	case "shift+tab":
		m.moveEditFocus(-1)
	case "f2":
		m.applyEditorToDraft()
	case "up":
		m.moveEditFocus(-1)
	case "down":
		m.moveEditFocus(1)
	case "left":
		m.moveFieldCursor(-1)
	case "right":
		m.moveFieldCursor(1)
	case "home":
		m.editCursor[m.editFocus] = 0
	case "end":
		m.editCursor[m.editFocus] = len([]rune(m.editFields[m.editFocus].Value))
	case "backspace":
		m.editFields[m.editFocus].Value, m.editCursor[m.editFocus] = deleteBeforeCursor(m.editFields[m.editFocus].Value, m.editCursor[m.editFocus])
	case "delete":
		m.editFields[m.editFocus].Value, m.editCursor[m.editFocus] = deleteAtCursor(m.editFields[m.editFocus].Value, m.editCursor[m.editFocus])
	default:
		if len(msg.Runes) > 0 && m.editFields[m.editFocus].Kind == promptFieldInput {
			m.editFields[m.editFocus].Value, m.editCursor[m.editFocus] = insertAtCursor(m.editFields[m.editFocus].Value, m.editCursor[m.editFocus], filterPrintableRunes(msg.Runes))
		}
	}
	return nil
}

func (m *promptManagerModel) handleSelectKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.closeFieldSelect()
	case "up", "k", "shift+tab":
		m.moveSelectCursor(-1)
	case "down", "j", "tab":
		m.moveSelectCursor(1)
	case "enter", " ", "space":
		m.confirmFieldSelect()
	}
	return nil
}

func (m *promptManagerModel) refresh() {
	config.Normalize(m.cfg)
	m.presetNames = m.cfg.PromptPresetNames()
	m.presetIndex = clampIndex(m.presetIndex, len(m.presetNames))
	m.bindingIndex = clampIndex(m.bindingIndex, len(m.cfg.Prompts.Bindings))
	m.actionIndex = clampIndex(m.actionIndex, len(m.actions()))
}

func (m *promptManagerModel) moveFocus(delta int) {
	focuses := []promptFocus{promptFocusList, promptFocusActions}
	current := 0
	for i, focus := range focuses {
		if focus == m.focus {
			current = i
		}
	}
	next := (current + delta) % len(focuses)
	if next < 0 {
		next += len(focuses)
	}
	m.focus = focuses[next]
}

func (m *promptManagerModel) moveSelection(delta int) {
	switch m.section {
	case promptSectionPresets:
		if delta > 0 && len(m.cfg.Prompts.Bindings) > 0 && m.presetIndex == len(m.presetNames)-1 {
			m.section = promptSectionBindings
			m.bindingIndex = 0
			return
		}
		m.presetIndex = clampIndex(m.presetIndex+delta, len(m.presetNames))
	case promptSectionBindings:
		if delta < 0 && len(m.presetNames) > 0 && m.bindingIndex == 0 {
			m.section = promptSectionPresets
			m.presetIndex = len(m.presetNames) - 1
			return
		}
		m.bindingIndex = clampIndex(m.bindingIndex+delta, len(m.cfg.Prompts.Bindings))
	}
}

func (m *promptManagerModel) moveSection(delta int) {
	sections := []promptSection{promptSectionPresets, promptSectionBindings, promptSectionSettings}
	current := 0
	for i, section := range sections {
		if section == m.section {
			current = i
			break
		}
	}
	next := (current + delta) % len(sections)
	if next < 0 {
		next += len(sections)
	}
	m.section = sections[next]
}

func (m *promptManagerModel) moveAction(delta int) {
	m.actionIndex = clampIndex(m.actionIndex+delta, len(m.actions()))
}

func (m *promptManagerModel) actions() []promptAction {
	if m.section == promptSectionSettings {
		return []promptAction{{"save", "Save"}, {"edit", "Edit"}, {"validate", "Validate"}}
	}
	if m.section == promptSectionBindings {
		return []promptAction{{"save", "Save"}, {"add", "Add"}, {"edit", "Edit"}, {"toggle", "Toggle"}, {"delete", "Delete"}, {"validate", "Validate"}}
	}
	return []promptAction{{"save", "Save"}, {"add", "Add"}, {"edit", "Edit"}, {"copy", "Copy"}, {"delete", "Delete"}, {"validate", "Validate"}}
}

func (m *promptManagerModel) activateAction() tea.Cmd {
	actions := m.actions()
	if len(actions) == 0 {
		return nil
	}
	switch actions[clampIndex(m.actionIndex, len(actions))].Key {
	case "save":
		return m.saveDraft()
	case "add":
		m.startAddCurrentSection()
	case "edit":
		m.startEditCurrent()
	case "copy":
		m.copyCurrentPreset()
	case "toggle":
		m.toggleCurrentBinding()
	case "delete":
		m.startDeleteCurrent()
	case "validate":
		m.validate()
	}
	return nil
}

func (m *promptManagerModel) startAddCurrentSection() {
	if m.section == promptSectionSettings {
		m.status = infoStatus("Settings can be edited directly.")
		return
	}
	m.editing = true
	m.adding = true
	m.editOriginal = ""
	m.editBindingIx = -1
	m.editFocus = 0
	if m.section == promptSectionBindings {
		m.editKind = promptEditBinding
		binding := config.PromptBinding{Integration: config.PromptIntegrationCodex, Model: "*"}
		binding.SetEnabled(true)
		m.editFields = m.newBindingFields(binding)
		m.status = "Add binding. F2 applies to draft."
	} else {
		m.editKind = promptEditPreset
		m.editFields = []promptField{{Label: "Name"}, {Label: "Description"}, {Label: "File"}, {Label: "Mode", Value: config.PromptModeAppend, Kind: promptFieldSelect, Options: []string{config.PromptModeAppend, config.PromptModeReplace}}}
		m.status = "Add preset. F2 applies to draft."
	}
	m.resetEditCursors()
}

func (m *promptManagerModel) startEditCurrent() {
	if m.section == promptSectionSettings {
		m.editing = true
		m.adding = false
		m.editKind = promptEditSettings
		m.editFields = m.newSettingsFields()
		m.status = "Edit settings. F2 applies to draft."
		m.resetEditCursors()
		return
	}
	if m.section == promptSectionBindings {
		if len(m.cfg.Prompts.Bindings) == 0 {
			return
		}
		idx := clampIndex(m.bindingIndex, len(m.cfg.Prompts.Bindings))
		m.editing = true
		m.adding = false
		m.editKind = promptEditBinding
		m.editBindingIx = idx
		m.editFields = m.newBindingFields(m.cfg.Prompts.Bindings[idx])
		m.status = "Edit binding. F2 applies to draft."
		m.resetEditCursors()
		return
	}
	name := m.currentPresetName()
	if name == "" {
		return
	}
	preset := m.cfg.Prompts.Presets[name]
	m.editing = true
	m.adding = false
	m.editKind = promptEditPreset
	m.editOriginal = name
	m.editFields = []promptField{
		{Label: "Name", Value: preset.Name},
		{Label: "Description", Value: preset.Description},
		{Label: "File", Value: preset.File},
		{Label: "Mode", Value: defaultString(config.NormalizePromptMode(preset.Mode), config.PromptModeAppend), Kind: promptFieldSelect, Options: []string{config.PromptModeAppend, config.PromptModeReplace}},
	}
	m.status = "Edit preset. F2 applies to draft."
	m.resetEditCursors()
}

func (m *promptManagerModel) newBindingFields(binding config.PromptBinding) []promptField {
	presets := m.cfg.PromptPresetNames()
	presetValue := binding.Preset
	if presetValue == "" && len(presets) > 0 {
		presetValue = presets[0]
	}
	return []promptField{
		{Label: "Integration", Value: defaultString(config.NormalizePromptIntegration(binding.Integration), config.PromptIntegrationCodex), Kind: promptFieldSelect, Options: []string{config.PromptIntegrationCodex, config.PromptIntegrationClaude}},
		{Label: "Model", Value: binding.Model},
		{Label: "Preset", Value: presetValue, Kind: promptFieldSelect, Options: presets},
		{Label: "Enabled", Value: boolString(binding.IsEnabled()), Kind: promptFieldSelect, Options: []string{"true", "false"}},
	}
}

func (m *promptManagerModel) newSettingsFields() []promptField {
	return []promptField{
		{Label: "Codex Catalog", Value: m.cfg.Integration("codex").ModelCatalogJSON},
	}
}

func (m *promptManagerModel) resetEditCursors() {
	m.editCursor = make(map[int]int, len(m.editFields))
	for i, field := range m.editFields {
		m.editCursor[i] = len([]rune(field.Value))
	}
}

func (m *promptManagerModel) moveEditFocus(delta int) {
	if len(m.editFields) == 0 {
		return
	}
	m.editFocus = clampIndex(m.editFocus+delta, len(m.editFields))
}

func (m *promptManagerModel) moveFieldCursor(delta int) {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) || m.editFields[m.editFocus].Kind == promptFieldSelect {
		return
	}
	maxCursor := len([]rune(m.editFields[m.editFocus].Value))
	m.editCursor[m.editFocus] = clampIndexInclusive(m.editCursor[m.editFocus]+delta, maxCursor)
}

func (m *promptManagerModel) cycleField(delta int) {
	field := &m.editFields[m.editFocus]
	if field.Kind != promptFieldSelect || len(field.Options) == 0 {
		return
	}
	current := strings.TrimSpace(field.Value)
	for i, option := range field.Options {
		if option == current {
			next := (i + delta) % len(field.Options)
			if next < 0 {
				next += len(field.Options)
			}
			field.Value = field.Options[next]
			return
		}
	}
	field.Value = field.Options[0]
}

func (m *promptManagerModel) openFieldSelect() {
	if m.editFocus < 0 || m.editFocus >= len(m.editFields) {
		return
	}
	field := m.editFields[m.editFocus]
	if field.Kind != promptFieldSelect || len(field.Options) == 0 {
		return
	}
	m.selectOpen = true
	m.selectField = m.editFocus
	m.selectCursor = 0
	for i, option := range field.Options {
		if option == strings.TrimSpace(field.Value) {
			m.selectCursor = i
			break
		}
	}
}

func (m *promptManagerModel) closeFieldSelect() {
	m.selectOpen = false
	m.selectField = -1
	m.selectCursor = 0
}

func (m *promptManagerModel) moveSelectCursor(delta int) {
	if !m.selectOpen || m.selectField < 0 || m.selectField >= len(m.editFields) {
		return
	}
	options := m.editFields[m.selectField].Options
	if len(options) == 0 {
		return
	}
	m.selectCursor = clampIndex(m.selectCursor+delta, len(options))
}

func (m *promptManagerModel) confirmFieldSelect() {
	if !m.selectOpen || m.selectField < 0 || m.selectField >= len(m.editFields) {
		return
	}
	options := m.editFields[m.selectField].Options
	if len(options) > 0 {
		m.editFields[m.selectField].Value = options[clampIndex(m.selectCursor, len(options))]
	}
	m.closeFieldSelect()
}

func (m *promptManagerModel) applyEditorToDraft() {
	if m.editKind == promptEditSettings {
		m.applySettingsEditor()
		return
	}
	if m.editKind == promptEditBinding {
		m.applyBindingEditor()
		return
	}
	m.applyPresetEditor()
}

func (m *promptManagerModel) applySettingsEditor() {
	m.cfg.Integration("codex").ModelCatalogJSON = strings.TrimSpace(m.editFields[0].Value)
	m.finishDraftApply("Applied settings.")
}

func (m *promptManagerModel) applyPresetEditor() {
	name := strings.TrimSpace(m.editFields[0].Value)
	if name == "" {
		m.status = errorStatus("Preset name is required.")
		return
	}
	file := strings.TrimSpace(m.editFields[2].Value)
	if file == "" {
		m.status = errorStatus("Preset file path is required.")
		return
	}
	mode := config.NormalizePromptMode(m.editFields[3].Value)
	if mode == "" {
		m.status = errorStatus("Mode must be append or replace.")
		return
	}
	if m.editOriginal != "" && m.editOriginal != name {
		delete(m.cfg.Prompts.Presets, m.editOriginal)
		for i := range m.cfg.Prompts.Bindings {
			if m.cfg.Prompts.Bindings[i].Preset == m.editOriginal {
				m.cfg.Prompts.Bindings[i].Preset = name
			}
		}
	}
	m.cfg.Prompts.Presets[name] = &config.PromptPreset{Name: name, Description: strings.TrimSpace(m.editFields[1].Value), File: file, Mode: mode}
	m.finishDraftApply("Applied preset " + name + ".")
	m.selectPreset(name)
}

func (m *promptManagerModel) applyBindingEditor() {
	binding := config.PromptBinding{
		Integration: m.editFields[0].Value,
		Model:       config.NormalizePromptBindingModel(m.editFields[1].Value),
		Preset:      strings.TrimSpace(m.editFields[2].Value),
	}
	binding.SetEnabled(strings.TrimSpace(strings.ToLower(m.editFields[3].Value)) != "false")
	if config.NormalizePromptIntegration(binding.Integration) == "" {
		m.status = errorStatus("Integration must be codex or claude.")
		return
	}
	if binding.Model == "" {
		m.status = errorStatus("Model is required.")
		return
	}
	if m.cfg.Prompts.Presets[binding.Preset] == nil {
		m.status = errorStatus("Preset does not exist.")
		return
	}
	binding.Integration = config.NormalizePromptIntegration(binding.Integration)
	for i, existing := range m.cfg.Prompts.Bindings {
		if i == m.editBindingIx {
			continue
		}
		if existing.Integration == binding.Integration && existing.Model == binding.Model {
			m.status = errorStatus("Binding already exists for integration and model.")
			return
		}
	}
	if m.editBindingIx >= 0 && m.editBindingIx < len(m.cfg.Prompts.Bindings) {
		m.cfg.Prompts.Bindings[m.editBindingIx] = binding
	} else {
		m.cfg.Prompts.Bindings = append(m.cfg.Prompts.Bindings, binding)
		m.bindingIndex = len(m.cfg.Prompts.Bindings) - 1
	}
	m.finishDraftApply("Applied binding " + binding.Integration + "/" + binding.Model + ".")
}

func (m *promptManagerModel) finishDraftApply(status string) {
	config.Normalize(m.cfg)
	m.dirty = true
	m.editing = false
	m.refresh()
	m.status = successStatus(status + " Press S or F2 to save.")
}

func (m *promptManagerModel) copyCurrentPreset() {
	name := m.currentPresetName()
	if name == "" {
		return
	}
	preset := m.cfg.Prompts.Presets[name]
	copyName := uniquePromptPresetName(m.cfg, name+" copy")
	m.cfg.Prompts.Presets[copyName] = &config.PromptPreset{Name: copyName, Description: preset.Description, File: preset.File, Mode: preset.Mode}
	m.dirty = true
	m.refresh()
	m.selectPreset(copyName)
	m.status = successStatus("Copied preset " + name + ". Press S or F2 to save.")
}

func (m *promptManagerModel) startDeleteCurrent() {
	if m.section == promptSectionSettings {
		m.status = infoStatus("Settings cannot be deleted.")
		return
	}
	if m.section == promptSectionPresets && m.currentPresetName() == "" {
		return
	}
	if m.section == promptSectionBindings && len(m.cfg.Prompts.Bindings) == 0 {
		return
	}
	m.confirmDelete = true
	m.status = "Delete selected prompt item? Press Y to confirm or N to cancel."
}

func (m *promptManagerModel) handleConfirmKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.confirmDelete = false
		m.deleteCurrent()
	case "n", "esc":
		m.confirmDelete = false
		m.status = "Delete canceled."
	}
	return nil
}

func (m *promptManagerModel) handleConfirmQuitKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.confirmQuit = false
		if err := m.persistDraft(); err != nil {
			m.status = errorStatus(err.Error())
			return nil
		}
		return tea.Quit
	case "n":
		m.confirmQuit = false
		return tea.Quit
	case "esc", "q":
		m.confirmQuit = false
		m.status = "Quit canceled."
	}
	return nil
}

func (m *promptManagerModel) deleteCurrent() {
	if m.section == promptSectionBindings {
		idx := clampIndex(m.bindingIndex, len(m.cfg.Prompts.Bindings))
		if len(m.cfg.Prompts.Bindings) == 0 {
			return
		}
		m.cfg.Prompts.Bindings = append(m.cfg.Prompts.Bindings[:idx], m.cfg.Prompts.Bindings[idx+1:]...)
		m.applyAfterDelete("Deleted binding.")
		return
	}
	name := m.currentPresetName()
	if err := m.cfg.RemovePromptPreset(name); err != nil {
		m.status = errorStatus(err.Error())
		return
	}
	m.applyAfterDelete("Deleted preset " + name + ".")
}

func (m *promptManagerModel) applyAfterDelete(status string) {
	m.dirty = true
	m.refresh()
	m.status = successStatus(status + " Press S or F2 to save.")
}

func (m *promptManagerModel) saveDraft() tea.Cmd {
	if err := m.persistDraft(); err != nil {
		m.status = errorStatus(err.Error())
	}
	return nil
}

func (m *promptManagerModel) persistDraft() error {
	config.Normalize(m.cfg)
	if err := config.Save(m.cfg); err != nil {
		return err
	}
	if m.source != nil {
		*m.source = *clonePromptRootConfig(m.cfg)
	}
	m.dirty = false
	m.refresh()
	m.status = successStatus("Prompt configuration saved.")
	return nil
}

func (m *promptManagerModel) validate() {
	issues := m.cfg.CheckPrompts()
	if len(issues) == 0 {
		m.status = successStatus("Prompt configuration is valid.")
		return
	}
	activeErrors := 0
	inactiveWarnings := 0
	firstActive := ""
	firstInactive := ""
	for _, issue := range issues {
		if issue.Active && issue.Severity == config.PromptValidationError {
			activeErrors++
			if firstActive == "" {
				firstActive = issue.Message
			}
		} else {
			inactiveWarnings++
			if firstInactive == "" {
				firstInactive = issue.Message
			}
		}
	}
	if activeErrors > 0 {
		msg := fmt.Sprintf("%d active error(s)", activeErrors)
		if inactiveWarnings > 0 {
			msg += fmt.Sprintf(", %d inactive warning(s)", inactiveWarnings)
		}
		m.status = errorStatus(msg + ": " + firstActive)
		return
	}
	m.status = infoStatus(fmt.Sprintf("No active errors. %d inactive warning(s): %s", inactiveWarnings, firstInactive))
}

func (m *promptManagerModel) toggleEnabled() {
	next := !m.cfg.Prompts.IsEnabled()
	m.cfg.Prompts.SetEnabled(next)
	m.dirty = true
	if next {
		m.status = successStatus("Prompt injection enabled. Press S or F2 to save.")
	} else {
		m.status = infoStatus("Prompt injection disabled in draft. Existing presets and bindings are preserved.")
	}
}

func (m *promptManagerModel) toggleCurrentBinding() {
	if m.section != promptSectionBindings {
		m.status = infoStatus("Select a binding before toggling it.")
		return
	}
	if len(m.cfg.Prompts.Bindings) == 0 {
		m.status = infoStatus("No bindings to toggle.")
		return
	}
	idx := clampIndex(m.bindingIndex, len(m.cfg.Prompts.Bindings))
	binding := &m.cfg.Prompts.Bindings[idx]
	next := !binding.IsEnabled()
	binding.SetEnabled(next)
	m.dirty = true
	m.refresh()
	state := "enabled"
	if !next {
		state = "disabled"
	}
	m.status = successStatus("Binding " + binding.Integration + "/" + binding.Model + " " + state + ". Press S or F2 to save.")
}

func (m *promptManagerModel) presetBindingSummary(name string) []string {
	var lines []string
	for _, binding := range m.cfg.Prompts.Bindings {
		if strings.TrimSpace(binding.Preset) != name {
			continue
		}
		lines = append(lines, m.bindingSummary(binding))
	}
	return lines
}

func (m *promptManagerModel) bindingSummary(binding config.PromptBinding) string {
	return fmt.Sprintf("%s · %s · %s · %s", binding.Integration, displayPromptBindingModel(binding.Model), strings.ToUpper(m.effectiveModeForBinding(binding)), bindingState(binding))
}

func (m *promptManagerModel) effectiveModeForBinding(binding config.PromptBinding) string {
	preset := m.cfg.Prompts.Presets[strings.TrimSpace(binding.Preset)]
	mode := config.EffectivePromptMode(binding, preset)
	return defaultString(mode, defaultString(binding.Mode, config.PromptModeAppend))
}

func (m *promptManagerModel) currentPresetName() string {
	if len(m.presetNames) == 0 {
		return ""
	}
	return m.presetNames[clampIndex(m.presetIndex, len(m.presetNames))]
}

func (m *promptManagerModel) currentBinding() (config.PromptBinding, bool) {
	if len(m.cfg.Prompts.Bindings) == 0 {
		return config.PromptBinding{}, false
	}
	return m.cfg.Prompts.Bindings[clampIndex(m.bindingIndex, len(m.cfg.Prompts.Bindings))], true
}

func (m *promptManagerModel) selectPreset(name string) {
	for i, presetName := range m.presetNames {
		if presetName == name {
			m.presetIndex = i
			return
		}
	}
}

func (m *promptManagerModel) View() string {
	if m.width == 0 {
		return "loading..."
	}
	header := dashboardHeaderStyle.Width(m.width - 6).Render(m.headerTitle())
	leftW := 36
	if m.width < 100 {
		leftW = 34
	}
	rightW := m.width - leftW - 4
	if rightW < 50 {
		rightW = 50
	}
	statusBar := pmStatusBarStyle.Width(m.width - 4).Render(lipgloss.NewStyle().Align(lipgloss.Right).Foreground(colorMuted).Render(m.helpText()))
	left := m.renderLeftPane(0)
	right := m.renderRightPane(0)
	paneInnerH := max(lipgloss.Height(left), lipgloss.Height(right))
	availableOuterH := m.height - lipgloss.Height(header) - lipgloss.Height(statusBar)
	if availableOuterH > 2 {
		paneInnerH = availableOuterH - 2
	}
	left = m.renderLeftPane(paneInnerH)
	leftStyle := pmPanelStyle.Width(leftW)
	rightStyle := pmPanelStyle.Width(rightW)
	if !m.editing && m.focus == promptFocusList {
		leftStyle = pmFocusedPanelStyle.Width(leftW)
	} else {
		rightStyle = pmFocusedPanelStyle.Width(rightW)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftStyle.Render(left), rightStyle.Render(right))
	view := pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar))
	if m.selectOpen {
		return fitToViewportHeight(m.overlayFieldSelect(view), m.height)
	}
	return fitToViewportHeight(view, m.height)
}

func (m *promptManagerModel) renderLeftPane(height int) string {
	width := 30
	lines := []string{lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Presets"), ""}
	if len(m.presetNames) == 0 {
		lines = append(lines, pmItemStyle.Width(width).Render("  No presets"))
	} else {
		for i, name := range m.presetNames {
			prefix := "  "
			if m.section == promptSectionPresets && i == m.presetIndex {
				prefix = "> "
			}
			style := pmItemStyle
			if m.section == promptSectionPresets && i == m.presetIndex {
				style = pmSelectedMutedItemStyle
				if m.focus == promptFocusList {
					style = pmFocusedItemStyle
				}
			}
			lines = append(lines, style.Width(width).Render(prefix+truncateDisplay(name, width-2)))
		}
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Bindings"), "")
	if len(m.cfg.Prompts.Bindings) == 0 {
		lines = append(lines, pmItemStyle.Width(width).Render("  No bindings"))
	} else {
		for i, binding := range m.cfg.Prompts.Bindings {
			label := m.bindingSummary(binding)
			prefix := "  "
			if m.section == promptSectionBindings && i == m.bindingIndex {
				prefix = "> "
			}
			style := pmItemStyle
			if m.section == promptSectionBindings && i == m.bindingIndex {
				style = pmSelectedMutedItemStyle
				if m.focus == promptFocusList {
					style = pmFocusedItemStyle
				}
			}
			lines = append(lines, style.Width(width).Render(prefix+truncateDisplay(label, width-2)))
		}
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Settings"), "")
	settingsLabel := "  Codex model catalog"
	if m.section == promptSectionSettings {
		settingsLabel = "> Codex model catalog"
		style := pmSelectedMutedItemStyle
		if m.focus == promptFocusList {
			style = pmFocusedItemStyle
		}
		lines = append(lines, style.Width(width).Render(truncateDisplay(settingsLabel, width)))
	} else {
		lines = append(lines, pmItemStyle.Width(width).Render(truncateDisplay(settingsLabel, width)))
	}
	return joinTopAndBottom(lines, m.renderActionRows(width), height)
}

func (m *promptManagerModel) renderActionRows(width int) []string {
	lines := []string{"", lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Actions")}
	parts := make([]string, 0, len(m.actions()))
	for i, action := range m.actions() {
		label := "[" + strings.ToUpper(action.Label[:1]) + "] " + action.Label
		style := pmLeftBtnStyle.Copy().MarginRight(0)
		if m.focus == promptFocusActions && i == m.actionIndex {
			style = pmLeftActiveBtnStyle.Copy().MarginRight(0)
		}
		parts = append(parts, style.Render(label))
	}
	for len(parts) > 0 {
		line := parts[0]
		parts = parts[1:]
		if len(parts) > 0 && lipgloss.Width(line)+2+lipgloss.Width(parts[0]) <= width {
			line = lipgloss.JoinHorizontal(lipgloss.Top, line, "  ", parts[0])
			parts = parts[1:]
		}
		lines = append(lines, line)
	}
	lines = append(lines, pmLeftBtnStyle.Copy().MarginRight(0).Render("[Space] ON/OFF"), pmLeftBtnStyle.Copy().MarginRight(0).Render("[S/F2] Save"), "", m.renderStatusSummary(width))
	return lines
}

func (m *promptManagerModel) renderStatusSummary(width int) string {
	main, _ := splitStatusText(m.status)
	if main == "" {
		main = "Ready."
	}
	if m.dirty {
		main = "* Unsaved changes  " + main
	}
	return lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(main)
}

func (m *promptManagerModel) renderRightPane(height int) string {
	if m.editing {
		return m.renderEditor(height)
	}
	lines := []string{lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("Prompt Details"), ""}
	if !m.cfg.Prompts.IsEnabled() {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("Prompt injection is disabled."),
			lipgloss.NewStyle().Foreground(colorMuted).Render("All presets and bindings are paused until re-enabled."),
			"",
		)
	}
	if m.section == promptSectionSettings {
		catalog := m.cfg.Integration("codex").ModelCatalogJSON
		lines = append(lines,
			m.detailRow("Codex Catalog", catalog),
			"",
			lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Catalog Format"),
			lipgloss.NewStyle().Foreground(colorMuted).Render(`Top-level JSON object: {"models": [...]}`),
			lipgloss.NewStyle().Foreground(colorMuted).Render("Each model entry uses Codex ModelInfo snake_case fields."),
		)
	} else if m.section == promptSectionBindings {
		binding, ok := m.currentBinding()
		if !ok {
			lines = append(lines, "No bindings yet", "", "Add a binding to attach a preset to an integration/model pair.")
		} else {
			lines = append(lines,
				m.detailRow("Integration", binding.Integration),
				m.detailRow("Model", displayPromptBindingModel(binding.Model)),
				m.detailRow("Preset", binding.Preset),
				m.detailRow("Effective Mode", m.effectiveModeForBinding(binding)),
				m.detailRow("Enabled", boolString(binding.IsEnabled())),
			)
			if strings.TrimSpace(binding.Mode) != "" {
				lines = append(lines, m.detailRow("Mode Override", binding.Mode))
			}
			if !binding.IsEnabled() {
				lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Render("This binding is OFF and will not inject its preset."))
			}
		}
	} else {
		name := m.currentPresetName()
		if name == "" {
			lines = append(lines, "No presets yet", "", "Add a preset that points at a prompt file under ~/.spark or another path.")
		} else {
			preset := m.cfg.Prompts.Presets[name]
			resolved, err := config.ResolvePromptPath(preset.File)
			status := "ok"
			if _, _, readErr := config.ResolvePromptPresetFile(preset); readErr != nil {
				status = readErr.Error()
			}
			if err != nil {
				resolved = err.Error()
			}
			lines = append(lines,
				m.detailRow("Name", preset.Name),
				m.detailRow("Description", preset.Description),
				m.detailRow("File", preset.File),
				m.detailRow("Mode", defaultString(config.NormalizePromptMode(preset.Mode), config.PromptModeAppend)),
				m.detailRow("Resolved", resolved),
				m.detailRow("Validation", status),
			)
			bindings := m.presetBindingSummary(name)
			lines = append(lines, "", lipgloss.NewStyle().Foreground(colorLabel).Bold(true).Render("Used By"))
			if len(bindings) == 0 {
				lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("No bindings use this preset."))
			} else {
				lines = append(lines, bindings...)
			}
		}
	}
	return joinTopAndBottom(lines, []string{"", styleStatusMain(m.status)}, height)
}

func (m *promptManagerModel) renderEditor(height int) string {
	title := "Edit Preset"
	if m.adding {
		title = "Add Preset"
	}
	if m.editKind == promptEditSettings {
		title = "Edit Settings"
	}
	if m.editKind == promptEditBinding {
		title = "Edit Binding"
		if m.adding {
			title = "Add Binding"
		}
	}
	lines := []string{lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title), ""}
	inputW := max(24, m.width-74)
	for i, field := range m.editFields {
		value := field.Value
		if field.Kind == promptFieldSelect && len(field.Options) == 0 {
			value = "no presets available"
		}
		if field.Kind == promptFieldSelect && len(field.Options) > 0 {
			value = value + "  ▼"
		}
		focused := m.editFocus == i
		lines = append(lines, renderCompactFormRow(compactFormRowOptions{
			Label:      field.Label,
			Value:      value,
			Width:      inputW,
			Focused:    focused,
			Cursor:     m.editCursor[i],
			ShowCursor: focused && field.Kind == promptFieldInput,
		}))
	}
	return joinTopAndBottom(lines, []string{"", styleStatusMain(m.status)}, height)
}

func (m *promptManagerModel) overlayFieldSelect(bg string) string {
	_ = bg
	if !m.selectOpen || m.selectField < 0 || m.selectField >= len(m.editFields) {
		return bg
	}
	field := m.editFields[m.selectField]
	return renderSelectModalOverlay(selectModalOptions{
		Width:         m.width,
		Height:        m.height,
		Title:         "Select " + field.Label,
		Options:       field.Options,
		SelectedValue: field.Value,
		Cursor:        m.selectCursor,
	})
}

func (m *promptManagerModel) detailRow(label, value string) string {
	if strings.TrimSpace(value) == "" {
		value = "-"
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, pmLabelStyle.Render(label), pmCompactReadOnlyInputStyle.Width(max(24, m.width-70)).Render(truncateDisplay(value, max(20, m.width-74))))
}

func (m *promptManagerModel) helpText() string {
	if m.confirmDelete {
		return "Y Confirm • N Cancel"
	}
	if m.confirmQuit {
		return "Y Save & Quit • N Discard • Esc Cancel"
	}
	if m.selectOpen {
		return "Up/Down Move • Enter Select • Esc Cancel"
	}
	if m.editing {
		return "Tab Move • Enter Select • F2 Apply • Esc Cancel"
	}
	return "Left/Right Section • S/F2 Save • Space ON/OFF • T Toggle Binding • Enter Edit • A Add • D Delete • V Validate • Q Quit"
}

func (m *promptManagerModel) headerTitle() string {
	state := lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("[ENABLED ●]")
	if !m.cfg.Prompts.IsEnabled() {
		state = lipgloss.NewStyle().Foreground(colorMuted).Bold(true).Render("[DISABLED ○]")
	}
	plain := "Prompt Manager"
	if m.dirty {
		plain += "  * Unsaved changes"
	}
	gap := max(1, m.width-lipgloss.Width(plain)-lipgloss.Width(StripANSI(state))-12)
	return plain + strings.Repeat(" ", gap) + state
}

func uniquePromptPresetName(cfg *config.RootConfig, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "Preset"
	}
	if cfg.Prompts.Presets[base] == nil {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s %d", base, i)
		if cfg.Prompts.Presets[name] == nil {
			return name
		}
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func bindingState(binding config.PromptBinding) string {
	if binding.IsEnabled() {
		return "ON"
	}
	return "OFF"
}

func displayPromptBindingModel(model string) string {
	if strings.TrimSpace(model) == "*" {
		return "all models"
	}
	return model
}

func clonePromptRootConfig(cfg *config.RootConfig) *config.RootConfig {
	if cfg == nil {
		return &config.RootConfig{}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		clone := *cfg
		return &clone
	}
	var clone config.RootConfig
	if err := json.Unmarshal(data, &clone); err != nil {
		fallback := *cfg
		return &fallback
	}
	config.Normalize(&clone)
	return &clone
}
