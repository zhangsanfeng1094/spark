package app

import (
	"fmt"
	"strings"
	"testing"

	"spark/internal/config"
	"spark/internal/integrations"
)

func TestEditorLaunchConfirmRequest(t *testing.T) {
	req := editorLaunchConfirmRequest(
		"Grok Build",
		"work",
		[]string{"grok-4.5"},
		[]string{"/home/me/.grok/config.toml"},
	)
	if req.Title != "Apply Spark config for Grok Build?" {
		t.Fatalf("title=%q", req.Title)
	}
	if !strings.Contains(req.Summary, "writes provider/model settings") {
		t.Fatalf("summary=%q", req.Summary)
	}
	if req.ConfirmLabel != "Write config & launch" || req.CancelLabel != "Cancel" {
		t.Fatalf("labels confirm=%q cancel=%q", req.ConfirmLabel, req.CancelLabel)
	}
	if !req.DefaultConfirm {
		t.Fatal("expected default confirm")
	}
	if !strings.Contains(req.Footnote, "does not create a backup") {
		t.Fatalf("footnote=%q", req.Footnote)
	}
	if strings.Contains(req.Footnote, "spark-backups") {
		t.Fatalf("footnote should not claim a backup directory: %q", req.Footnote)
	}
	joined := strings.Join(req.Details, "\n")
	for _, want := range []string{
		"Integration: Grok Build",
		"Profile:     work",
		"Model:       grok-4.5",
		"Files Spark will update:",
		"/home/me/.grok/config.toml",
		"spark-* models",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in details:\n%s", want, joined)
		}
	}
}

func TestEditorLaunchConfirmRequestMultipleModels(t *testing.T) {
	req := editorLaunchConfirmRequest("OpenCode", "default", []string{"a", "b"}, nil)
	joined := strings.Join(req.Details, "\n")
	if !strings.Contains(joined, "Models:      a, b") {
		t.Fatalf("details=%s", joined)
	}
}

type stubEditorRunner struct {
	needs    bool
	needsErr error
	editErr  error
	edits    int
}

func (s *stubEditorRunner) String() string { return "Stub" }
func (s *stubEditorRunner) Run(*config.Profile, string, []string) error {
	return nil
}
func (s *stubEditorRunner) Paths() []string  { return []string{"/tmp/stub.toml"} }
func (s *stubEditorRunner) Models() []string { return nil }
func (s *stubEditorRunner) NeedsEdit(*config.Profile, []string) (bool, error) {
	return s.needs, s.needsErr
}
func (s *stubEditorRunner) Edit(*config.Profile, []string) error {
	s.edits++
	return s.editErr
}

func TestApplyEditorIfNeededSkipsConfirmWhenConfigMatches(t *testing.T) {
	stub := &stubEditorRunner{needs: false}
	confirmCalls := 0
	continueLaunch, err := applyEditorIfNeeded(stub, stub, &config.Profile{}, "work", []string{"grok-4.5"}, func(string, string, []string, []string) (bool, error) {
		confirmCalls++
		return true, nil
	})
	if err != nil {
		t.Fatalf("applyEditorIfNeeded: %v", err)
	}
	if !continueLaunch {
		t.Fatal("matching config should continue launch")
	}
	if confirmCalls != 0 {
		t.Fatalf("confirm calls=%d want 0", confirmCalls)
	}
	if stub.edits != 0 {
		t.Fatalf("edits=%d want 0", stub.edits)
	}
}

func TestApplyEditorIfNeededConfirmsWhenConfigChanges(t *testing.T) {
	stub := &stubEditorRunner{needs: true}
	continueLaunch, err := applyEditorIfNeeded(stub, stub, &config.Profile{}, "work", []string{"grok-4.5"}, func(integration, profileName string, models, paths []string) (bool, error) {
		if integration != "Stub" || profileName != "work" || models[0] != "grok-4.5" {
			t.Fatalf("unexpected confirm args: %s %s %v", integration, profileName, models)
		}
		if paths[0] != "/tmp/stub.toml" {
			t.Fatalf("paths=%v", paths)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("applyEditorIfNeeded: %v", err)
	}
	if !continueLaunch {
		t.Fatal("confirmed write should continue launch")
	}
	if stub.edits != 1 {
		t.Fatalf("edits=%d want 1", stub.edits)
	}
}

func TestApplyEditorIfNeededCancelDoesNotWrite(t *testing.T) {
	stub := &stubEditorRunner{needs: true}
	continueLaunch, err := applyEditorIfNeeded(stub, stub, &config.Profile{}, "work", []string{"grok-4.5"}, func(string, string, []string, []string) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatalf("applyEditorIfNeeded: %v", err)
	}
	if continueLaunch {
		t.Fatal("cancel should stop launch")
	}
	if stub.edits != 0 {
		t.Fatalf("edits=%d want 0", stub.edits)
	}
}

func TestApplyEditorIfNeededWithoutCheckerAlwaysConfirms(t *testing.T) {
	ed := &alwaysEditor{}
	confirmCalls := 0
	continueLaunch, err := applyEditorIfNeeded(ed, ed, &config.Profile{}, "work", []string{"m"}, func(string, string, []string, []string) (bool, error) {
		confirmCalls++
		return true, nil
	})
	if err != nil {
		t.Fatalf("applyEditorIfNeeded: %v", err)
	}
	if !continueLaunch || confirmCalls != 1 || ed.edits != 1 {
		t.Fatalf("continue=%v confirm=%d edits=%d", continueLaunch, confirmCalls, ed.edits)
	}
}

func TestApplyEditorIfNeededNeedsEditError(t *testing.T) {
	stub := &stubEditorRunner{needsErr: fmt.Errorf("read failed")}
	_, err := applyEditorIfNeeded(stub, stub, &config.Profile{}, "work", []string{"m"}, func(string, string, []string, []string) (bool, error) {
		t.Fatal("confirm should not run")
		return false, nil
	})
	if err == nil || err.Error() != "read failed" {
		t.Fatalf("err=%v", err)
	}
}

type alwaysEditor struct {
	edits int
}

func (a *alwaysEditor) String() string { return "Always" }
func (a *alwaysEditor) Run(*config.Profile, string, []string) error {
	return nil
}
func (a *alwaysEditor) Paths() []string  { return []string{"/tmp/always.toml"} }
func (a *alwaysEditor) Models() []string { return nil }
func (a *alwaysEditor) Edit(*config.Profile, []string) error {
	a.edits++
	return nil
}

var (
	_ integrations.Runner      = (*stubEditorRunner)(nil)
	_ integrations.Editor      = (*stubEditorRunner)(nil)
	_ integrations.EditChecker = (*stubEditorRunner)(nil)
	_ integrations.Runner      = (*alwaysEditor)(nil)
	_ integrations.Editor      = (*alwaysEditor)(nil)
)
