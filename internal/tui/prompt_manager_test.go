package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
)

func TestPromptManagerEmptyStateAndAddAction(t *testing.T) {
	m := newPromptManagerModel(&config.RootConfig{})
	m.width = 120
	m.height = 32

	view := StripANSI(m.View())
	for _, want := range []string{"Prompt Manager", "[DISABLED", "No presets", "No bindings", "Add", "[Space] ON/OFF", "[S/F2] Save"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got %q", want, view)
		}
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !m.editing || !m.adding || m.editKind != promptEditPreset {
		t.Fatalf("expected add preset editor, editing=%t adding=%t kind=%v", m.editing, m.adding, m.editKind)
	}
}

func TestPromptManagerSpaceTogglesGlobalEnabledAndSavesDraft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newPromptManagerModel(promptTestConfig())
	m.width = 120
	m.height = 32

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.cfg.Prompts.IsEnabled() || !m.dirty {
		t.Fatalf("expected prompt manager to be disabled in dirty draft, enabled=%t dirty=%t", m.cfg.Prompts.IsEnabled(), m.dirty)
	}
	view := StripANSI(m.View())
	for _, want := range []string{"[DISABLED", "Prompt injection is disabled", "* Unsaved changes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected dirty disabled view to contain %q, got %q", want, view)
		}
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Prompts.IsEnabled() {
		t.Fatal("expected draft toggle not to persist before save")
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	loaded, err = config.Load()
	if err != nil {
		t.Fatalf("Load after save failed: %v", err)
	}
	if loaded.Prompts.IsEnabled() || m.dirty {
		t.Fatalf("expected disabled state to persist after save, loaded=%t dirty=%t", loaded.Prompts.IsEnabled(), m.dirty)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !m.cfg.Prompts.IsEnabled() {
		t.Fatal("expected saved manager to remain enabled after second draft toggle? got disabled")
	}
}

func TestPromptManagerRendersPresetAndBindingDetails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	promptPath := filepath.Join(home, ".spark", "p.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("prompt"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	cfg := promptTestConfig()

	m := newPromptManagerModel(cfg)
	m.width = 180
	m.height = 32
	view := StripANSI(m.View())
	for _, want := range []string{"coding", "Resolved", promptPath, "Validation", "ok"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected preset detail to contain %q, got %q", want, view)
		}
	}

	m.section = promptSectionBindings
	view = StripANSI(m.View())
	for _, want := range []string{"codex · gpt-5 · APPEND · ON", "Integration", "codex", "Model", "gpt-5", "Effective Mode", "append", "Enabled", "true"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected binding detail to contain %q, got %q", want, view)
		}
	}
}

func TestPromptManagerPresetDetailsShowBindingsUsingPreset(t *testing.T) {
	m := newPromptManagerModel(promptTestConfig())
	m.width = 180
	m.height = 32

	view := StripANSI(m.View())
	for _, want := range []string{"Used By", "codex · gpt-5 · APPEND · ON"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected preset detail to contain %q, got %q", want, view)
		}
	}
}

func TestPromptManagerAddBindingSelectOptions(t *testing.T) {
	m := newPromptManagerModel(promptTestConfig())
	m.width = 120
	m.height = 32
	m.section = promptSectionBindings
	m.startAddCurrentSection()

	if !m.editing || m.editKind != promptEditBinding {
		t.Fatalf("expected binding editor")
	}
	if got := m.editFields[0].Options; strings.Join(got, ",") != "codex,claude" {
		t.Fatalf("unexpected integration options: %v", got)
	}
	if len(m.editFields) != 4 || m.editFields[3].Label != "Enabled" {
		t.Fatalf("expected binding editor without mode field: %#v", m.editFields)
	}
	if got := m.editFields[3].Value; got != "true" {
		t.Fatalf("expected add binding enabled default true, got %q", got)
	}
	if got := m.editFields[3].Options; strings.Join(got, ",") != "true,false" {
		t.Fatalf("unexpected enabled options: %v", got)
	}
	if got := m.editFields[2].Options; len(got) != 1 || got[0] != "coding" {
		t.Fatalf("unexpected preset options: %v", got)
	}
	view := StripANSI(m.View())
	for _, want := range []string{"Integration", "│", "codex  ▼", "Enabled", "true  ▼"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected add binding form to contain %q, got %q", want, view)
		}
	}
}

func TestPromptManagerSelectFieldUsesModal(t *testing.T) {
	m := newPromptManagerModel(promptTestConfig())
	m.width = 120
	m.height = 32
	m.section = promptSectionBindings
	m.startAddCurrentSection()

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.selectOpen {
		t.Fatal("expected enter on select field to open modal")
	}
	view := StripANSI(m.View())
	for _, want := range []string{"Select Integration", "codex", "claude", "[Enter] Select"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected select modal to contain %q, got %q", want, view)
		}
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.selectOpen {
		t.Fatal("expected select modal to close after confirm")
	}
	if got := m.editFields[0].Value; got != config.PromptIntegrationClaude {
		t.Fatalf("expected selected integration to be claude, got %q", got)
	}
}

func TestPromptManagerEditBindingCanSetEnabledFalse(t *testing.T) {
	m := newPromptManagerModel(promptTestConfig())
	m.section = promptSectionBindings
	m.startEditCurrent()

	if len(m.editFields) != 4 || m.editFields[3].Label != "Enabled" {
		t.Fatalf("expected enabled field in binding editor: %#v", m.editFields)
	}
	m.editFields[3].Value = "false"
	m.applyEditorToDraft()
	if m.cfg.Prompts.Bindings[0].IsEnabled() {
		t.Fatal("expected edited binding to be disabled")
	}
}

