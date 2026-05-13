package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryPathAndStorageRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registryPath, err := RegistryPath()
	if err != nil {
		t.Fatalf("RegistryPath failed: %v", err)
	}
	storeRoot, err := StorageRoot()
	if err != nil {
		t.Fatalf("StorageRoot failed: %v", err)
	}

	home := os.Getenv("HOME")
	if registryPath != filepath.Join(home, ".spark", "skill-registry.json") {
		t.Fatalf("RegistryPath=%q", registryPath)
	}
	if storeRoot != filepath.Join(home, ".spark", "skills") {
		t.Fatalf("StorageRoot=%q", storeRoot)
	}
}

func TestLoadRegistryReturnsDefaultWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	if registry.Version != CurrentRegistryVersion {
		t.Fatalf("Version=%d", registry.Version)
	}
	if len(registry.Skills) != 0 {
		t.Fatalf("expected empty skills, got %#v", registry.Skills)
	}
}

func TestSaveLoadRegistryRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry := DefaultRegistry()
	registry.Skills["brainstorming"] = &SkillEntry{
		Name:       "brainstorming",
		SourceType: SourceTypeLocal,
		Source:     "/tmp/brainstorming",
		Enabled:    true,
		Targets:    []string{"codex", "claude"},
		Managed:    true,
		Manifest: SkillManifest{
			Name:        "brainstorming",
			Description: "Explores design intent before implementation.",
		},
	}

	if err := SaveRegistry(registry); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	got, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	entry := got.Skills["brainstorming"]
	if entry == nil {
		t.Fatalf("missing saved skill: %#v", got.Skills)
	}
	if entry.SourceType != SourceTypeLocal {
		t.Fatalf("SourceType=%q", entry.SourceType)
	}
	if !entry.Enabled {
		t.Fatalf("expected enabled entry")
	}
	if entry.Manifest.Description == "" {
		t.Fatalf("manifest description should persist")
	}
}

func TestNormalizeRegistryDefaultsTargetsAndName(t *testing.T) {
	registry := &Registry{
		Skills: map[string]*SkillEntry{
			" Brainstorming ": {
				Name:    " Brainstorming ",
				Managed: true,
			},
		},
	}

	NormalizeRegistry(registry)

	entry := registry.Skills["brainstorming"]
	if entry == nil {
		t.Fatalf("normalized key missing: %#v", registry.Skills)
	}
	if entry.Name != "brainstorming" {
		t.Fatalf("Name=%q", entry.Name)
	}
	if len(entry.Targets) != 2 || entry.Targets[0] != "codex" || entry.Targets[1] != "claude" {
		t.Fatalf("Targets=%v", entry.Targets)
	}
}
