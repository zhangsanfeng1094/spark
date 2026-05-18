package tui

import (
	"strings"
	"testing"

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
