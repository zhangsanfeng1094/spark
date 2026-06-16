package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

type settingsFieldKind int

const (
	settingsFieldSelect settingsFieldKind = iota
	settingsFieldToggle
	settingsFieldText
	settingsFieldReadOnly
	settingsFieldAction
)

const (
	settingsSectionGeneral = iota
	settingsSectionPrompt
	settingsSectionIntegration
	settingsSectionHistory
	settingsSectionConfig
	settingsSectionCount
)

const (
	settingsActionClearHistory = "clear_history"
	settingsFieldCodexCatalog  = "codex_model_catalog_json"
	settingsLabelWidth         = 28
)

type settingsDraft struct {
	DefaultIntegration    string
	DefaultProfile        string
	PromptEnabled         bool
	CodexModelCatalogJSON string
	History               config.History
}

type settingsField struct {
	label   string
	kind    settingsFieldKind
	value   string
	options []string
	key     string
	action  string
}

type settingsSection struct {
	title  string
	fields []settingsField
}

type settingsModel struct {
	cfg *config.RootConfig

	draft settingsDraft

	integrationNames []string
	profileNames     []string
	configPath       string

	focusSection  int
	focusField    int
	catalogCursor int

	width  int
	height int

	status string
	dirty  bool
}

func ManageSettingsDashboard(cfg *config.RootConfig, integrationNames []string) error {
	m := newSettingsModel(cfg, integrationNames)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func newSettingsModel(cfg *config.RootConfig, integrationNames []string) *settingsModel {
	if cfg == nil {
		cfg = &config.RootConfig{}
	}
	config.Normalize(cfg)
	path, _ := config.ConfigPath()
	m := &settingsModel{
		cfg:              cfg,
		integrationNames: cleanIntegrationNames(integrationNames),
		profileNames:     settingsProfileNames(cfg),
		configPath:       path,
		status:           "Ready. Change fields, then save settings.",
	}
	m.loadDraftFromConfig()
	return m
}

func (m *settingsModel) Init() tea.Cmd { return nil }

func (m *settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *settingsModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(msg.String())
	switch key {
	case "ctrl+c", "esc":
		return tea.Quit
	case "ctrl+s", "f2":
		m.save()
		return nil
	case "q":
		if !m.focusedFieldIsText() {
			return tea.Quit
		}
	}

	if m.handleFocusedTextKey(msg) {
		return nil
	}

	switch key {
	case "tab":
		m.moveSection(1)
	case "shift+tab":
		m.moveSection(-1)
	case "up", "k":
		m.moveField(-1)
	case "down", "j":
		m.moveField(1)
	case "enter":
		m.activateField()
	}
	return nil
}

func (m *settingsModel) View() string {
	if m.width == 0 {
		return "loading..."
	}
	width := m.width
	if width < 84 {
		width = 84
	}

	header := dashboardHeaderStyle.Width(width - 6).Render("Settings")
	leftW := 28
	rightW := width - leftW - 4
	if rightW < 54 {
		rightW = 54
	}

	left := m.renderSectionList(leftW)
	right := m.renderFields(rightW)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		pmPanelStyle.Width(leftW).Render(left),
		pmFocusedPanelStyle.Width(rightW).Render(right),
	)
	status := m.status
	if m.dirty {
		status = status + " Unsaved changes."
	}
	footer := pmStatusBarStyle.Width(width - 4).Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Foreground(colorText).Render(status),
			lipgloss.NewStyle().Width(max(0, width-lipgloss.Width(status)-8)).Align(lipgloss.Right).Foreground(colorMuted).Render("Tab Section | Up/Down Field | Enter Change | F2/Ctrl+S Save | Esc Back"),
		),
	)

	return fitToViewportHeight(pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer)), m.height)
}

func (m *settingsModel) loadDraftFromConfig() {
	codexCatalog := ""
	if m.cfg != nil && m.cfg.Integrations != nil && m.cfg.Integrations["codex"] != nil {
		codexCatalog = m.cfg.Integrations["codex"].ModelCatalogJSON
	}
	m.draft = settingsDraft{
		DefaultIntegration:    strings.ToLower(strings.TrimSpace(m.cfg.DefaultIntegration)),
		DefaultProfile:        m.cfg.DefaultProfile,
		PromptEnabled:         m.cfg.Prompts.IsEnabled(),
		CodexModelCatalogJSON: strings.TrimSpace(codexCatalog),
		History:               m.cfg.History,
	}
	m.catalogCursor = len([]rune(m.draft.CodexModelCatalogJSON))
}

