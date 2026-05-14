package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/skills"
)

func TestSkillManagerBrowseFocusStartsInQuickAdd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := newSkillManagerModel(skills.DefaultRegistry())
	if m.browseFocus != skillBrowseFocusQuickAdd {
		t.Fatalf("expected quick add focus, got %v", m.browseFocus)
	}
	if len(m.quickAddItems) != 3 {
		t.Fatalf("expected 3 quick actions, got %d", len(m.quickAddItems))
	}
	if got := m.quickAddItems[0].Label; got != "Install Local" {
		t.Fatalf("expected first action to be Install Local, got %q", got)
	}
}

func TestSkillManagerRenderEmptyState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := newSkillManagerModel(skills.DefaultRegistry())
	right := m.renderDetails()

	for _, want := range []string{"No skills yet", "Install a local skill", "skill-registry.json"} {
		if !strings.Contains(right, want) {
			t.Fatalf("expected empty state to contain %q, got %q", want, right)
		}
	}
}

func TestSkillManagerBrowseFocusCyclesWithTab(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry := skills.DefaultRegistry()
	registry.Skills["brainstorming"] = &skills.SkillEntry{Name: "brainstorming", Enabled: true, Managed: true}
	m := newSkillManagerModel(registry)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.browseFocus != skillBrowseFocusSkills {
		t.Fatalf("expected skill list focus after tab, got %v", m.browseFocus)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.browseFocus != skillBrowseFocusQuickAdd {
		t.Fatalf("expected quick add focus after shift+tab, got %v", m.browseFocus)
	}
}

func TestSkillManagerQuickAddEnterOpensInstallModal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := newSkillManagerModel(skills.DefaultRegistry())
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.installing {
		t.Fatal("expected install modal to open")
	}
	if got := m.installFields[skillInstallFieldName].value; got != "" {
		t.Fatalf("expected empty name field, got %q", got)
	}
}

func TestSkillManagerBrowseCatalogOpensCatalogModal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := newSkillManagerModel(skills.DefaultRegistry())
	m.quickAddIndex = 1
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.cataloging {
		t.Fatal("expected catalog modal to open")
	}
}

func TestSkillManagerToggleShortcutUpdatesEnabledState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sourceDir := filepath.Join(t.TempDir(), "brainstorming")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte(`---
name: brainstorming
description: Explore intent first.
---

# Brainstorming
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.Install(skills.InstallOptions{Name: "brainstorming", SourceType: skills.SourceTypeLocal, Source: sourceDir}); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	registry, err := skills.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	m := newSkillManagerModel(registry)
	m.browseFocus = skillBrowseFocusSkills
	m.selected = 0

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if cmd == nil {
		t.Fatal("expected toggle command")
	}
	msg := cmd()
	m = model.(*skillManagerModel)
	model, _ = m.Update(msg)
	m = model.(*skillManagerModel)

	reloaded, err := skills.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	if reloaded.Skills["brainstorming"].Enabled {
		t.Fatal("expected skill to be disabled after toggle")
	}
}

func TestSkillManagerRenderDetailsIncludesSkillMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry := skills.DefaultRegistry()
	registry.Skills["brainstorming"] = &skills.SkillEntry{
		Name:                "brainstorming",
		Scope:               skills.ScopeGlobal,
		SourceKind:          skills.SourceKindLocal,
		SourceType:          skills.SourceTypeLocal,
		Source:              "/tmp/brainstorming",
		Enabled:             true,
		Managed:             true,
		InstalledPath:       "/tmp/brainstorming",
		AgentTargets:        []string{"codex", "claude"},
		MaterializationMode: skills.MaterializationCopy,
		Manifest:            skills.SkillManifest{Name: "brainstorming", Description: "Explore intent first."},
	}
	m := newSkillManagerModel(registry)
	m.selected = 0
	m.browseFocus = skillBrowseFocusSkills

	right := m.renderDetails()
	for _, want := range []string{"Overview", "brainstorming", "Scope: global", "Source: local", "Targets: claude, codex", "Explore intent first."} {
		if !strings.Contains(right, want) {
			t.Fatalf("expected details to contain %q, got %q", want, right)
		}
	}
}
