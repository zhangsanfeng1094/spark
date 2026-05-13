package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/skills"
)

var searchCatalogForTUI = skills.SearchCatalog

type skillSaveFinishedMsg struct {
	Status   string
	Err      error
	Registry *skills.Registry
	Catalog  []skills.CatalogEntry
}

type skillBrowseFocus int

const (
	skillBrowseFocusQuickAdd skillBrowseFocus = iota
	skillBrowseFocusSkills
)

type skillQuickAddItem struct {
	Key         string
	Label       string
	Description string
}

type skillTransferItem struct {
	Key         string
	Label       string
	Description string
}

type skillInstallField struct {
	label string
	value string
}

const (
	skillInstallFieldName = iota
	skillInstallFieldSource
)

type skillManagerModel struct {
	registry *skills.Registry
	names    []string
	selected int
	width    int
	height   int
	status   string

	browseFocus   skillBrowseFocus
	quickAddItems []skillQuickAddItem
	quickAddIndex int

	transferring  bool
	transferItems []skillTransferItem
	transferIndex int

	cataloging   bool
	catalogQuery string
	catalogItems []skills.CatalogEntry
	catalogIndex int

	installing    bool
	installFields []skillInstallField
	installFocus  int

	confirmDelete bool
}

func ManageSkillsDashboard() error {
	registry, err := skills.LoadRegistry()
	if err != nil {
		return err
	}
	m := newSkillManagerModel(registry)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newSkillManagerModel(registry *skills.Registry) *skillManagerModel {
	if registry == nil {
		registry = skills.DefaultRegistry()
	}
	skills.NormalizeRegistry(registry)
	m := &skillManagerModel{
		registry:    registry,
		status:      "Ready.\nInstall a local skill or sync with Codex/Claude.",
		browseFocus: skillBrowseFocusQuickAdd,
		quickAddItems: []skillQuickAddItem{
			{Key: "install_local", Label: "Install Local", Description: "Copy a local skill package into Spark storage"},
			{Key: "browse_catalog", Label: "Browse Catalog", Description: "Search skills.sh and install a listed skill"},
			{Key: "transfer", Label: "Transfer", Description: "Import from or sync to Codex and Claude"},
		},
		transferItems: []skillTransferItem{
			{Key: "import_codex", Label: "Import from Codex", Description: "Load skills from ~/.codex/skills"},
			{Key: "import_claude", Label: "Import from Claude", Description: "Load skills from ~/.claude/skills"},
			{Key: "sync_codex", Label: "Sync to Codex", Description: "Write enabled skills to ~/.codex/skills"},
			{Key: "sync_claude", Label: "Sync to Claude", Description: "Write enabled skills to ~/.claude/skills"},
		},
		installFields: []skillInstallField{
			{label: "Name"},
			{label: "Local Path"},
		},
	}
	m.refreshNames()
	return m
}

func (m *skillManagerModel) Init() tea.Cmd { return nil }

func (m *skillManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case skillSaveFinishedMsg:
		if msg.Err != nil {
			m.status = errorStatus(msg.Err.Error())
			return m, nil
		}
		if msg.Registry != nil {
			m.registry = msg.Registry
			m.refreshNames()
		}
		if msg.Status != "" {
			m.status = msg.Status
		}
		if msg.Catalog != nil {
			m.catalogItems = msg.Catalog
			m.catalogIndex = 0
		}
		return m, nil
	case tea.KeyMsg:
		if m.confirmDelete {
			return m, m.handleConfirmKey(msg)
		}
		if m.cataloging {
			return m, m.handleCatalogKey(msg)
		}
		if m.installing {
			return m, m.handleInstallKey(msg)
		}
		if m.transferring {
			return m, m.handleTransferKey(msg)
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *skillManagerModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "ctrl+c", "q":
		return tea.Quit
	case "tab":
		m.moveBrowseFocus(1)
	case "shift+tab":
		m.moveBrowseFocus(-1)
	case "up", "k":
		m.moveSelection(-1)
	case "down", "j":
		m.moveSelection(1)
	case "enter":
		return m.activateFocusedItem()
	case "a":
		m.openInstallModal()
	case "/":
		m.openCatalogModal()
	case "t":
		m.openTransferMenu()
	case "d", "x":
		if m.currentName() != "" {
			m.confirmDelete = true
			m.status = fmt.Sprintf("Delete %s? Press Y to confirm or N to cancel.", m.currentName())
		}
	case " ":
		if m.browseFocus == skillBrowseFocusSkills && m.currentName() != "" {
			return m.toggleCurrentEnabled()
		}
	}
	return nil
}

func (m *skillManagerModel) handleConfirmKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.confirmDelete = false
		return m.deleteCurrent()
	case "n", "esc", "q":
		m.confirmDelete = false
		m.status = infoStatus("Delete canceled.")
	}
	return nil
}

