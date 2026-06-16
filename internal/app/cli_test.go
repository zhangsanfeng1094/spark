package app

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"spark/internal/config"
	"spark/internal/skills"
	"spark/internal/tui"
	"spark/internal/usage"
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

func TestResolveLaunchProfileUsesDefaultProfile(t *testing.T) {
	cfg := testProfileConfig()

	name, profile, err := resolveLaunchProfile(cfg, "", false, func(title string, options []string) (string, error) {
		t.Fatalf("picker should not be called")
		return "", nil
	})
	if err != nil {
		t.Fatalf("resolveLaunchProfile failed: %v", err)
	}
	if name != "default" || profile != cfg.Profiles["default"] {
		t.Fatalf("expected default profile, got name=%q profile=%p", name, profile)
	}
}

func TestResolveLaunchProfileUsesProfileFlag(t *testing.T) {
	cfg := testProfileConfig()

	name, profile, err := resolveLaunchProfile(cfg, " foo ", false, func(title string, options []string) (string, error) {
		t.Fatalf("picker should not be called")
		return "", nil
	})
	if err != nil {
		t.Fatalf("resolveLaunchProfile failed: %v", err)
	}
	if name != "foo" || profile != cfg.Profiles["foo"] {
		t.Fatalf("expected foo profile, got name=%q profile=%p", name, profile)
	}
}

func TestResolveLaunchProfileSelectProfileUsesPicker(t *testing.T) {
	cfg := testProfileConfig()
	called := false

	name, profile, err := resolveLaunchProfile(cfg, "", true, func(title string, options []string) (string, error) {
		called = true
		if title != "Select profile:" {
			t.Fatalf("unexpected picker title: %q", title)
		}
		want := []string{"default", "foo", "zeta"}
		if !reflect.DeepEqual(options, want) {
			t.Fatalf("picker options mismatch, got %v want %v", options, want)
		}
		return "zeta", nil
	})
	if err != nil {
		t.Fatalf("resolveLaunchProfile failed: %v", err)
	}
	if !called {
		t.Fatalf("expected picker to be called")
	}
	if name != "zeta" || profile != cfg.Profiles["zeta"] {
		t.Fatalf("expected zeta profile, got name=%q profile=%p", name, profile)
	}
}

func TestResolveLaunchProfileRejectsProfileAndSelectProfile(t *testing.T) {
	cfg := testProfileConfig()

	_, _, err := resolveLaunchProfile(cfg, "foo", true, func(title string, options []string) (string, error) {
		t.Fatalf("picker should not be called")
		return "", nil
	})
	if err == nil || !strings.Contains(err.Error(), "--profile and --select-profile cannot be used together") {
		t.Fatalf("expected conflicting flags error, got %v", err)
	}
}

func TestResolveLaunchProfileFallsBackWhenDefaultMissing(t *testing.T) {
	cfg := testProfileConfig()
	cfg.DefaultProfile = "missing"

	name, profile, err := resolveLaunchProfile(cfg, "", false, func(title string, options []string) (string, error) {
		want := []string{"default", "foo", "zeta"}
		if !reflect.DeepEqual(options, want) {
			t.Fatalf("picker options mismatch, got %v want %v", options, want)
		}
		return "foo", nil
	})
	if err != nil {
		t.Fatalf("resolveLaunchProfile failed: %v", err)
	}
	if name != "foo" || profile != cfg.Profiles["foo"] {
		t.Fatalf("expected foo profile, got name=%q profile=%p", name, profile)
	}
}

func TestResolveLaunchProfileProfileFlagDoesNotFallback(t *testing.T) {
	cfg := testProfileConfig()

	_, _, err := resolveLaunchProfile(cfg, "missing", false, func(title string, options []string) (string, error) {
		t.Fatalf("picker should not be called")
		return "", nil
	})
	if err == nil || !strings.Contains(err.Error(), "profile not found: missing") {
		t.Fatalf("expected missing profile error, got %v", err)
	}
}

