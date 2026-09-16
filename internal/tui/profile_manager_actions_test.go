package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
)

func TestSetCurrentProfileDefaultPersistsImmediately(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://api.openai.com/v1"},
			"backup":  {OpenAIBaseURL: "https://example.com/v1"},
		},
	}
	m := newPMModel(cfg)
	m.selectByName("backup")

	m.setCurrentProfileDefault()

	if got := m.cfg.DefaultProfile; got != "backup" {
		t.Fatalf("DefaultProfile=%q, want backup", got)
	}
	if m.dirty {
		t.Fatal("expected default change to be saved immediately")
	}
	if strings.Contains(m.status, "Save to persist") {
		t.Fatalf("expected persisted status, got %q", m.status)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := loaded.DefaultProfile; got != "backup" {
		t.Fatalf("persisted DefaultProfile=%q, want backup", got)
	}
}

func TestCopySelectedProfileDeepCopiesV2AndThinking(t *testing.T) {
	budget := 4096
	cfg := &config.RootConfig{
		DefaultProfile: "source",
		Profiles: map[string]*config.Profile{
			"source": {
				Endpoint: "https://api.openai.com/v1",
				Protocol: config.ProtocolOpenAIChat,
				Credential: config.CredentialConfig{
					Mode:    config.CredentialModeAuto,
					AuthRef: "openai:default",
				},
				Thinking: &config.ThinkingConfig{
					Mode:         "client",
					Effort:       "high",
					BudgetTokens: &budget,
				},
				OpenAIBaseURL: "https://api.openai.com/v1",
				APIKey:        "sk-test-key",
				AuthProvider:  "openai",
				AuthRef:       "openai:default",
				Models:        []string{"gpt-4o", "gpt-4o-mini"},
				DefaultModel:  "gpt-4o",
			},
		},
	}
	m := newPMModel(cfg)
	m.selectByName("source")
	m.copySelectedProfile()

	copyName := "source-copy"
	copied, ok := m.cfg.Profiles[copyName]
	if !ok {
		t.Fatalf("expected profile %q to exist", copyName)
	}

	if copied.Endpoint != "https://api.openai.com/v1" {
		t.Errorf("expected Endpoint copied, got %q", copied.Endpoint)
	}
	if copied.Protocol != config.ProtocolOpenAIChat {
		t.Errorf("expected Protocol copied, got %q", copied.Protocol)
	}
	if copied.AuthProvider != "openai" {
		t.Errorf("expected AuthProvider copied, got %q", copied.AuthProvider)
	}
	if copied.Credential.AuthRef != "openai:default" {
		t.Errorf("expected Credential copied, got %+v", copied.Credential)
	}
	if copied.Thinking == nil {
		t.Fatal("expected Thinking config copied, got nil")
	}
	if copied.Thinking.Mode != "client" || copied.Thinking.Effort != "high" || copied.Thinking.BudgetTokens == nil || *copied.Thinking.BudgetTokens != 4096 {
		t.Errorf("Thinking config mismatch: %+v", copied.Thinking)
	}

	// Mutate source thinking to ensure deep copy
	*m.cfg.Profiles["source"].Thinking.BudgetTokens = 2048
	if *copied.Thinking.BudgetTokens != 4096 {
		t.Errorf("expected deep copy of BudgetTokens, but copied was mutated to %d", *copied.Thinking.BudgetTokens)
	}
}

func TestSetCurrentProfileDefaultAppliesInFlightEdits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://api.openai.com/v1"},
			"backup":  {OpenAIBaseURL: "https://example.com/v1"},
		},
	}
	m := newPMModel(cfg)
	m.selectByName("backup")

	// Simulate editing baseURL in the form
	m.fields[pmFieldOpenAIBaseURL].value = "https://new-api.example.com/v1"
	m.dirty = true

	m.setCurrentProfileDefault()

	if m.dirty {
		t.Fatal("expected dirty to be false after default save")
	}
	if got := m.cfg.Profiles["backup"].OpenAIBaseURL; got != "https://new-api.example.com/v1" {
		t.Fatalf("expected OpenAIBaseURL updated in memory, got %q", got)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := loaded.Profiles["backup"].OpenAIBaseURL; got != "https://new-api.example.com/v1" {
		t.Fatalf("expected OpenAIBaseURL persisted to disk, got %q", got)
	}
}