func (m *settingsModel) sections() []settingsSection {
	return []settingsSection{
		{
			title: "General",
			fields: []settingsField{
				{label: "Default integration", kind: settingsFieldSelect, value: m.draft.DefaultIntegration, options: defaultIntegrationOptions(m.integrationNames)},
				{label: "Default profile", kind: settingsFieldSelect, value: m.draft.DefaultProfile, options: m.profileNames},
				{label: "Effective default model", kind: settingsFieldReadOnly, value: effectiveModelForProfile(m.cfg, m.draft.DefaultProfile)},
			},
		},
		{
			title: "Prompt",
			fields: []settingsField{
				{label: "Prompt injection enabled", kind: settingsFieldToggle, value: boolLabel(m.draft.PromptEnabled)},
				{label: "Prompt presets", kind: settingsFieldReadOnly, value: strconv.Itoa(len(m.cfg.Prompts.Presets))},
				{label: "Prompt bindings", kind: settingsFieldReadOnly, value: strconv.Itoa(len(m.cfg.Prompts.Bindings))},
			},
		},
		{
			title: "Integration",
			fields: []settingsField{
				{label: "Codex model catalog JSON", kind: settingsFieldText, value: m.draft.CodexModelCatalogJSON, key: settingsFieldCodexCatalog},
			},
		},
		{
			title: "History",
			fields: []settingsField{
				{label: "Last launched integration", kind: settingsFieldReadOnly, value: m.draft.History.LastSelection},
				{label: "Last model input", kind: settingsFieldReadOnly, value: m.draft.History.LastModelInput},
				{label: "Model input count", kind: settingsFieldReadOnly, value: strconv.Itoa(len(m.draft.History.ModelInputs))},
				{label: "Clear history", kind: settingsFieldAction, value: "clear", action: settingsActionClearHistory},
			},
		},
		{
			title: "Config",
			fields: []settingsField{
				{label: "Config path", kind: settingsFieldReadOnly, value: m.configPath},
				{label: "Config version", kind: settingsFieldReadOnly, value: strconv.Itoa(m.cfg.Version)},
			},
		},
	}
}

func (m *settingsModel) currentSection() settingsSection {
	sections := m.sections()
	m.focusSection = clampIndex(m.focusSection, len(sections))
	return sections[m.focusSection]
}

func (m *settingsModel) currentField() settingsField {
	section := m.currentSection()
	m.focusField = clampIndex(m.focusField, len(section.fields))
	return section.fields[m.focusField]
}

