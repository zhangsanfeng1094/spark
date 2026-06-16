package tui

import (
	"strings"
	"testing"

	"spark/internal/config"
)

func TestSettingsInitialRenderShowsGlobalConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := testSettingsConfig()
	cfg.DefaultIntegration = "codex"
	cfg.Integration("codex").ModelCatalogJSON = "/tmp/catalog.json"

	m := newSettingsModel(cfg, []string{"claude", "codex"})
	m.width = 120
	m.height = 40

	plain := StripANSI(m.View())
	for _, want := range []string{
		"Settings",
		"Default integration",
		"codex",
		"Default profile",
		"work",
		"Effective default model",
		"work-default",
		"Prompt injection enabled",
		"enabled",
		"Codex model catalog JSON",
		"/tmp/catalog.json",
		"Config path",
		"Config version",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected settings view to contain %q, got %q", want, plain)
		}
	}
}

func TestSettingsSaveAppliesGlobalDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := testSettingsConfig()
	cfg.History = config.History{LastSelection: "claude", LastModelInput: "old", ModelInputs: []string{"old"}}

	m := newSettingsModel(cfg, []string{"claude", "codex"})
	m.draft.DefaultIntegration = "codex"
	m.draft.DefaultProfile = "default"
	m.draft.PromptEnabled = false
	m.draft.CodexModelCatalogJSON = " /tmp/codex-models.json "
	m.save()

	if cfg.DefaultIntegration != "codex" {
		t.Fatalf("DefaultIntegration=%q", cfg.DefaultIntegration)
	}
	if cfg.DefaultProfile != "default" {
		t.Fatalf("DefaultProfile=%q", cfg.DefaultProfile)
	}
	if cfg.Prompts.IsEnabled() {
		t.Fatalf("prompt injection should be disabled")
	}
	if got := cfg.Integration("codex").ModelCatalogJSON; got != "/tmp/codex-models.json" {
		t.Fatalf("ModelCatalogJSON=%q", got)
	}
	if cfg.History.LastSelection != "claude" || cfg.History.LastModelInput != "old" {
		t.Fatalf("history should be preserved, got %#v", cfg.History)
	}
	if m.dirty {
		t.Fatalf("dirty should be false after save")
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.DefaultIntegration != "codex" || loaded.DefaultProfile != "default" {
		t.Fatalf("loaded config mismatch: default integration=%q profile=%q", loaded.DefaultIntegration, loaded.DefaultProfile)
	}
}

func TestSettingsSaveRejectsMissingDefaultProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := testSettingsConfig()
	m := newSettingsModel(cfg, []string{"codex"})
	m.draft.DefaultProfile = "missing"

	m.save()

	if cfg.DefaultProfile != "work" {
		t.Fatalf("default profile should be unchanged, got %q", cfg.DefaultProfile)
	}
	if !strings.Contains(m.status, "profile does not exist") {
		t.Fatalf("expected validation status, got %q", m.status)
	}
}

func TestSettingsClearHistorySavesEmptyHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := testSettingsConfig()
	cfg.History = config.History{LastSelection: "codex", LastModelInput: "gpt-5", ModelInputs: []string{"gpt-5", "sonnet"}}
	m := newSettingsModel(cfg, []string{"codex"})
	m.focusSection = settingsSectionHistory
	m.focusField = 3

	m.activateField()
	m.save()

	if cfg.History.LastSelection != "" || cfg.History.LastModelInput != "" || len(cfg.History.ModelInputs) != 0 {
		t.Fatalf("history should be cleared, got %#v", cfg.History)
	}
}

func testSettingsConfig() *config.RootConfig {
	cfg := &config.RootConfig{
		Version:        1,
		DefaultProfile: "work",
		Profiles: map[string]*config.Profile{
			"default": {DefaultModel: "default-model"},
			"work":    {DefaultModel: "work-default", Models: []string{"work-a"}},
		},
		Integrations: map[string]*config.IntegrationConfig{},
	}
	cfg.Prompts.SetEnabled(true)
	config.Normalize(cfg)
	return cfg
}
