package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
)

func TestLaunchOptionsInitialSelectionUsesQuickLaunchDefaults(t *testing.T) {
	cfg := testLaunchOptionsConfig()

	m := newLaunchOptionsModel([]string{"claude", "codex"}, cfg, "codex")
	selection := m.currentSelection()

	if selection.Integration != "codex" || selection.Profile != "work" || selection.Model != "work-default" {
		t.Fatalf("unexpected launch selection: %+v", selection)
	}
}

func TestLaunchOptionsProfileSwitchRefreshesModels(t *testing.T) {
	cfg := testLaunchOptionsConfig()
	m := newLaunchOptionsModel([]string{"claude", "codex"}, cfg, "codex")
	m.activeColumn = launchOptionsColumnProfile

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})

	wantModels := []string{"default-default", "default-a"}
	if !reflect.DeepEqual(m.models, wantModels) {
		t.Fatalf("models mismatch after profile switch, got %v want %v", m.models, wantModels)
	}
	if m.currentSelection().Profile != "default" || m.currentSelection().Model != "default-default" {
		t.Fatalf("unexpected selection after profile switch: %+v", m.currentSelection())
	}
}

func TestLaunchOptionsEnterReturnsCompleteSelection(t *testing.T) {
	cfg := testLaunchOptionsConfig()
	m := newLaunchOptionsModel([]string{"claude", "codex"}, cfg, "codex")

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	want := LaunchSelection{Integration: "codex", Profile: "work", Model: "work-default"}
	if m.selected != want {
		t.Fatalf("selected mismatch, got %+v want %+v", m.selected, want)
	}
}

func TestLaunchOptionsModelColumnScrollsWithinViewport(t *testing.T) {
	models := make([]string, 20)
	for i := range models {
		models[i] = "model-" + string(rune('a'+i))
	}
	cfg := &config.RootConfig{
		DefaultProfile: "work",
		Profiles: map[string]*config.Profile{
			"work": {Models: models},
		},
	}
	m := newLaunchOptionsModel([]string{"codex"}, cfg, "codex")
	m.width = 90
	m.height = 12
	m.activeColumn = launchOptionsColumnModel
	m.modelCursor = 15

	view := StripANSI(m.View())
	for _, want := range []string{"^ more", "> model-p", "v more", "Enter Launch"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected scrolled launch options view to contain %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "model-a") {
		t.Fatalf("expected early models to be windowed out, got %q", view)
	}
}

func testLaunchOptionsConfig() *config.RootConfig {
	return &config.RootConfig{
		DefaultProfile: "work",
		Profiles: map[string]*config.Profile{
			"default": {
				Models:       []string{"default-a"},
				DefaultModel: "default-default",
			},
			"work": {
				Models:       []string{"work-a"},
				DefaultModel: "work-default",
			},
		},
	}
}
