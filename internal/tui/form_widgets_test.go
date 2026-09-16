package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestSpliceAnsiLine(t *testing.T) {
	// 1. Plain text splicing
	bg := "Hello World, Welcome to TUI"
	overlay := "###"
	got := spliceAnsiLine(bg, overlay, 6)
	wantPlain := "Hello ###ld, Welcome to TUI"
	if ansi.Strip(got) != wantPlain {
		t.Fatalf("spliceAnsiLine plain text got %q, want %q", ansi.Strip(got), wantPlain)
	}

	// 2. ANSI styled background
	bgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Background(lipgloss.Color("#222222"))
	styledBg := bgStyle.Render("12345678901234567890")
	boxStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#7d56f4")).Bold(true)
	styledOverlay := boxStyle.Render("ABCDE")

	spliced := spliceAnsiLine(styledBg, styledOverlay, 5)
	if lipgloss.Width(spliced) != lipgloss.Width(styledBg) {
		t.Fatalf("expected visual width %d, got %d", lipgloss.Width(styledBg), lipgloss.Width(spliced))
	}
	splicedPlain := ansi.Strip(spliced)
	if splicedPlain != "12345ABCDE1234567890"[:20] {
		t.Fatalf("expected plain %q, got %q", "12345ABCDE1234567890"[:20], splicedPlain)
	}

	// 3. Splicing at x=0
	spliced0 := spliceAnsiLine(bg, "Hi", 0)
	if !strings.HasPrefix(ansi.Strip(spliced0), "Hillo") {
		t.Fatalf("expected prefix 'Hillo', got %q", ansi.Strip(spliced0))
	}

	// 4. Splicing beyond bg width
	shortBg := "Short"
	splicedBeyond := spliceAnsiLine(shortBg, "End", 8)
	if ansi.Strip(splicedBeyond) != "Short   End" {
		t.Fatalf("expected 'Short   End', got %q", ansi.Strip(splicedBeyond))
	}
}

func TestOverlayBox(t *testing.T) {
	bgLines := []string{
		"....................",
		"....................",
		"....................",
		"....................",
		"....................",
	}
	bg := strings.Join(bgLines, "\n")
	boxLines := []string{
		"┌──┐",
		"│OK│",
		"└──┘",
	}
	box := strings.Join(boxLines, "\n")

	overlaid := overlayBox(bg, box, 4, 1)
	lines := strings.Split(overlaid, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	if ansi.Strip(lines[0]) != "...................." {
		t.Errorf("line 0 changed: %q", ansi.Strip(lines[0]))
	}
	if ansi.Strip(lines[1]) != "....┌──┐............" {
		t.Errorf("line 1 mismatch: got %q, want %q", ansi.Strip(lines[1]), "....┌──┐............")
	}
	if ansi.Strip(lines[2]) != "....│OK│............" {
		t.Errorf("line 2 mismatch: got %q, want %q", ansi.Strip(lines[2]), "....│OK│............")
	}
	if ansi.Strip(lines[3]) != "....└──┘............" {
		t.Errorf("line 3 mismatch: got %q, want %q", ansi.Strip(lines[3]), "....└──┘............")
	}
	if ansi.Strip(lines[4]) != "...................." {
		t.Errorf("line 4 changed: %q", ansi.Strip(lines[4]))
	}
}