func (m *skillManagerModel) handleTransferKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "esc", "q":
		m.transferring = false
		m.status = infoStatus("Transfer canceled.")
	case "up", "k":
		if m.transferIndex > 0 {
			m.transferIndex--
		}
	case "down", "j":
		if m.transferIndex < len(m.transferItems)-1 {
			m.transferIndex++
		}
	case "enter":
		return m.activateTransferItem()
	}
	return nil
}

func (m *skillManagerModel) handleInstallKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "esc", "q":
		m.installing = false
		m.status = infoStatus("Install canceled.")
	case "tab":
		m.installFocus = (m.installFocus + 1) % len(m.installFields)
	case "shift+tab":
		m.installFocus--
		if m.installFocus < 0 {
			m.installFocus = len(m.installFields) - 1
		}
	case "backspace":
		value := []rune(m.installFields[m.installFocus].value)
		if len(value) > 0 {
			m.installFields[m.installFocus].value = string(value[:len(value)-1])
		}
	case "enter":
		if m.installFocus == len(m.installFields)-1 {
			return m.installLocalSkill()
		}
		m.installFocus++
	default:
		if len(msg.Runes) > 0 {
			m.installFields[m.installFocus].value += string(msg.Runes)
		}
	}
	return nil
}

func (m *skillManagerModel) handleCatalogKey(msg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(msg.String()) {
	case "esc", "q":
		m.cataloging = false
		m.status = infoStatus("Catalog search canceled.")
	case "up", "k":
		if m.catalogIndex > 0 {
			m.catalogIndex--
		}
	case "down", "j":
		if m.catalogIndex < len(m.catalogItems)-1 {
			m.catalogIndex++
		}
	case "backspace":
		value := []rune(m.catalogQuery)
		if len(value) > 0 {
			m.catalogQuery = string(value[:len(value)-1])
		}
	case "enter":
		if len(m.catalogItems) > 0 {
			return m.installCatalogSelection()
		}
		return m.searchCatalog()
	default:
		if len(msg.Runes) > 0 {
			m.catalogQuery += string(msg.Runes)
		}
	}
	return nil
}

func (m *skillManagerModel) moveBrowseFocus(delta int) {
	focuses := []skillBrowseFocus{skillBrowseFocusQuickAdd, skillBrowseFocusSkills}
	current := 0
	for i, focus := range focuses {
		if m.browseFocus == focus {
			current = i
			break
		}
	}
	next := (current + delta) % len(focuses)
	if next < 0 {
		next += len(focuses)
	}
	m.browseFocus = focuses[next]
}

func (m *skillManagerModel) moveSelection(delta int) {
	switch m.browseFocus {
	case skillBrowseFocusQuickAdd:
		m.quickAddIndex = clampIndex(m.quickAddIndex+delta, len(m.quickAddItems))
	case skillBrowseFocusSkills:
		m.selected = clampIndex(m.selected+delta, len(m.names))
	}
}

func (m *skillManagerModel) activateFocusedItem() tea.Cmd {
	switch m.browseFocus {
	case skillBrowseFocusQuickAdd:
		item := m.quickAddItems[m.quickAddIndex]
		switch item.Key {
		case "install_local":
			m.openInstallModal()
		case "browse_catalog":
			m.openCatalogModal()
		case "transfer":
			m.openTransferMenu()
		}
	case skillBrowseFocusSkills:
		return m.toggleCurrentEnabled()
	}
	return nil
}

func (m *skillManagerModel) openInstallModal() {
	m.installing = true
	m.installFocus = 0
	for i := range m.installFields {
		m.installFields[i].value = ""
	}
	m.status = infoStatus("Install a local skill by name and path.")
}

func (m *skillManagerModel) openCatalogModal() {
	m.cataloging = true
	m.catalogQuery = ""
	m.catalogItems = nil
	m.catalogIndex = 0
	m.status = infoStatus("Search skills.sh by name or repo.")
}

func (m *skillManagerModel) openTransferMenu() {
	m.transferring = true
	m.transferIndex = 0
	m.status = infoStatus("Choose a skill transfer action.")
}

