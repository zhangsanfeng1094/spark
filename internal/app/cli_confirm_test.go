package app

import (
	"strings"
	"testing"
)

func TestEditorLaunchConfirmRequest(t *testing.T) {
	req := editorLaunchConfirmRequest(
		"Grok Build",
		"work",
		[]string{"grok-4.5"},
		[]string{"/home/me/.grok/config.toml"},
		"/tmp/spark-backups",
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
	if !strings.Contains(req.Footnote, "/tmp/spark-backups") {
		t.Fatalf("footnote=%q", req.Footnote)
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
	req := editorLaunchConfirmRequest("OpenCode", "default", []string{"a", "b"}, nil, "/tmp/x")
	joined := strings.Join(req.Details, "\n")
	if !strings.Contains(joined, "Models:      a, b") {
		t.Fatalf("details=%s", joined)
	}
}
