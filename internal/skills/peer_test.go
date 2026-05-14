package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncToCodexExportsEnabledSkillsOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	skillDir := filepath.Join(t.TempDir(), "brainstorming")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(testSkillFrontmatter), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(InstallOptions{Name: "brainstorming", SourceType: SourceTypeLocal, Source: skillDir}); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	disabledDir := filepath.Join(t.TempDir(), "disabled")
	if err := os.MkdirAll(disabledDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disabledDir, "SKILL.md"), []byte(`---
name: disabled
description: Disabled skill.
---

# Disabled
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(InstallOptions{Name: "disabled", SourceType: SourceTypeLocal, Source: disabledDir}); err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if err := SetEnabled("disabled", false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	targetRoot := filepath.Join(os.Getenv("HOME"), ".codex", "skills")
	if err := SyncToPeer("codex", targetRoot); err != nil {
		t.Fatalf("SyncToPeer failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetRoot, "brainstorming", "SKILL.md")); err != nil {
		t.Fatalf("expected exported skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "disabled")); !os.IsNotExist(err) {
		t.Fatalf("disabled skill should not be exported, err=%v", err)
	}
}

func TestImportFromClaudeCreatesUnmanagedLocalEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	peerRoot := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "brainstorming")
	if err := os.MkdirAll(peerRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerRoot, "SKILL.md"), []byte(testSkillFrontmatter), 0o644); err != nil {
		t.Fatal(err)
	}

	imported, err := ImportFromPeer("claude", filepath.Join(os.Getenv("HOME"), ".claude", "skills"))
	if err != nil {
		t.Fatalf("ImportFromPeer failed: %v", err)
	}
	if imported.Added != 1 {
		t.Fatalf("Added=%d", imported.Added)
	}

	registry, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	entry := registry.Skills["brainstorming"]
	if entry == nil {
		t.Fatalf("missing imported entry")
	}
	if entry.Managed {
		t.Fatalf("imported peer entry should be unmanaged")
	}
	if entry.SourceKind != SourceKindImported {
		t.Fatalf("SourceKind=%q", entry.SourceKind)
	}
	if entry.InstalledPath != peerRoot {
		t.Fatalf("InstalledPath=%q", entry.InstalledPath)
	}
}