func (m *skillManagerModel) activateTransferItem() tea.Cmd {
	if len(m.transferItems) == 0 {
		return nil
	}
	item := m.transferItems[m.transferIndex]
	switch item.Key {
	case "import_codex":
		m.transferring = false
		m.status = infoStatus("Importing skills from Codex...")
		return m.importFromPeer("codex")
	case "import_claude":
		m.transferring = false
		m.status = infoStatus("Importing skills from Claude...")
		return m.importFromPeer("claude")
	case "sync_codex":
		m.transferring = false
		m.status = infoStatus("Syncing skills to Codex...")
		return m.syncToPeer("codex")
	case "sync_claude":
		m.transferring = false
		m.status = infoStatus("Syncing skills to Claude...")
		return m.syncToPeer("claude")
	default:
		return nil
	}
}

func (m *skillManagerModel) refreshNames() {
	m.names = m.names[:0]
	for name := range m.registry.Skills {
		m.names = append(m.names, name)
	}
	sort.Strings(m.names)
	if m.selected >= len(m.names) && len(m.names) > 0 {
		m.selected = len(m.names) - 1
	}
	if len(m.names) == 0 {
		m.selected = 0
	}
}

func (m *skillManagerModel) currentName() string {
	if m.selected < 0 || m.selected >= len(m.names) {
		return ""
	}
	return m.names[m.selected]
}

func (m *skillManagerModel) currentEntry() *skills.SkillEntry {
	name := m.currentName()
	if name == "" {
		return nil
	}
	return m.registry.Skills[name]
}

func (m *skillManagerModel) View() string {
	if m.width == 0 {
		return "loading..."
	}
	header := pmTitleStyle.Render("Skill Manager")
	subtitle := lipgloss.NewStyle().Foreground(colorDim).Render("Manage installed skills, local installs, and Codex/Claude sync.")
	leftW := 42
	if m.width > 0 && leftW > m.width/2 {
		leftW = m.width / 2
	}
	if leftW < 34 {
		leftW = 34
	}
	rightW := m.width - leftW - 4
	if rightW < 50 {
		rightW = 50
	}
	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.leftPaneStyle(leftW).Render(m.renderSkillList()),
		m.rightPaneStyle(rightW).Render(m.renderDetails()),
	)
	statusBar := pmStatusBarStyle.Width(m.width - 4).Render(m.renderStatusBar())
	return fitToViewportHeight(pmAppStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, subtitle, body, statusBar)), m.height)
}

func (m *skillManagerModel) leftPaneStyle(width int) lipgloss.Style {
	style := pmPanelStyle.Width(width)
	if !m.installing && !m.transferring && !m.cataloging {
		return pmFocusedPanelStyle.Width(width)
	}
	return style
}

func (m *skillManagerModel) rightPaneStyle(width int) lipgloss.Style {
	style := pmPanelStyle.Width(width)
	if m.installing || m.transferring || m.cataloging {
		return pmFocusedPanelStyle.Width(width)
	}
	return style
}

