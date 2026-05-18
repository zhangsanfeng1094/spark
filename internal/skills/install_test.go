package skills

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const testSkillFrontmatter = `---
name: brainstorming
description: Explore intent first.
---

# Brainstorming
`

func TestInstallLocalSkillCopiesContentAndUpdatesRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sourceDir := filepath.Join(t.TempDir(), "brainstorming")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte(testSkillFrontmatter), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := Install(InstallOptions{
		Name:       "brainstorming",
		SourceType: SourceTypeLocal,
		Source:     sourceDir,
	})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if installed.Name != "brainstorming" {
		t.Fatalf("Name=%q", installed.Name)
	}

	storeRoot, err := StorageRoot()
	if err != nil {
		t.Fatalf("StorageRoot failed: %v", err)
	}
	targetSkill := filepath.Join(storeRoot, "brainstorming")
	if _, err := os.Stat(filepath.Join(targetSkill, "SKILL.md")); err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}
	if installed.InstalledPath != targetSkill {
		t.Fatalf("InstalledPath=%q want %q", installed.InstalledPath, targetSkill)
	}

	registry, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	entry := registry.Skills["brainstorming"]
	if entry == nil {
		t.Fatalf("registry entry missing")
	}
	if entry.Manifest.Description != "Explore intent first." {
		t.Fatalf("Description=%q", entry.Manifest.Description)
	}
	if !entry.Managed || !entry.Enabled {
		t.Fatalf("managed=%t enabled=%t", entry.Managed, entry.Enabled)
	}
}

func TestSetEnabledUpdatesRegistryWithoutRemovingFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sourceDir := filepath.Join(t.TempDir(), "brainstorming")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte(testSkillFrontmatter), 0o644); err != nil {
		t.Fatal(err)
	}

	entry, err := Install(InstallOptions{
		Name:       "brainstorming",
		SourceType: SourceTypeLocal,
		Source:     sourceDir,
	})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	if err := SetEnabled("brainstorming", false); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	if registry.Skills["brainstorming"].Enabled {
		t.Fatalf("expected disabled entry")
	}
	if _, err := os.Stat(filepath.Join(entry.InstalledPath, "SKILL.md")); err != nil {
		t.Fatalf("skill files should remain on disk: %v", err)
	}
}

func TestLoadManifestReadsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: brainstorming-ideas
description: Explore adjacent directions.
---

# Brainstorming Ideas
`), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if manifest.Name != "brainstorming-ideas" {
		t.Fatalf("Name=%q", manifest.Name)
	}
	if manifest.Description != "Explore adjacent directions." {
		t.Fatalf("Description=%q", manifest.Description)
	}
}

func TestInstallFromGitFallsBackToRepoRootWhenSubdirMissingSkillMarkdown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "SKILL.md"), []byte(`---
name: root-skill
description: root package
---

# Root Skill
`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "config", "user.email", "spark@example.com")
	runGit(t, repoDir, "config", "user.name", "Spark")
	runGit(t, repoDir, "commit", "-m", "init")

	entry, err := Install(InstallOptions{
		Name:       "root-skill",
		SourceType: SourceTypeGit,
		Source:     repoDir,
		Subdir:     "missing-subdir",
	})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(entry.InstalledPath, "SKILL.md")); err != nil {
		t.Fatalf("expected installed root skill: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(output))
	}
}