func TestSaveUpdatesBaseURLWhenEndpointAlreadySet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				Endpoint:      "https://old-endpoint.example.com/v1",
				OpenAIBaseURL: "https://old-endpoint.example.com/v1",
			},
		},
	}
	m := newPMModel(cfg)
	m.selectByName("default")

	// Verify loaded field
	if got := m.fields[pmFieldOpenAIBaseURL].value; got != "https://old-endpoint.example.com/v1" {
		t.Fatalf("expected loaded Base URL to be old endpoint, got %q", got)
	}

	// Update Base URL
	m.fields[pmFieldOpenAIBaseURL].value = "https://new-endpoint.example.com/v1"
	m.dirty = true
	m.save()

	if m.dirty {
		t.Fatal("expected dirty to be false after save")
	}

	// In memory
	p := m.cfg.Profiles["default"]
	if p.Endpoint != "https://new-endpoint.example.com/v1" {
		t.Fatalf("expected Endpoint updated in memory, got %q", p.Endpoint)
	}
	if p.OpenAIBaseURL != "https://new-endpoint.example.com/v1" {
		t.Fatalf("expected OpenAIBaseURL updated in memory, got %q", p.OpenAIBaseURL)
	}

	// Persisted to disk
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	loadedP := loaded.Profiles["default"]
	if loadedP.Endpoint != "https://new-endpoint.example.com/v1" {
		t.Fatalf("expected Endpoint persisted to disk, got %q", loadedP.Endpoint)
	}
	if loadedP.OpenAIBaseURL != "https://new-endpoint.example.com/v1" {
		t.Fatalf("expected OpenAIBaseURL persisted to disk, got %q", loadedP.OpenAIBaseURL)
	}
}

func TestDirtyStateQuitProtection(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://example.com/v1"},
		},
	}
	m := newPMModel(cfg)
	m.focusArea = pmFocusProfiles
	m.dirty = true

	// First Esc press should prompt/warn and not quit
	cmd, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled")
	}
	if cmd != nil {
		t.Fatal("expected first Esc when dirty not to quit immediately")
	}
	if !m.confirmQuit {
		t.Fatal("expected confirmQuit to be set")
	}
	if !strings.Contains(m.status, "Unsaved changes") {
		t.Fatalf("expected warning status, got %q", m.status)
	}

	// Second Esc press should confirm quit
	cmd, handled = m.handleMainKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !handled || cmd == nil {
		t.Fatal("expected second Esc to quit")
	}
}

func TestModelsModalSpaceSetsDefaultModel(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "p1",
		Profiles: map[string]*config.Profile{
			"p1": {},
		},
	}
	m := newPMModel(cfg)
	m.modelsDraft = []string{"model-a", "model-b"}
	m.defaultModel = "model-a"
	m.openModelsModal()

	// Switch focus away from search to the models list (Tab)
	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.modelSearchFocused {
		t.Fatal("expected search not focused after Tab")
	}

	// Move cursor down to model-b
	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.modalCursor != 1 {
		t.Fatalf("expected modalCursor=1, got %d", m.modalCursor)
	}

	// Space should set model-b as default
	_ = m.handleModalKey(tea.KeyMsg{Type: tea.KeySpace})
	if m.defaultModel != "model-b" {
		t.Fatalf("expected defaultModel to be model-b, got %q", m.defaultModel)
	}
	if m.modelModalNote != "Default model set." {
		t.Fatalf("unexpected modal note: %q", m.modelModalNote)
	}
}

func TestEnterKeyAdvancesFormField(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	}
	m := newPMModel(cfg)
	m.focusArea = pmFocusFields
	m.focusField = pmFieldProfileName // field 0

	// Pressing Enter on Profile Name (text input) should advance to field 1 (Provider Type)
	_, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("expected Enter on text field to be handled")
	}
	if m.focusField != pmFieldProviderType {
		t.Fatalf("expected focusField to advance to pmFieldProviderType (1), got %d", m.focusField)
	}

	// Pressing Enter on Provider Type opens modal
	_, handled = m.handleMainKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("expected Enter on select field to be handled")
	}
	if !m.modalOpen || m.modalKind != pmModalKindProviderType {
		t.Fatalf("expected Provider Type modal to open, got modalOpen=%v modalKind=%d", m.modalOpen, m.modalKind)
	}
}