func (m *skillManagerModel) renderSkillList() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Actions"),
		"",
	}
	for i, item := range m.quickAddItems {
		label := fmt.Sprintf("%s\n%s", item.Label, lipgloss.NewStyle().Foreground(colorDim).Render(item.Description))
		if m.browseFocus == skillBrowseFocusQuickAdd && i == m.quickAddIndex {
			lines = append(lines, pmFocusedItemStyle.Width(max(36, 42-4)).Render(label))
		} else {
			lines = append(lines, pmItemStyle.Width(max(36, 42-4)).Render(label))
		}
	}
	lines = append(lines, "", lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Installed Skills"), "")
	if len(m.names) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render("No installed skills yet."))
	} else {
		for i, name := range m.names {
			entry := m.registry.Skills[name]
			state := "disabled"
			if entry.Enabled {
				state = "enabled"
			}
			row := fmt.Sprintf("%s\n%s", name, lipgloss.NewStyle().Foreground(colorDim).Render(fmt.Sprintf("%s • %s", entry.SourceType, state)))
			if m.browseFocus == skillBrowseFocusSkills && i == m.selected {
				lines = append(lines, pmFocusedItemStyle.Width(max(36, 42-4)).Render(row))
			} else {
				lines = append(lines, pmItemStyle.Width(max(36, 42-4)).Render(row))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (m *skillManagerModel) renderDetails() string {
	if m.installing {
		return m.renderInstallModal()
	}
	if m.cataloging {
		return m.renderCatalogModal()
	}
	if m.transferring {
		return m.renderTransferMenu()
	}
	entry := m.currentEntry()
	if entry == nil {
		return m.renderEmptyState()
	}
	status := "Disabled"
	if entry.Enabled {
		status = "Enabled"
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Overview"),
		lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(entry.Name),
		"",
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("Managed: %t", entry.Managed),
		fmt.Sprintf("Source: %s", entry.SourceType),
		fmt.Sprintf("Origin: %s", entry.Source),
	}
	if entry.InstalledPath != "" {
		lines = append(lines, fmt.Sprintf("Installed Path: %s", entry.InstalledPath))
	}
	if len(entry.Targets) > 0 {
		lines = append(lines, fmt.Sprintf("Targets: %s", strings.Join(entry.Targets, ", ")))
	}
	if entry.Ref != "" {
		lines = append(lines, fmt.Sprintf("Ref: %s", entry.Ref))
	}
	if entry.Manifest.Description != "" {
		lines = append(lines, "", lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Description"), entry.Manifest.Description)
	}
	lines = append(lines, "", lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Actions"), "Space toggle enabled • D delete • T transfer")
	return strings.Join(lines, "\n")
}

func (m *skillManagerModel) renderEmptyState() string {
	registryPath, _ := skills.RegistryPath()
	return strings.Join([]string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("No skills yet"),
		"",
		"Install a local skill from the left action list.",
		fmt.Sprintf("Spark stores skill metadata in %s", registryPath),
		"Installed content lives under ~/.spark/skills/.",
	}, "\n")
}

func (m *skillManagerModel) renderInstallModal() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Install Local Skill"),
		"",
		"Copy a local skill package into Spark storage.",
		"",
	}
	for i, field := range m.installFields {
		value := field.value
		if value == "" {
			value = lipgloss.NewStyle().Foreground(colorDim).Render(" ")
		}
		style := pmInputStyle
		if i == m.installFocus {
			style = pmFocusedInputStyle
		}
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, pmLabelStyle.Render(field.label+":"), style.Width(max(32, m.width/3)).Render(value)))
	}
	lines = append(lines, "", "Tab move • Enter next/save • Esc cancel")
	return strings.Join(lines, "\n")
}

func (m *skillManagerModel) renderCatalogModal() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Browse Catalog"),
		"",
		"Search skills.sh and install the selected result.",
		"",
		lipgloss.JoinHorizontal(lipgloss.Top, pmLabelStyle.Render("Query:"), pmFocusedInputStyle.Width(max(32, m.width/3)).Render(displaySkillValue(m.catalogQuery))),
		"",
	}
	if len(m.catalogItems) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render("Press Enter to search the current query."))
	} else {
		for i, item := range m.catalogItems {
			row := fmt.Sprintf("%s\n%s", item.Name, lipgloss.NewStyle().Foreground(colorDim).Render(item.Repo))
			if i == m.catalogIndex {
				lines = append(lines, pmFocusedItemStyle.Width(max(36, m.width/3)).Render(row))
			} else {
				lines = append(lines, pmItemStyle.Width(max(36, m.width/3)).Render(row))
			}
		}
	}
	lines = append(lines, "", "Type search • Enter search/install • Up/Down select • Esc cancel")
	return strings.Join(lines, "\n")
}

func (m *skillManagerModel) renderTransferMenu() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Transfer Skills"),
		"",
	}
	for i, item := range m.transferItems {
		row := fmt.Sprintf("%s\n%s", item.Label, lipgloss.NewStyle().Foreground(colorDim).Render(item.Description))
		if i == m.transferIndex {
			lines = append(lines, pmFocusedItemStyle.Width(max(36, m.width/3)).Render(row))
		} else {
			lines = append(lines, pmItemStyle.Width(max(36, m.width/3)).Render(row))
		}
	}
	lines = append(lines, "", "Up/Down select • Enter run • Esc cancel")
	return strings.Join(lines, "\n")
}

func displaySkillValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return lipgloss.NewStyle().Foreground(colorDim).Render(" ")
	}
	return v
}

func (m *skillManagerModel) renderStatusBar() string {
	text := m.status
	if text == "" {
		text = "Ready."
	}
	return strings.TrimSpace(text) + "    " + pmStatusLogStyle.Render(m.contextHelpText())
}

func (m *skillManagerModel) contextHelpText() string {
	if m.confirmDelete {
		return "Y confirm • N cancel"
	}
	if m.cataloging {
		return "Type search • Enter search/install • Up/Down select • Esc cancel"
	}
	if m.installing {
		return "Tab move • Enter next/save • Esc cancel"
	}
	if m.transferring {
		return "Up/Down select • Enter run • Esc cancel"
	}
	switch m.browseFocus {
	case skillBrowseFocusQuickAdd:
		return "Enter run action • Tab next zone • A install • / catalog • T transfer"
	case skillBrowseFocusSkills:
		return "Up/Down select • Space toggle • D delete • Tab next zone"
	default:
		return "Q quit"
	}
}

