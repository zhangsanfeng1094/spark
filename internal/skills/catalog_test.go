package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchCatalogUsesSkillsFindOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	restore := stubCatalogCommand(`brainstorming obra/superpowers
openai-docs openai/skills
`)
	defer restore()
	results, err := SearchCatalog("brain")
	if err != nil {
		t.Fatalf("SearchCatalog failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %#v", results)
	}
	if results[0].Name != "brainstorming" {
		t.Fatalf("Name=%q", results[0].Name)
	}
	if results[0].Repo != "obra/superpowers" {
		t.Fatalf("Repo=%q", results[0].Repo)
	}
	if results[0].DetailURL != "https://skills.sh/obra/superpowers/brainstorming" {
		t.Fatalf("DetailURL=%q", results[0].DetailURL)
	}
}

func TestResolveCatalogInstallParsesDetailPage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	restoreCmd := stubCatalogCommand("openai-docs openai/skills\n")
	defer restoreCmd()
	restore := stubCatalogFetch(map[string]string{
		"https://skills.sh":                           `<html><body><a href="/openai/skills/openai-docs">1 openai-docs openai/skills 100</a></body></html>`,
		"https://skills.sh/openai/skills/openai-docs": `<html><body><code>$ npx skills add https://github.com/openai/skills --skill openai-docs</code></body></html>`,
	})
	defer restore()
	entry, err := ResolveCatalogInstall("openai-docs")
	if err != nil {
		t.Fatalf("ResolveCatalogInstall failed: %v", err)
	}
	if entry.Source != "https://github.com/openai/skills" {
		t.Fatalf("Source=%q", entry.Source)
	}
	if entry.Subdir != "openai-docs" {
		t.Fatalf("Subdir=%q", entry.Subdir)
	}
}

func TestInstallFromCatalogInstallsSkillIntoRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := filepath.Join(t.TempDir(), "skills-repo")
	skillDir := filepath.Join(repoDir, "openai-docs")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: openai-docs
description: Use OpenAI docs effectively.
---

# OpenAI Docs
`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "spark@example.com")
	runGit(t, repoDir, "config", "user.name", "Spark")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "init")

	restoreCmd := stubCatalogCommand("openai-docs openai/skills\n")
	defer restoreCmd()
	restore := stubCatalogFetch(map[string]string{
		"https://skills.sh":                           `<html><body><a href="/openai/skills/openai-docs">1 openai-docs openai/skills 100</a></body></html>`,
		"https://skills.sh/openai/skills/openai-docs": `<html><body><code>$ npx skills add ` + repoDir + ` --skill openai-docs</code></body></html>`,
	})
	defer restore()
	entry, err := InstallFromCatalog("openai-docs")
	if err != nil {
		t.Fatalf("InstallFromCatalog failed: %v", err)
	}
	if entry.Name != "openai-docs" {
		t.Fatalf("Name=%q", entry.Name)
	}
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}
	if registry.Skills["openai-docs"] == nil {
		t.Fatalf("expected installed catalog entry")
	}
}

func TestSaveAndLoadCatalogCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	entries := []CatalogEntry{{Name: "brainstorming", Repo: "obra/superpowers", DetailURL: "https://skills.sh/obra/superpowers/brainstorming"}}
	if err := SaveCatalogCache("skills.sh", entries); err != nil {
		t.Fatalf("SaveCatalogCache failed: %v", err)
	}
	got, err := LoadCatalogCache("skills.sh")
	if err != nil {
		t.Fatalf("LoadCatalogCache failed: %v", err)
	}
	if len(got) != 1 || got[0].Name != "brainstorming" {
		t.Fatalf("unexpected cache entries: %#v", got)
	}
}

func TestSearchCatalogFallsBackToCacheOnFetchFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	entries := []CatalogEntry{{Name: "brainstorming", Repo: "obra/superpowers"}}
	if err := SaveCatalogCache("skills.sh", entries); err != nil {
		t.Fatalf("SaveCatalogCache failed: %v", err)
	}
	restore := stubCatalogCommandError(os.ErrNotExist)
	defer restore()

	got, err := SearchCatalog("brain")
	if err != nil {
		t.Fatalf("SearchCatalog failed: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Name, "brain") {
		t.Fatalf("unexpected fallback results: %#v", got)
	}
}

func TestParseSkillsFindOutputAcceptsBracketFormat(t *testing.T) {
	entries := parseSkillsFindOutput("Catalog results:\n- brainstorming [obra/superpowers]\n")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %#v", entries)
	}
	if entries[0].Name != "brainstorming" || entries[0].Repo != "obra/superpowers" {
		t.Fatalf("unexpected entry: %#v", entries[0])
	}
}

func stubCatalogFetch(responses map[string]string) func() {
	original := catalogFetchURL
	catalogFetchURL = func(rawURL string) (string, error) {
		if body, ok := responses[rawURL]; ok {
			return body, nil
		}
		return "", os.ErrNotExist
	}
	return func() {
		catalogFetchURL = original
	}
}

func stubCatalogCommand(output string) func() {
	original := runSkillsFind
	runSkillsFind = func(query string) (string, error) {
		return output, nil
	}
	return func() {
		runSkillsFind = original
	}
}

func stubCatalogCommandError(err error) func() {
	original := runSkillsFind
	runSkillsFind = func(query string) (string, error) {
		return "", err
	}
	return func() {
		runSkillsFind = original
	}
}
