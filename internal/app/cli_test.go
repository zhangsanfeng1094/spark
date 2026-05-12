package app

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"spark/internal/config"
	"spark/internal/skills"
)

func TestProfileNamesSorted(t *testing.T) {
	cfg := &config.RootConfig{
		Profiles: map[string]*config.Profile{
			"zeta":  {},
			"alpha": {},
			"beta":  {},
		},
	}

	got := profileNames(cfg)
	want := []string{"alpha", "beta", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profileNames mismatch, got %v want %v", got, want)
	}
}

func TestResolveModelsPrecedence(t *testing.T) {
	profile := &config.Profile{
		Models:       []string{"profile-model-a", "profile-model-b"},
		DefaultModel: "profile-default-model",
	}

	got := resolveModels("flag-model", profile)
	want := []string{"flag-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flag precedence mismatch, got %v want %v", got, want)
	}

	got = resolveModels("", profile)
	want = []string{"profile-default-model", "profile-model-a", "profile-model-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default model precedence mismatch, got %v want %v", got, want)
	}

	got = resolveModels("", &config.Profile{DefaultModel: "profile-model"})
	want = []string{"profile-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default model fallback mismatch, got %v want %v", got, want)
	}
}

func TestResolveModelsDefaultModelDedupAndReorder(t *testing.T) {
	profile := &config.Profile{
		Models:       []string{"model-a", "model-b", "model-a"},
		DefaultModel: "model-b",
	}

	got := resolveModels("", profile)
	want := []string{"model-b", "model-a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default model reorder mismatch, got %v want %v", got, want)
	}
}

func TestResolveModelsStripsNUL(t *testing.T) {
	profile := &config.Profile{
		Models:       []string{"glm-5\x00", "other"},
		DefaultModel: " glm-5\x00 ",
	}

	got := resolveModels("", profile)
	want := []string{"glm-5", "other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveModels mismatch, got %v want %v", got, want)
	}

	got = resolveModels(" glm-5\x00 ", profile)
	want = []string{"glm-5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveModels flag mismatch, got %v want %v", got, want)
	}
}

func TestRootCmdIncludesSkillCommand(t *testing.T) {
	root := NewRootCmd()
	if root.CommandPath() != "spark" {
		t.Fatalf("unexpected root path: %q", root.CommandPath())
	}
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected skill command to be registered")
	}
}

func TestSkillListCommandPrintsInstalledSkills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)

	registry := skills.DefaultRegistry()
	registry.Skills["brainstorming"] = &skills.SkillEntry{
		Name:       "brainstorming",
		SourceType: skills.SourceTypeLocal,
		Source:     "/tmp/brainstorming",
		Enabled:    true,
		Managed:    true,
		Targets:    []string{"codex", "claude"},
	}
	if err := skills.SaveRegistry(registry); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	root.SetArgs([]string{"skill", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "brainstorming") {
		t.Fatalf("expected skill name in output, got %q", got)
	}
}

func TestSkillSearchCommandUsesCatalogResultsInSelector(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	restore := stubSkillCatalogForApp([]skills.CatalogEntry{
		{Name: "brainstorming", Repo: "obra/superpowers", DetailURL: "https://skills.sh/obra/superpowers/brainstorming"},
	})
	defer restore()
	restoreSelect := stubSkillSelector(func(title string, options []string) (string, error) {
		if len(options) != 1 || !strings.Contains(options[0], "obra/superpowers") {
			t.Fatalf("unexpected options: %v", options)
		}
		return options[0], nil
	})
	defer restoreSelect()
	restoreInstall := stubSkillInstallFromCatalog(func(name string) (*skills.SkillEntry, error) {
		return &skills.SkillEntry{Name: name}, nil
	})
	defer restoreInstall()

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"skill", "search", "brain"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "Installed skill brainstorming.") {
		t.Fatalf("expected install confirmation, got %q", got)
	}
}

func TestSkillSearchCommandSelectsCandidateAndInstalls(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	restoreSearch := stubSkillCatalogForApp([]skills.CatalogEntry{
		{Name: "brainstorming", Repo: "obra/superpowers"},
		{Name: "openai-docs", Repo: "openai/skills"},
	})
	defer restoreSearch()

	selected := ""
	restoreSelect := stubSkillSelector(func(title string, options []string) (string, error) {
		if title != "Select skill to install:" {
			t.Fatalf("unexpected selector title: %q", title)
		}
		selected = options[1]
		return options[1], nil
	})
	defer restoreSelect()

	installed := ""
	restoreInstall := stubSkillInstallFromCatalog(func(name string) (*skills.SkillEntry, error) {
		installed = name
		return &skills.SkillEntry{Name: name}, nil
	})
	defer restoreInstall()

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"skill", "search", "docs"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if selected == "" {
		t.Fatal("expected candidate selection")
	}
	if installed != "openai-docs" {
		t.Fatalf("expected selected skill to be installed, got %q", installed)
	}
	if got := buf.String(); !strings.Contains(got, "Installed skill openai-docs.") {
		t.Fatalf("expected install confirmation, got %q", got)
	}
}