func (m *settingsModel) renderSectionList(width int) string {
	sections := m.sections()
	lines := []string{
		dashboardSectionTitleStyle.Render("Sections"),
		"",
	}
	for i, section := range sections {
		line := "  " + section.title
		style := pmItemStyle.Width(width - 4)
		if i == m.focusSection {
			line = "> " + section.title
			style = pmFocusedItemStyle.Width(width - 4)
		}
		lines = append(lines, style.Render(line))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *settingsModel) renderFields(width int) string {
	sections := m.sections()
	m.focusSection = clampIndex(m.focusSection, len(sections))
	m.focusField = clampIndex(m.focusField, len(sections[m.focusSection].fields))
	lines := []string{}
	inputW := max(24, width-settingsLabelWidth-8)
	for sectionIndex, section := range sections {
		if sectionIndex > 0 {
			lines = append(lines, "")
		}
		titleStyle := dashboardSectionTitleStyle
		if sectionIndex == m.focusSection {
			titleStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		lines = append(lines, titleStyle.Render(section.title))
		for fieldIndex, field := range section.fields {
			focused := sectionIndex == m.focusSection && fieldIndex == m.focusField
			lines = append(lines, m.renderField(field, focused, inputW))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *settingsModel) renderField(field settingsField, focused bool, inputW int) string {
	if field.kind == settingsFieldAction {
		style := pmItemStyle.Width(inputW + settingsLabelWidth + 2)
		label := field.label
		if focused {
			style = pmFocusedItemStyle.Width(inputW + settingsLabelWidth + 2)
			label = "> " + label
		} else {
			label = "  " + label
		}
		return style.Render(label)
	}
	return renderSettingsFormRow(field.label, m.displayFieldValue(field), inputW, focused, field.kind == settingsFieldReadOnly, m.fieldCursor(field), focused && field.kind == settingsFieldText)
}

func renderSettingsFormRow(label, value string, inputW int, focused, readOnly bool, cursor int, showCursor bool) string {
	width := max(1, inputW)
	displayValue := truncateDisplay(value, width-2)
	if showCursor {
		cursor = clampIndexInclusive(cursor, len([]rune(value)))
		r := []rune(value)
		displayValue = truncateDisplay(string(r[:cursor])+"█"+string(r[cursor:]), width-2)
	}

	inputStyle := pmCompactInputStyle.Copy().Width(width)
	if focused {
		inputStyle = pmCompactFocusedInputStyle.Copy().Width(width)
	}
	if readOnly {
		inputStyle = pmCompactReadOnlyInputStyle.Copy().Width(width)
		if focused {
			inputStyle = pmCompactFocusedInputStyle.Copy().Width(width)
		}
	}

	labelStyle := pmLabelStyle.Copy().Width(settingsLabelWidth)
	if focused {
		labelStyle = pmFocusedLabelStyle.Copy().Width(settingsLabelWidth)
	}
	divider := lipgloss.NewStyle().Foreground(colorBorder).Render("│")
	return lipgloss.JoinHorizontal(lipgloss.Center, labelStyle.Render(label), divider, inputStyle.Render(displayValue))
}

func (m *settingsModel) displayFieldValue(field settingsField) string {
	switch field.kind {
	case settingsFieldToggle:
		return field.value
	case settingsFieldSelect:
		if field.label == "Default integration" && strings.TrimSpace(field.value) == "" {
			return "history fallback"
		}
	case settingsFieldReadOnly:
		if strings.TrimSpace(field.value) == "" {
			return "not set"
		}
	}
	return field.value
}

func (m *settingsModel) fieldCursor(field settingsField) int {
	if field.key == settingsFieldCodexCatalog {
		return m.catalogCursor
	}
	return len([]rune(field.value))
}

func (m *settingsModel) moveSection(delta int) {
	m.focusSection = (m.focusSection + delta + settingsSectionCount) % settingsSectionCount
	fields := m.currentSection().fields
	m.focusField = clampIndex(m.focusField, len(fields))
}

func (m *settingsModel) moveField(delta int) {
	fields := m.currentSection().fields
	m.focusField = moveCursor(m.focusField, len(fields), delta)
}

func (m *settingsModel) activateField() {
	field := m.currentField()
	switch field.kind {
	case settingsFieldSelect:
		m.cycleSelect(field)
	case settingsFieldToggle:
		m.draft.PromptEnabled = !m.draft.PromptEnabled
		m.markDirty("Prompt injection setting changed. Save to persist.")
	case settingsFieldAction:
		if field.action == settingsActionClearHistory {
			m.draft.History = config.History{}
			m.markDirty("History cleared. Save to persist.")
		}
	}
}

func (m *settingsModel) cycleSelect(field settingsField) {
	if len(field.options) == 0 {
		m.status = "No options available."
		return
	}
	current := field.value
	idx := indexOfFold(field.options, current)
	if idx < 0 {
		idx = 0
	} else {
		idx = (idx + 1) % len(field.options)
	}
	next := field.options[idx]
	switch field.label {
	case "Default integration":
		m.draft.DefaultIntegration = strings.ToLower(strings.TrimSpace(next))
	case "Default profile":
		m.draft.DefaultProfile = next
	}
	m.markDirty(field.label + " changed. Save to persist.")
}

func (m *settingsModel) focusedFieldIsText() bool {
	return m.currentField().kind == settingsFieldText
}

func (m *settingsModel) handleFocusedTextKey(msg tea.KeyMsg) bool {
	field := m.currentField()
	if field.kind != settingsFieldText {
		return false
	}
	switch msg.Type {
	case tea.KeyLeft:
		m.catalogCursor = moveRuneCursor(m.catalogCursor, m.draft.CodexModelCatalogJSON, -1)
		return true
	case tea.KeyRight:
		m.catalogCursor = moveRuneCursor(m.catalogCursor, m.draft.CodexModelCatalogJSON, 1)
		return true
	case tea.KeyHome:
		m.catalogCursor = 0
		return true
	case tea.KeyEnd:
		m.catalogCursor = len([]rune(m.draft.CodexModelCatalogJSON))
		return true
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.deleteCatalogRune(-1)
		return true
	case tea.KeyDelete:
		m.deleteCatalogRune(1)
		return true
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return true
		}
		m.insertCatalogRunes(msg.Runes)
		return true
	}
	return false
}

func (m *settingsModel) insertCatalogRunes(in []rune) {
	r := []rune(m.draft.CodexModelCatalogJSON)
	m.catalogCursor = clampIndexInclusive(m.catalogCursor, len(r))
	next := make([]rune, 0, len(r)+len(in))
	next = append(next, r[:m.catalogCursor]...)
	next = append(next, in...)
	next = append(next, r[m.catalogCursor:]...)
	m.draft.CodexModelCatalogJSON = string(next)
	m.catalogCursor += len(in)
	m.markDirty("Codex model catalog changed. Save to persist.")
}

func (m *settingsModel) deleteCatalogRune(direction int) {
	r := []rune(m.draft.CodexModelCatalogJSON)
	m.catalogCursor = clampIndexInclusive(m.catalogCursor, len(r))
	if direction < 0 {
		if m.catalogCursor == 0 {
			return
		}
		r = append(r[:m.catalogCursor-1], r[m.catalogCursor:]...)
		m.catalogCursor--
	} else {
		if m.catalogCursor >= len(r) {
			return
		}
		r = append(r[:m.catalogCursor], r[m.catalogCursor+1:]...)
	}
	m.draft.CodexModelCatalogJSON = string(r)
	m.markDirty("Codex model catalog changed. Save to persist.")
}

func (m *settingsModel) save() {
	if err := m.applyDraftToConfig(); err != nil {
		m.status = "Save failed: " + err.Error()
		return
	}
	if err := config.Save(m.cfg); err != nil {
		m.status = "Save failed: " + err.Error()
		return
	}
	m.loadDraftFromConfig()
	m.dirty = false
	m.status = "Saved settings."
}

func (m *settingsModel) applyDraftToConfig() error {
	if m.cfg == nil {
		return fmt.Errorf("config is required")
	}
	config.Normalize(m.cfg)
	defaultIntegration := strings.ToLower(strings.TrimSpace(m.draft.DefaultIntegration))
	if defaultIntegration != "" && indexOfFold(m.integrationNames, defaultIntegration) < 0 {
		return fmt.Errorf("default integration %q is not available", defaultIntegration)
	}
	defaultProfile := strings.TrimSpace(m.draft.DefaultProfile)
	if defaultProfile == "" {
		return fmt.Errorf("default profile is required")
	}
	if err := m.cfg.SetDefaultProfile(defaultProfile); err != nil {
		return fmt.Errorf("default profile: %w", err)
	}
	m.cfg.DefaultIntegration = defaultIntegration
	m.cfg.Prompts.SetEnabled(m.draft.PromptEnabled)
	m.cfg.History = config.History{
		LastSelection:  strings.ToLower(strings.TrimSpace(m.draft.History.LastSelection)),
		LastModelInput: config.NormalizeModel(m.draft.History.LastModelInput),
		ModelInputs:    config.NormalizeModels(m.draft.History.ModelInputs),
	}
	catalog := strings.TrimSpace(m.draft.CodexModelCatalogJSON)
	if catalog != "" || m.cfg.Integrations["codex"] != nil {
		m.cfg.Integration("codex").ModelCatalogJSON = catalog
	}
	return nil
}

func (m *settingsModel) markDirty(status string) {
	m.dirty = true
	m.status = status
}

func cleanIntegrationNames(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, name := range in {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func defaultIntegrationOptions(integrationNames []string) []string {
	out := make([]string, 0, len(integrationNames)+1)
	out = append(out, "")
	out = append(out, integrationNames...)
	return out
}

func settingsProfileNames(cfg *config.RootConfig) []string {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func effectiveModelForProfile(cfg *config.RootConfig, profileName string) string {
	if cfg == nil {
		return ""
	}
	profile, err := cfg.ProfileByName(profileName)
	if err != nil {
		return ""
	}
	models := config.EffectiveProfileModels(profile)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func boolLabel(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

func moveRuneCursor(cursor int, value string, delta int) int {
	return clampIndexInclusive(cursor+delta, len([]rune(value)))
}