func (m *skillManagerModel) installLocalSkill() tea.Cmd {
	name := strings.TrimSpace(m.installFields[skillInstallFieldName].value)
	source := strings.TrimSpace(m.installFields[skillInstallFieldSource].value)
	m.installing = false
	if name == "" {
		m.status = errorStatus("Skill name cannot be empty.")
		return nil
	}
	if source == "" {
		m.status = errorStatus("Local path cannot be empty.")
		return nil
	}
	m.status = infoStatus(fmt.Sprintf("Installing %s...", name))
	return func() tea.Msg {
		if _, err := skills.Install(skills.InstallOptions{
			Name:       name,
			SourceType: skills.SourceTypeLocal,
			Source:     source,
		}); err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		registry, err := skills.LoadRegistry()
		if err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		return skillSaveFinishedMsg{
			Status:   successStatus(fmt.Sprintf("Installed skill %s.", skills.NormalizeName(name))),
			Registry: registry,
		}
	}
}

func (m *skillManagerModel) searchCatalog() tea.Cmd {
	query := strings.TrimSpace(m.catalogQuery)
	if query == "" {
		m.status = errorStatus("Catalog query cannot be empty.")
		return nil
	}
	m.status = infoStatus(fmt.Sprintf("Searching catalog for %s...", query))
	return func() tea.Msg {
		results, err := searchCatalogForTUI(query)
		if err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		return skillSaveFinishedMsg{
			Status:  successStatus(fmt.Sprintf("Found %d catalog skill(s).", len(results))),
			Catalog: results,
		}
	}
}

func (m *skillManagerModel) installCatalogSelection() tea.Cmd {
	if m.catalogIndex < 0 || m.catalogIndex >= len(m.catalogItems) {
		return nil
	}
	selected := m.catalogItems[m.catalogIndex]
	m.cataloging = false
	m.status = infoStatus(fmt.Sprintf("Installing %s from catalog...", selected.Name))
	return func() tea.Msg {
		if _, err := skills.InstallFromCatalog(selected.Name); err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		registry, err := skills.LoadRegistry()
		if err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		return skillSaveFinishedMsg{
			Status:   successStatus(fmt.Sprintf("Installed %s from catalog.", selected.Name)),
			Registry: registry,
		}
	}
}

func (m *skillManagerModel) toggleCurrentEnabled() tea.Cmd {
	entry := m.currentEntry()
	if entry == nil {
		return nil
	}
	next := !entry.Enabled
	label := "disabled"
	if next {
		label = "enabled"
	}
	m.status = infoStatus(fmt.Sprintf("Updating %s...", entry.Name))
	return func() tea.Msg {
		if err := skills.SetEnabled(entry.Name, next); err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		registry, err := skills.LoadRegistry()
		if err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		return skillSaveFinishedMsg{
			Status:   successStatus(fmt.Sprintf("%s %s.", strings.Title(label), entry.Name)),
			Registry: registry,
		}
	}
}

func (m *skillManagerModel) deleteCurrent() tea.Cmd {
	name := m.currentName()
	if name == "" {
		return nil
	}
	m.status = infoStatus(fmt.Sprintf("Removing %s...", name))
	return func() tea.Msg {
		if err := skills.Remove(name); err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		registry, err := skills.LoadRegistry()
		if err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		return skillSaveFinishedMsg{
			Status:   successStatus(fmt.Sprintf("Removed skill %s.", name)),
			Registry: registry,
		}
	}
}

func (m *skillManagerModel) importFromPeer(peer string) tea.Cmd {
	return func() tea.Msg {
		result, err := skills.ImportFromPeer(peer, "")
		if err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		registry, err := skills.LoadRegistry()
		if err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		return skillSaveFinishedMsg{
			Status:   successStatus(fmt.Sprintf("Imported %d skill(s) from %s.", result.Added, strings.Title(peer))),
			Registry: registry,
		}
	}
}

func (m *skillManagerModel) syncToPeer(peer string) tea.Cmd {
	return func() tea.Msg {
		if err := skills.SyncToPeer(peer, ""); err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		registry, err := skills.LoadRegistry()
		if err != nil {
			return skillSaveFinishedMsg{Err: err}
		}
		return skillSaveFinishedMsg{
			Status:   successStatus(fmt.Sprintf("Synced skills to %s.", strings.Title(peer))),
			Registry: registry,
		}
	}
}
