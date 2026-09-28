package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Old configs written by the removed Prompt Manager may still contain a
// "prompts" block. Loading must ignore it instead of failing.
func TestLoadIgnoresLegacyPromptsField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sparkDir := filepath.Join(home, ".spark")
	if err := os.MkdirAll(sparkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{
		"version": 1,
		"default_profile": "default",
		"profiles": {"default": {"openai_base_url": "https://api.openai.com/v1"}},
		"prompts": {
			"enabled": true,
			"presets": {"coding": {"name": "coding", "file": "prompts/coding.md", "mode": "append"}},
			"bindings": [{"integration": "codex", "model": "*", "preset": "coding", "enabled": true}]
		}
	}`
	if err := os.WriteFile(filepath.Join(sparkDir, "config.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with legacy prompts field failed: %v", err)
	}
	if cfg.DefaultProfile != "default" || cfg.Profiles["default"] == nil {
		t.Fatalf("unexpected config after legacy load: %#v", cfg)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save after legacy load failed: %v", err)
	}
	saved, err := os.ReadFile(filepath.Join(sparkDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 || saved[0] != '{' {
		t.Fatalf("saved config is not JSON: %q", string(saved))
	}
	if _, err := Load(); err != nil {
		t.Fatalf("re-load after save failed: %v", err)
	}
}