func TestResolveQuickLaunchDefaultsUsesLastSelection(t *testing.T) {
	cfg := testProfileConfig()
	cfg.History.LastSelection = "codex"

	selection, err := resolveQuickLaunchDefaults(cfg, []string{"claude", "codex"})
	if err != nil {
		t.Fatalf("resolveQuickLaunchDefaults failed: %v", err)
	}
	if selection.Integration != "codex" || selection.Profile != "default" || selection.Model != "model-default" {
		t.Fatalf("unexpected quick launch selection: %+v", selection)
	}
}

func TestResolveQuickLaunchDefaultsUsesDefaultIntegrationBeforeHistory(t *testing.T) {
	cfg := testProfileConfig()
	cfg.DefaultIntegration = " Codex "
	cfg.History.LastSelection = "claude"

	selection, err := resolveQuickLaunchDefaults(cfg, []string{"claude", "codex"})
	if err != nil {
		t.Fatalf("resolveQuickLaunchDefaults failed: %v", err)
	}
	if selection.Integration != "codex" || cfg.DefaultIntegration != " Codex " {
		t.Fatalf("unexpected quick launch selection: %+v default=%q", selection, cfg.DefaultIntegration)
	}
}

func TestResolveQuickLaunchDefaultsFallsBackToFirstIntegration(t *testing.T) {
	for _, lastSelection := range []string{"", "missing"} {
		cfg := testProfileConfig()
		cfg.History.LastSelection = lastSelection

		selection, err := resolveQuickLaunchDefaults(cfg, []string{"claude", "codex"})
		if err != nil {
			t.Fatalf("resolveQuickLaunchDefaults failed: %v", err)
		}
		if selection.Integration != "claude" {
			t.Fatalf("expected first integration for last selection %q, got %+v", lastSelection, selection)
		}
	}
}

func TestResolveQuickLaunchDefaultsFallsBackToHistoryWhenDefaultIntegrationInvalid(t *testing.T) {
	cfg := testProfileConfig()
	cfg.DefaultIntegration = "missing"
	cfg.History.LastSelection = "codex"

	selection, err := resolveQuickLaunchDefaults(cfg, []string{"claude", "codex"})
	if err != nil {
		t.Fatalf("resolveQuickLaunchDefaults failed: %v", err)
	}
	if selection.Integration != "codex" {
		t.Fatalf("expected history fallback, got %+v", selection)
	}
}

func TestResolveQuickLaunchDefaultsPrefersDefaultModel(t *testing.T) {
	cfg := testProfileConfig()
	cfg.Profiles["default"] = &config.Profile{
		Models:       []string{"model-a", "model-b"},
		DefaultModel: "model-b",
	}

	selection, err := resolveQuickLaunchDefaults(cfg, []string{"claude", "codex"})
	if err != nil {
		t.Fatalf("resolveQuickLaunchDefaults failed: %v", err)
	}
	if selection.Model != "model-b" {
		t.Fatalf("expected default model, got %+v", selection)
	}
}

func TestResolveQuickLaunchDefaultsRequiresModel(t *testing.T) {
	cfg := testProfileConfig()
	cfg.Profiles["default"] = &config.Profile{}

	_, err := resolveQuickLaunchDefaults(cfg, []string{"claude", "codex"})
	if err == nil || !strings.Contains(err.Error(), "Manage profiles") {
		t.Fatalf("expected Manage profiles model error, got %v", err)
	}
}

func TestFormatLaunchLineHighlightsValuesWithoutChangingText(t *testing.T) {
	got := formatLaunchLine("Codex", "gpt-5.5", "azure-free-linux")
	plain := tui.StripANSI(got)
	want := "Launching Codex with gpt-5.5 using profile azure-free-linux"
	if plain != want {
		t.Fatalf("plain launch line mismatch, got %q want %q", plain, want)
	}
	if got == plain {
		t.Fatalf("expected colored launch line, got plain text")
	}
	if strings.Contains(got, "48;") {
		t.Fatalf("expected foreground-only launch highlights, got %q", got)
	}
}

