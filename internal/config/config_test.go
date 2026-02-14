package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := defaultConfig()
	cfg.DefaultProfile = "work"
	cfg.Profiles["work"] = &Profile{
		OpenAIBaseURL: "https://example.com/v1",
		OpenAIAPIKey:  "token",
		Models:        []string{"gpt-4.1-mini", "gpt-4.1"},
		DefaultModel:  "gpt-4.1",
	}
	cfg.UpsertModelHistory("gpt-4.1-mini")

	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if got.DefaultProfile != "work" {
		t.Fatalf("DefaultProfile mismatch, got %q", got.DefaultProfile)
	}
	if got.Profiles["work"] == nil || got.Profiles["work"].OpenAIAPIKey != "token" {
		t.Fatalf("work profile not persisted correctly: %#v", got.Profiles["work"])
	}
	if got.Profiles["work"].DefaultModel != "gpt-4.1" {
		t.Fatalf("work profile default model mismatch: %q", got.Profiles["work"].DefaultModel)
	}
	if got.Integration("codex").Profile != "work" {
		t.Fatalf("integration profile mismatch, got %q", got.Integration("codex").Profile)
	}
	if !reflect.DeepEqual(got.Profiles["work"].Models, []string{"gpt-4.1-mini", "gpt-4.1"}) {
		t.Fatalf("profile models mismatch: %#v", got.Profiles["work"].Models)
	}
	if got.History.LastModelInput != "gpt-4.1-mini" {
		t.Fatalf("history last model mismatch, got %q", got.History.LastModelInput)
	}
}

func TestLoadCreatesDefaultConfigWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got.DefaultProfile != "default" {
		t.Fatalf("DefaultProfile mismatch, got %q", got.DefaultProfile)
	}
	if got.Profiles["default"] == nil {
		t.Fatalf("default profile should exist")
	}

	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath failed: %v", err)
	}
	wantPrefix := filepath.Join(homeDirFromTest(t), ".spark")
	if filepath.Dir(path) != wantPrefix {
		t.Fatalf("config dir mismatch, got %q want %q", filepath.Dir(path), wantPrefix)
	}
	if filepath.Base(path) != "config.json" {
		t.Fatalf("unexpected config path: %q", path)
	}
}

func TestUpsertModelHistoryDedupAndLimit(t *testing.T) {
	cfg := defaultConfig()
	for i := 0; i < 30; i++ {
		cfg.UpsertModelHistory(fmt.Sprintf("model-%02d", i))
	}
	cfg.UpsertModelHistory("model-29")

	if len(cfg.History.ModelInputs) > 20 {
		t.Fatalf("history length should be <= 20, got %d", len(cfg.History.ModelInputs))
	}
	if cfg.History.ModelInputs[0] != "model-29" {
		t.Fatalf("latest history item should be first, got %q", cfg.History.ModelInputs[0])
	}
}

func TestSetDefaultProfileRequiresExistingProfile(t *testing.T) {
	cfg := defaultConfig()
	if err := cfg.SetDefaultProfile("missing"); err == nil {
		t.Fatalf("expected error when setting missing profile")
	}
}

func homeDirFromTest(t *testing.T) string {
	t.Helper()
	return os.Getenv("HOME")
}