func TestCycleSelectFieldInlineWithArrowKeys(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				OpenAIBaseURL: "https://api.openai.com/v1",
				Thinking: &config.ThinkingConfig{
					Mode: "client",
				},
			},
		},
	}
	m := newPMModel(cfg)
	m.focusArea = pmFocusFields
	m.focusField = pmFieldThinkingMode

	// Current thinking mode is "client"
	if m.fields[pmFieldThinkingMode].value != "client" {
		t.Fatalf("expected initial thinking mode 'client', got %q", m.fields[pmFieldThinkingMode].value)
	}

	// Pressing Right should cycle to "auto"
	_, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeyRight})
	if !handled {
		t.Fatal("expected Right on select field to be handled")
	}
	if m.fields[pmFieldThinkingMode].value != "auto" {
		t.Fatalf("expected thinking mode to cycle to 'auto', got %q", m.fields[pmFieldThinkingMode].value)
	}

	// Pressing Left should cycle back to "client"
	_, handled = m.handleMainKey(tea.KeyMsg{Type: tea.KeyLeft})
	if !handled {
		t.Fatal("expected Left on select field to be handled")
	}
	if m.fields[pmFieldThinkingMode].value != "client" {
		t.Fatalf("expected thinking mode to cycle back to 'client', got %q", m.fields[pmFieldThinkingMode].value)
	}

	// Test Provider Type cycling
	m.focusField = pmFieldProviderType
	origProvider := m.fields[pmFieldProviderType].value
	_, handled = m.handleMainKey(tea.KeyMsg{Type: tea.KeyRight})
	if !handled {
		t.Fatal("expected Right on provider type to be handled")
	}
	if m.fields[pmFieldProviderType].value == origProvider {
		t.Fatalf("expected provider type to cycle to a different provider from %q", origProvider)
	}
}

func TestCtrlSSavesProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	}
	m := newPMModel(cfg)
	m.focusArea = pmFocusFields
	m.focusField = pmFieldOpenAIBaseURL
	m.fields[pmFieldOpenAIBaseURL].value = "https://saved-via-ctrls.example.com/v1"
	m.dirty = true

	// Press Ctrl+S
	_, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !handled {
		t.Fatal("expected Ctrl+S to be handled")
	}
	if m.dirty {
		t.Fatal("expected dirty to be cleared after Ctrl+S save")
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := loaded.Profiles["default"].OpenAIBaseURL; got != "https://saved-via-ctrls.example.com/v1" {
		t.Fatalf("persisted baseURL=%q, want 'https://saved-via-ctrls.example.com/v1'", got)
	}
}

func TestProfileManagerTextInputCapabilities(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	}
	m := newPMModel(cfg)
	m.focusArea = pmFocusFields
	m.focusField = pmFieldOpenAIBaseURL
	m.updateFocus()

	// Test Ctrl+W (delete word backward)
	m.fields[pmFieldOpenAIBaseURL].value = "hello world"
	m.fields[pmFieldOpenAIBaseURL].cursor = len([]rune("hello world"))
	m.syncInputs()

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := m.fields[pmFieldOpenAIBaseURL].value; got != "hello " {
		t.Fatalf("expected 'hello ' after Ctrl+W, got %q", got)
	}

	// Test Ctrl+U (delete before cursor)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if got := m.fields[pmFieldOpenAIBaseURL].value; got != "" {
		t.Fatalf("expected empty string after Ctrl+U, got %q", got)
	}

	// Typing runes
	for _, r := range []rune("https://api.test.com") {
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.fields[pmFieldOpenAIBaseURL].value; got != "https://api.test.com" {
		t.Fatalf("expected 'https://api.test.com', got %q", got)
	}
}