func testProfileConfig() *config.RootConfig {
	return &config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"zeta":    {DefaultModel: "model-z"},
			"default": {DefaultModel: "model-default"},
			"foo":     {DefaultModel: "model-foo"},
		},
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
	foundSkill := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "skill" {
			foundSkill = true
		}
	}
	if !foundSkill {
		t.Fatalf("expected skill command to be registered")
	}
}

func TestProfileSelectionFlagsConflictBeforePrompt(t *testing.T) {
	for _, args := range [][]string{
		{"launch", "--profile", "foo", "--select-profile"},
		{"config", "--profile", "foo", "--select-profile"},
	} {
		root := NewRootCmd()
		buf := &bytes.Buffer{}
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs(args)

		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "--profile and --select-profile cannot be used together") {
			t.Fatalf("expected conflicting flags error for %v, got %v", args, err)
		}
	}
}

func TestDebugDashboardSnapshotRendersCurrentDashboard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"debug", "snapshot", "dashboard", "--width", "90", "--height", "16"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	got := buf.String()
	for _, want := range []string{"Spark", "Quick launch", "Launch options", "Manage settings", "Default profile: default", "Default model: not set", "Config file:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected dashboard snapshot to contain %q, got %q", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("expected plain snapshot without ANSI escapes, got %q", got)
	}
}

func TestDebugNestedSnapshotsRenderSubscreens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "profile",
			args: []string{"debug", "snapshot", "profile", "--width", "120", "--height", "26"},
			want: []string{"Spark Profiles", "Base URL", "Actions"},
		},
		{
			name: "mcp add http",
			args: []string{"debug", "snapshot", "mcp", "--state", "add-http", "--width", "120", "--height", "18"},
			want: []string{"MCP Manager", "Create Server", "Transport", "http"},
		},
		{
			name: "skills transfer",
			args: []string{"debug", "snapshot", "skills", "--state", "transfer", "--width", "120", "--height", "18"},
			want: []string{"Skill Manager", "Transfer Skills", "Import from Codex"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRootCmd()
			buf := &bytes.Buffer{}
			root.SetOut(buf)
			root.SetErr(buf)
			root.SetArgs(tc.args)

			if err := root.Execute(); err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			got := buf.String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("expected nested snapshot to contain %q, got %q", want, got)
				}
			}
		})
	}
}

func TestUsageCommandModelFilterOmitsOtherModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := usage.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath failed: %v", err)
	}
	now := time.Now()
	for _, record := range []usage.Record{
		{Timestamp: now.Add(-1 * time.Hour), Client: "codex", Model: "glm-5.1", InputTokens: 100, OutputTokens: 50},
		{Timestamp: now.Add(-30 * time.Minute), Client: "claude", Model: "other-model", TotalTokens: 900},
	} {
		if err := usage.Append(path, record); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"usage", "--model", "glm-5.1"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"Model: glm-5.1", "glm-5.1", "150"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected usage output to contain %q, got %q", want, got)
		}
	}
	for _, unwanted := range []string{"other-model", "900"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("expected usage output to omit %q, got %q", unwanted, got)
		}
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
		Name:                "brainstorming",
		Scope:               skills.ScopeGlobal,
		SourceKind:          skills.SourceKindLocal,
		SourceType:          skills.SourceTypeLocal,
		Source:              "/tmp/brainstorming",
		Enabled:             true,
		Managed:             true,
		AgentTargets:        []string{"codex", "claude"},
		MaterializationMode: skills.MaterializationCopy,
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
	restoreInstall := stubSkillInstallFromCatalog(func(name string, opts ...skills.InstallOptions) (*skills.SkillEntry, error) {
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
	restoreInstall := stubSkillInstallFromCatalog(func(name string, opts ...skills.InstallOptions) (*skills.SkillEntry, error) {
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
