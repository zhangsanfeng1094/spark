package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptConfigRoundTripAndResolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	promptPath := filepath.Join(home, ".spark", "prompts", "coding.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("be concise\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := defaultConfig()
	cfg.Prompts.SetEnabled(true)
	cfg.Prompts.Presets["coding"] = &PromptPreset{Name: "coding", Description: "Coding style", File: "prompts/coding.md"}
	cfg.Prompts.Bindings = []PromptBinding{
		{Integration: "codex", Model: " gpt-5\x00 ", Preset: "coding", Mode: "replace"},
		{Integration: "claude", Model: " gpt-5 ", Preset: "coding", Mode: "append"},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	codex, err := got.ResolvePromptInjection("codex", "gpt-5")
	if err != nil {
		t.Fatalf("ResolvePromptInjection(codex) failed: %v", err)
	}
	if codex == nil || codex.Mode != PromptModeReplace || codex.Content != "be concise\n" || codex.Path != promptPath {
		t.Fatalf("unexpected codex injection: %#v", codex)
	}

	claude, err := got.ResolvePromptInjection("claude", "gpt-5")
	if err != nil {
		t.Fatalf("ResolvePromptInjection(claude) failed: %v", err)
	}
	if claude == nil || claude.Mode != PromptModeAppend {
		t.Fatalf("unexpected claude injection: %#v", claude)
	}

	none, err := got.ResolvePromptInjection("codex", "other")
	if err != nil || none != nil {
		t.Fatalf("expected no binding, got injection=%#v err=%v", none, err)
	}
}

func TestPromptConfigGlobalEnabledDefaultsOffAndDisabledSkipsInjection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	promptPath := filepath.Join(home, ".spark", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("prompt"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := defaultConfig()
	cfg.Prompts.Presets["p"] = &PromptPreset{Name: "p", File: "prompt.md"}
	cfg.Prompts.Bindings = []PromptBinding{{Integration: "codex", Model: "gpt-5", Preset: "p"}}
	Normalize(cfg)
	if cfg.Prompts.IsEnabled() {
		t.Fatal("expected prompts to default disabled")
	}
	if injection, err := cfg.ResolvePromptInjection("codex", "gpt-5"); err != nil || injection != nil {
		t.Fatalf("expected no injection while default disabled, got injection=%#v err=%v", injection, err)
	}

	cfg.Prompts.SetEnabled(true)
	if injection, err := cfg.ResolvePromptInjection("codex", "gpt-5"); err != nil || injection == nil {
		t.Fatalf("expected injection while enabled, got injection=%#v err=%v", injection, err)
	}

	cfg.Prompts.SetEnabled(false)
	if injection, err := cfg.ResolvePromptInjection("codex", "gpt-5"); err != nil || injection != nil {
		t.Fatalf("expected no injection while disabled, got injection=%#v err=%v", injection, err)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Prompts.IsEnabled() {
		t.Fatal("expected disabled state to persist")
	}
	if len(loaded.Prompts.Bindings) != 1 || loaded.Prompts.Presets["p"] == nil {
		t.Fatalf("expected bindings and presets to be preserved: %#v", loaded.Prompts)
	}
	loaded.Prompts.SetEnabled(true)
	if injection, err := loaded.ResolvePromptInjection("codex", "gpt-5"); err != nil || injection == nil {
		t.Fatalf("expected injection after re-enable, got injection=%#v err=%v", injection, err)
	}
}

func TestPromptBindingEnabledDefaultsPersistsAndControlsInjection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	promptPath := filepath.Join(home, ".spark", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("prompt"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := defaultConfig()
	cfg.Prompts.SetEnabled(true)
	cfg.Prompts.Presets["p"] = &PromptPreset{Name: "p", File: "prompt.md"}
	cfg.Prompts.Bindings = []PromptBinding{{Integration: "codex", Model: "gpt-5", Preset: "p"}}
	Normalize(cfg)
	if !cfg.Prompts.Bindings[0].IsEnabled() {
		t.Fatal("expected old binding without enabled field to default on")
	}
	if injection, err := cfg.ResolvePromptInjection("codex", "gpt-5"); err != nil || injection == nil {
		t.Fatalf("expected injection while binding enabled, got injection=%#v err=%v", injection, err)
	}

	cfg.Prompts.Bindings[0].SetEnabled(false)
	if injection, err := cfg.ResolvePromptInjection("codex", "gpt-5"); err != nil || injection != nil {
		t.Fatalf("expected disabled binding to skip injection, got injection=%#v err=%v", injection, err)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Prompts.Bindings[0].IsEnabled() {
		t.Fatal("expected disabled binding state to persist")
	}

	loaded.Prompts.Bindings[0].SetEnabled(true)
	if injection, err := loaded.ResolvePromptInjection("codex", "gpt-5"); err != nil || injection == nil {
		t.Fatalf("expected injection after binding re-enable, got injection=%#v err=%v", injection, err)
	}
}

func TestPromptWildcardBindingsResolveWithExactPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sparkDir := filepath.Join(home, ".spark")
	if err := os.MkdirAll(sparkDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sparkDir, "default.md"), []byte("default"), 0o644); err != nil {
		t.Fatalf("WriteFile(default) failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sparkDir, "exact.md"), []byte("exact"), 0o644); err != nil {
		t.Fatalf("WriteFile(exact) failed: %v", err)
	}

	cfg := defaultConfig()
	cfg.Prompts.SetEnabled(true)
	cfg.Prompts.Presets["default"] = &PromptPreset{Name: "default", File: "default.md", Mode: PromptModeAppend}
	cfg.Prompts.Presets["exact"] = &PromptPreset{Name: "exact", File: "exact.md", Mode: PromptModeReplace}
	cfg.Prompts.Bindings = []PromptBinding{
		{Integration: "codex", Model: "*", Preset: "default"},
		{Integration: "codex", Model: "gpt-5", Preset: "exact"},
		{Integration: "claude", Model: "*", Preset: "default"},
	}
	Normalize(cfg)

	exact, err := cfg.ResolvePromptInjection("codex", "gpt-5")
	if err != nil || exact == nil || exact.Content != "exact" || exact.Mode != PromptModeReplace {
		t.Fatalf("expected exact codex binding, got injection=%#v err=%v", exact, err)
	}
	wildcard, err := cfg.ResolvePromptInjection("codex", "gpt-4")
	if err != nil || wildcard == nil || wildcard.Content != "default" || wildcard.Mode != PromptModeAppend {
		t.Fatalf("expected codex wildcard binding, got injection=%#v err=%v", wildcard, err)
	}
	claude, err := cfg.ResolvePromptInjection("claude", "gpt-5")
	if err != nil || claude == nil || claude.Content != "default" {
		t.Fatalf("expected claude wildcard binding, got injection=%#v err=%v", claude, err)
	}
	none, err := cfg.ResolvePromptInjection("unknown", "gpt-5")
	if err != nil || none != nil {
		t.Fatalf("expected no injection for unknown integration, got injection=%#v err=%v", none, err)
	}
}

func TestPromptPresetModeAndLegacyBindingOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	promptPath := filepath.Join(home, ".spark", "p.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("prompt"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := defaultConfig()
	cfg.Prompts.SetEnabled(true)
	cfg.Prompts.Presets["p"] = &PromptPreset{Name: "p", File: "p.md", Mode: PromptModeReplace}
	cfg.Prompts.Bindings = []PromptBinding{
		{Integration: "codex", Model: "gpt-5", Preset: "p"},
		{Integration: "claude", Model: "gpt-5", Preset: "p", Mode: PromptModeAppend},
	}
	Normalize(cfg)

	codex, err := cfg.ResolvePromptInjection("codex", "gpt-5")
	if err != nil || codex == nil || codex.Mode != PromptModeReplace {
		t.Fatalf("expected preset mode replace, got injection=%#v err=%v", codex, err)
	}
	claude, err := cfg.ResolvePromptInjection("claude", "gpt-5")
	if err != nil || claude == nil || claude.Mode != PromptModeAppend {
		t.Fatalf("expected legacy override append, got injection=%#v err=%v", claude, err)
	}
}

func TestNormalizePromptModesMigratesLegacyBindingModes(t *testing.T) {
	cfg := &RootConfig{Prompts: PromptConfig{
		Presets:  map[string]*PromptPreset{"p": {Name: "p", File: "p.md"}},
		Bindings: []PromptBinding{{Integration: "codex", Model: "gpt-5", Preset: "p", Mode: PromptModeReplace}},
	}}
	Normalize(cfg)
	if got := cfg.Prompts.Presets["p"].Mode; got != PromptModeReplace {
		t.Fatalf("expected preset mode promoted to replace, got %q", got)
	}
	if got := cfg.Prompts.Bindings[0].Mode; got != "" {
		t.Fatalf("expected matching binding mode cleared, got %q", got)
	}

	cfg = &RootConfig{Prompts: PromptConfig{
		Presets: map[string]*PromptPreset{"p": {Name: "p", File: "p.md"}},
		Bindings: []PromptBinding{
			{Integration: "codex", Model: "gpt-5", Preset: "p", Mode: PromptModeReplace},
			{Integration: "claude", Model: "gpt-5", Preset: "p", Mode: PromptModeAppend},
		},
	}}
	Normalize(cfg)
	if got := cfg.Prompts.Presets["p"].Mode; got != PromptModeAppend {
		t.Fatalf("expected conflicting preset mode to default append, got %q", got)
	}
	if got := cfg.Prompts.Bindings[0].Mode; got != PromptModeReplace {
		t.Fatalf("expected differing legacy override to remain, got %q", got)
	}
	if got := cfg.Prompts.Bindings[1].Mode; got != "" {
		t.Fatalf("expected matching append override to clear, got %q", got)
	}
}

func TestDisabledPromptBindingDoesNotReadPresetButValidationStillChecksIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := defaultConfig()
	cfg.Prompts.SetEnabled(true)
	cfg.Prompts.Presets["p"] = &PromptPreset{Name: "p", File: "missing.md"}
	binding := PromptBinding{Integration: "codex", Model: "gpt-5", Preset: "p"}
	binding.SetEnabled(false)
	cfg.Prompts.Bindings = []PromptBinding{binding}
	Normalize(cfg)

	if injection, err := cfg.ResolvePromptInjection("codex", "gpt-5"); err != nil || injection != nil {
		t.Fatalf("expected disabled binding to skip missing preset file, got injection=%#v err=%v", injection, err)
	}
	if err := cfg.ValidatePrompts(); err != nil {
		t.Fatalf("expected runtime validation to skip disabled binding preset, got %v", err)
	}
	if err := cfg.ValidatePromptConfigStrict(); err == nil || !strings.Contains(err.Error(), "missing.md") {
		t.Fatalf("expected strict validation to check disabled binding preset, got %v", err)
	}
	issues := cfg.CheckPrompts()
	if len(issues) == 0 || issues[0].Active || issues[0].Severity != PromptValidationWarning {
		t.Fatalf("expected inactive warning issue, got %#v", issues)
	}
}

func TestResolvePromptPathExpandsHomeAndSparkRelative(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := ResolvePromptPath("~/prompt.md")
	if err != nil {
		t.Fatalf("ResolvePromptPath(home) failed: %v", err)
	}
	if got != filepath.Join(home, "prompt.md") {
		t.Fatalf("home path mismatch: %q", got)
	}

	got, err = ResolvePromptPath("prompts/a.md")
	if err != nil {
		t.Fatalf("ResolvePromptPath(relative) failed: %v", err)
	}
	if got != filepath.Join(home, ".spark", "prompts", "a.md") {
		t.Fatalf("relative path mismatch: %q", got)
	}
}

func TestResolvePromptPathRejectsEscapesAndHomeExternalAbsolute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, path := range []string{"../x.md", "../../etc/passwd", filepath.Join(t.TempDir(), "outside.md")} {
		if got, err := ResolvePromptPath(path); err == nil || !strings.Contains(err.Error(), "allowed prompt locations") {
			t.Fatalf("expected %q to be rejected, got path=%q err=%v", path, got, err)
		}
	}

	inside := filepath.Join(home, "prompt.md")
	if got, err := ResolvePromptPath(inside); err != nil || got != inside {
		t.Fatalf("expected home absolute path to pass, got path=%q err=%v", got, err)
	}
}

func TestPromptValidationFailsForMissingPresetEmptyFileAndDeleteInUse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".spark"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".spark", "empty.md"), []byte(" \n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := defaultConfig()
	cfg.Prompts.SetEnabled(true)
	cfg.Prompts.Presets["empty"] = &PromptPreset{Name: "empty", File: "empty.md"}
	cfg.Prompts.Bindings = []PromptBinding{{Integration: "codex", Model: "gpt-5", Preset: "empty"}}

	if err := cfg.ValidatePrompts(); err == nil || !strings.Contains(err.Error(), "file is empty") {
		t.Fatalf("expected empty file error, got %v", err)
	}
	if err := cfg.RemovePromptPreset("empty"); err == nil || !strings.Contains(err.Error(), "used by a binding") {
		t.Fatalf("expected delete protection error, got %v", err)
	}

	cfg.Prompts.Presets = map[string]*PromptPreset{}
	if _, err := cfg.ResolvePromptInjection("codex", "gpt-5"); err == nil || !strings.Contains(err.Error(), "missing preset") {
		t.Fatalf("expected missing preset error, got %v", err)
	}
}

func TestPromptValidationFailsForInvalidModeAndDuplicateBinding(t *testing.T) {
	cfg := defaultConfig()
	cfg.Prompts.SetEnabled(true)
	cfg.Prompts.Presets["p"] = &PromptPreset{Name: "p", File: "missing.md"}
	cfg.Prompts.Bindings = []PromptBinding{{Integration: "codex", Model: "gpt-5", Preset: "p", Mode: "bad"}}

	if err := cfg.ValidatePrompts(); err == nil || !strings.Contains(err.Error(), "invalid mode") {
		t.Fatalf("expected invalid mode error, got %v", err)
	}

	cfg.Prompts.Bindings = []PromptBinding{
		{Integration: "codex", Model: "gpt-5", Preset: "p", Mode: "append"},
		{Integration: "codex", Model: "gpt-5", Preset: "p", Mode: "replace"},
	}
	if err := cfg.ValidatePrompts(); err == nil || !strings.Contains(err.Error(), "duplicate prompt binding") {
		t.Fatalf("expected duplicate binding error, got %v", err)
	}
}