func TestModelsModalTextInputCapabilities(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {},
		},
	}
	m := newPMModel(cfg)
	m.modelsDraft = []string{"gpt-4o", "claude-3-5-sonnet"}
	m.openModelsModal()

	// 1. Search input capabilities: typing, Ctrl+W, Delete
	for _, r := range []rune("claude test") {
		m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.modelSearchQuery != "claude test" {
		t.Fatalf("expected search query 'claude test', got %q", m.modelSearchQuery)
	}

	// Ctrl+W deletes previous word
	m.handleModalKey(tea.KeyMsg{Type: tea.KeyCtrlW})
	if m.modelSearchQuery != "claude " {
		t.Fatalf("expected 'claude ' after Ctrl+W, got %q", m.modelSearchQuery)
	}

	// Delete clears search
	m.handleModalKey(tea.KeyMsg{Type: tea.KeyDelete})
	if m.modelSearchQuery != "" {
		t.Fatalf("expected empty search after Delete, got %q", m.modelSearchQuery)
	}

	// 2. Add Model with textinput: F6 -> type name -> Enter
	m.startModelAdd()
	if !m.modelEditMode {
		t.Fatal("expected modelEditMode to be true")
	}
	for _, r := range []rune("deepseek-r1") {
		m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.modelEditBuffer != "deepseek-r1" {
		t.Fatalf("expected editBuffer 'deepseek-r1', got %q", m.modelEditBuffer)
	}
	m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.modelEditMode {
		t.Fatal("expected modelEditMode to be false after Enter")
	}
	found := false
	for _, mdl := range m.modelItems {
		if mdl == "deepseek-r1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'deepseek-r1' in modelItems, got %v", m.modelItems)
	}

	// 3. Edit Model with textinput: F7 -> Ctrl+W -> type new suffix -> Enter
	m.modalCursor = 0 // gpt-4o
	m.startModelEdit()
	if m.modelEditBuffer != m.modelItems[0] {
		t.Fatalf("expected editBuffer %q, got %q", m.modelItems[0], m.modelEditBuffer)
	}
	m.handleModalKey(tea.KeyMsg{Type: tea.KeyCtrlW}) // deletes "4o" or similar
	for _, r := range []rune("mini") {
		m.handleModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.modelEditMode {
		t.Fatal("expected modelEditMode to be false after Enter")
	}
}

func TestInlinePillsAndStepperKeyAndMouseInteractions(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				OpenAIBaseURL: "https://api.openai.com/v1",
				Thinking: &config.ThinkingConfig{
					Mode:   "client",
					Effort: "low",
				},
			},
		},
	}
	m := newPMModel(cfg)
	m.width = 100
	m.height = 30
	_ = m.View()

	m.focusArea = pmFocusFields

	// 1. Test Space key on Thinking Mode cycles inline
	m.focusField = pmFieldThinkingMode
	if m.fields[pmFieldThinkingMode].value != "client" {
		t.Fatalf("expected initial thinking mode 'client', got %q", m.fields[pmFieldThinkingMode].value)
	}
	_, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeySpace})
	if !handled {
		t.Fatal("expected Space on thinking mode to be handled")
	}
	if m.modalOpen {
		t.Fatal("expected thinking mode Space not to open a modal")
	}
	if m.fields[pmFieldThinkingMode].value != "auto" {
		t.Fatalf("expected thinking mode 'auto' after Space, got %q", m.fields[pmFieldThinkingMode].value)
	}

	// 2. Test Space key on Thinking Effort steps inline
	m.focusField = pmFieldThinkingEffort
	if m.fields[pmFieldThinkingEffort].value != "low" {
		t.Fatalf("expected initial thinking effort 'low', got %q", m.fields[pmFieldThinkingEffort].value)
	}
	_, handled = m.handleMainKey(tea.KeyMsg{Type: tea.KeySpace})
	if !handled {
		t.Fatal("expected Space on thinking effort to be handled")
	}
	if m.modalOpen {
		t.Fatal("expected thinking effort Space not to open a modal")
	}
	if m.fields[pmFieldThinkingEffort].value != "medium" {
		t.Fatalf("expected thinking effort 'medium' after Space, got %q", m.fields[pmFieldThinkingEffort].value)
	}

	// 3. Test Mouse click on Thinking Mode cycles inline
	_ = m.View()
	x := m.rightContentX + pmLabelWidth + 2
	yMode := m.rightContentY + m.fieldStartRelY[pmFieldThinkingMode]
	m.handleMainMouse(tea.MouseMsg{X: x, Y: yMode})
	if m.modalOpen {
		t.Fatal("expected clicking thinking mode not to open modal")
	}
	if m.fields[pmFieldThinkingMode].value != "force" {
		t.Fatalf("expected thinking mode 'force' after mouse click, got %q", m.fields[pmFieldThinkingMode].value)
	}

	// 4. Test Mouse click on Thinking Effort steps inline
	yEffort := m.rightContentY + m.fieldStartRelY[pmFieldThinkingEffort]
	m.handleMainMouse(tea.MouseMsg{X: x, Y: yEffort})
	if m.modalOpen {
		t.Fatal("expected clicking thinking effort not to open modal")
	}
	if m.fields[pmFieldThinkingEffort].value != "high" {
		t.Fatalf("expected thinking effort 'high' after mouse click, got %q", m.fields[pmFieldThinkingEffort].value)
	}
}

