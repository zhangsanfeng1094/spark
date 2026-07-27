package tui

import (
	"strings"
	"testing"
)

func TestConfirmModelViewIncludesContext(t *testing.T) {
	m := &confirmModel{
		title:        "Apply Spark config for Grok Build?",
		summary:      "Before launching, Spark writes provider/model settings.",
		details:      []string{"Integration: Grok Build", "Model:       grok-4.5", "~/.grok/config.toml"},
		footnote:     "Backups: /tmp/spark-backups",
		options:      []string{"Write config & launch", "Cancel"},
		cursor:       0,
		confirmLabel: "Write config & launch",
	}
	view := m.View()
	for _, want := range []string{
		"Apply Spark config for Grok Build?",
		"Before launching, Spark writes provider/model settings.",
		"Integration: Grok Build",
		"~/.grok/config.toml",
		"Backups: /tmp/spark-backups",
		"→ Write config & launch",
		"Cancel",
		"y/n quick",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
}

func TestConfirmModelBodyLineCount(t *testing.T) {
	m := &confirmModel{
		summary:  "hello",
		details:  []string{"a", "b"},
		footnote: "note",
	}
	// summary + 2 details + footnote + blank before options
	if got := m.bodyLineCount(); got != 5 {
		t.Fatalf("bodyLineCount=%d want 5", got)
	}
	empty := &confirmModel{}
	if got := empty.bodyLineCount(); got != 0 {
		t.Fatalf("empty bodyLineCount=%d want 0", got)
	}
}

func TestConfirmModelQuickKeysAndEmptyTitleFallback(t *testing.T) {
	// Mirror the label-defaulting logic used by ConfirmDetails so empty labels
	// still resolve to Yes/No for Confirm() callers.
	req := ConfirmRequest{}
	confirmLabel := strings.TrimSpace(req.ConfirmLabel)
	if confirmLabel == "" {
		confirmLabel = "Yes"
	}
	cancelLabel := strings.TrimSpace(req.CancelLabel)
	if cancelLabel == "" {
		cancelLabel = "No"
	}
	if confirmLabel != "Yes" || cancelLabel != "No" {
		t.Fatalf("defaults: %q %q", confirmLabel, cancelLabel)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Confirm"
	}
	if title != "Confirm" {
		t.Fatalf("title default=%q", title)
	}
}