func TestPromptManagerTogglesBindingOnlyInBindingsSectionAndSavesDraft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newPromptManagerModel(promptTestConfig())
	m.width = 120
	m.height = 32

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if !m.cfg.Prompts.Bindings[0].IsEnabled() {
		t.Fatal("expected T in presets section not to toggle binding")
	}
	if !strings.Contains(StripANSI(m.status), "Select a binding") {
		t.Fatalf("expected T in presets section to explain no toggle, got %q", m.status)
	}

	m.section = promptSectionBindings
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.cfg.Prompts.Bindings[0].IsEnabled() {
		t.Fatal("expected T in bindings section to disable binding")
	}
	if !m.dirty {
		t.Fatal("expected binding toggle to mark draft dirty")
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Prompts.Bindings) != 0 {
		t.Fatalf("expected unsaved binding toggle not to persist, got %#v", loaded.Prompts.Bindings)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	loaded, err = config.Load()
	if err != nil {
		t.Fatalf("Load after save failed: %v", err)
	}
	if loaded.Prompts.Bindings[0].IsEnabled() {
		t.Fatal("expected binding disabled state to persist")
	}
}

func TestPromptManagerDownCanMoveFromPresetsToBindingsBeforeToggle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newPromptManagerModel(promptTestConfig())
	m.width = 120
	m.height = 32

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.section != promptSectionBindings {
		t.Fatalf("expected down from last preset to select bindings, got section=%v", m.section)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.cfg.Prompts.Bindings[0].IsEnabled() {
		t.Fatal("expected T after moving to bindings to disable binding")
	}
}

func TestPromptManagerSettingsEditsCodexCatalog(t *testing.T) {
	m := newPromptManagerModel(promptTestConfig())
	m.width = 160
	m.height = 32
	m.cfg.Integration("codex").ModelCatalogJSON = "/home/me/.codex/models.json"
	m.section = promptSectionSettings

	view := StripANSI(m.View())
	for _, want := range []string{"Settings", "Codex Catalog", "/home/me/.codex/models.json", `{"models": [...]}`} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected settings view to contain %q, got %q", want, view)
		}
	}

	m.startEditCurrent()
	if !m.editing || m.editKind != promptEditSettings || len(m.editFields) != 1 {
		t.Fatalf("expected settings editor, editing=%t kind=%v fields=%#v", m.editing, m.editKind, m.editFields)
	}
	m.editFields[0].Value = " /home/me/.codex/custom_models.json "
	m.applyEditorToDraft()
	if got := m.cfg.Integration("codex").ModelCatalogJSON; got != "/home/me/.codex/custom_models.json" {
		t.Fatalf("codex catalog draft mismatch: %q", got)
	}
}

func TestPromptManagerSettingsSavesCodexCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newPromptManagerModel(promptTestConfig())
	m.section = promptSectionSettings
	m.startEditCurrent()
	m.editFields[0].Value = "/home/me/.codex/custom_models.json"
	m.applyEditorToDraft()

	if err := m.persistDraft(); err != nil {
		t.Fatalf("persistDraft failed: %v", err)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := loaded.Integration("codex").ModelCatalogJSON; got != "/home/me/.codex/custom_models.json" {
		t.Fatalf("persisted codex catalog mismatch: %q", got)
	}
}

func TestPromptManagerDeleteReferencedPresetShowsError(t *testing.T) {
	m := newPromptManagerModel(promptTestConfig())
	m.section = promptSectionPresets
	m.deleteCurrent()

	if !strings.Contains(StripANSI(m.status), "used by a binding") {
		t.Fatalf("expected referenced preset error, got %q", m.status)
	}
}

func TestRenderPromptManagerSnapshotStates(t *testing.T) {
	view, err := RenderPromptManagerSnapshot(promptTestConfig(), 120, 32, "add-binding")
	if err != nil {
		t.Fatalf("RenderPromptManagerSnapshot failed: %v", err)
	}
	plain := StripANSI(view)
	for _, want := range []string{"Add Binding", "Integration", "Model", "Enabled", "true"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected snapshot to contain %q, got %q", want, plain)
		}
	}
	if strings.Contains(plain, "             Mode │") {
		t.Fatalf("expected add binding snapshot to omit binding mode field, got %q", plain)
	}

	disabled, err := RenderPromptManagerSnapshot(promptTestConfig(), 120, 32, "disabled")
	if err != nil {
		t.Fatalf("RenderPromptManagerSnapshot(disabled) failed: %v", err)
	}
	if plain := StripANSI(disabled); !strings.Contains(plain, "[DISABLED") || !strings.Contains(plain, "Prompt injection is disabled") {
		t.Fatalf("expected disabled snapshot, got %q", plain)
	}

	bindingDisabled, err := RenderPromptManagerSnapshot(promptTestConfig(), 120, 32, "binding-disabled")
	if err != nil {
		t.Fatalf("RenderPromptManagerSnapshot(binding-disabled) failed: %v", err)
	}
	if plain := StripANSI(bindingDisabled); !strings.Contains(plain, "OFF") || !strings.Contains(plain, "will not inject") {
		t.Fatalf("expected binding-disabled snapshot, got %q", plain)
	}
}

func promptTestConfig() *config.RootConfig {
	return &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {},
		},
		Prompts: config.PromptConfig{
			Enabled: boolPtrTUI(true),
			Presets: map[string]*config.PromptPreset{
				"coding": {Name: "coding", Description: "Coding", File: "p.md", Mode: config.PromptModeAppend},
			},
			Bindings: []config.PromptBinding{{Integration: "codex", Model: "gpt-5", Preset: "coding"}},
		},
	}
}

func boolPtrTUI(v bool) *bool { return &v }