func TestAnchoredProviderTypeDropdown(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				OpenAIBaseURL: "https://api.openai.com/v1",
			},
		},
	}
	m := newPMModel(cfg)
	m.width = 100
	m.height = 30
	bg := m.View()

	// Open Provider Type modal
	m.openProviderTypeModal()
	if !m.modalOpen || m.modalKind != pmModalKindProviderType {
		t.Fatalf("expected modalOpen=true and modalKind=pmModalKindProviderType")
	}

	// Render overlay
	rendered := m.overlayModal(bg)
	if !strings.Contains(rendered, "Provider Type:") {
		t.Fatalf("expected dropdown to contain 'Provider Type:', got %s", rendered)
	}
	if m.modalX <= 0 || m.modalY <= 0 {
		t.Fatalf("expected anchored modalX > 0 and modalY > 0, got X=%d, Y=%d", m.modalX, m.modalY)
	}

	// Click on second provider option (anthropic)
	optionY := m.modalOptionStartY + 1
	m.handleModalMouse(tea.MouseMsg{X: m.modalX + 2, Y: optionY})
	if m.modalOpen {
		t.Fatal("expected dropdown to close after selecting an option")
	}
	if m.fields[pmFieldProviderType].value != m.providerOptions[1].name {
		t.Fatalf("expected provider %q, got %q", m.providerOptions[1].name, m.fields[pmFieldProviderType].value)
	}

	// Reopen dropdown and click outside to close
	m.openProviderTypeModal()
	_ = m.overlayModal(bg)
	if !m.modalOpen {
		t.Fatal("expected modalOpen=true after reopen")
	}
	// Click outside (e.g. at 0, 0)
	m.handleModalMouse(tea.MouseMsg{X: 0, Y: 0})
	if m.modalOpen {
		t.Fatal("expected clicking outside to close the dropdown")
	}
}

func TestProfileManagerSaveUpdatesAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.RootConfig{
		DefaultProfile: "my-profile",
		Profiles: map[string]*config.Profile{
			"my-profile": {
				Endpoint: "https://api.openai.com/v1",
				Protocol: config.ProtocolOpenAIChat,
				Credential: config.CredentialConfig{
					Mode:   config.CredentialModeAPIKey,
					APIKey: "old-secret-key",
				},
				OpenAIBaseURL: "https://api.openai.com/v1",
				APIKey:        "old-secret-key",
			},
		},
	}
	m := newPMModel(cfg)
	m.selectByName("my-profile")
	m.fields[pmFieldOpenAIAPIKey].value = "new-secret-key"
	m.save()

	p := m.cfg.Profiles["my-profile"]
	if p.APIKey != "new-secret-key" {
		t.Fatalf("expected p.APIKey 'new-secret-key', got %q", p.APIKey)
	}
	if p.Credential.APIKey != "new-secret-key" {
		t.Fatalf("expected p.Credential.APIKey 'new-secret-key', got %q", p.Credential.APIKey)
	}
	if p.EffectiveAPIKey() != "new-secret-key" {
		t.Fatalf("expected EffectiveAPIKey 'new-secret-key', got %q", p.EffectiveAPIKey())
	}
}
