package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/config"
	"spark/internal/skills"
)

func TestQuitOnScreenBackKeys(t *testing.T) {
	for _, key := range []string{"esc", "q", "ctrl+c"} {
		cmd, ok := quitOnScreenBack(key)
		if !ok || cmd == nil {
			t.Fatalf("expected %q to quit screen", key)
		}
	}
	if cmd, ok := quitOnScreenBack("tab"); ok || cmd != nil {
		t.Fatalf("tab should not quit screen")
	}
}

func TestMCPManagerBrowseEscQuits(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{})
	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected esc on MCP browse to quit")
	}
}

func TestSkillManagerBrowseEscQuits(t *testing.T) {
	reg := &skills.Registry{Skills: map[string]*skills.SkillEntry{}}
	m := newSkillManagerModel(reg)
	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected esc on skill browse to quit")
	}
}

func TestProfileManagerQQuitsFromProfilesFocus(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://example.com/v1"},
		},
	})
	m.focusArea = pmFocusProfiles
	cmd, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !handled || cmd == nil {
		t.Fatal("expected q on profiles focus to quit")
	}

	m.focusArea = pmFocusFields
	cmd, handled = m.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if handled || cmd != nil {
		t.Fatal("expected q on fields focus not to quit immediately")
	}
}

func TestMCPManagerHelpMentionsScreenBack(t *testing.T) {
	m := newMCPManagerModel(&config.RootConfig{})
	help := m.contextHelpText()
	if !strings.Contains(help, "Esc/Q Back") {
		t.Fatalf("browse help should mention Esc/Q Back, got %q", help)
	}
}
