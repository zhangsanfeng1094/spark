package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"spark/internal/config"
)

func TestProfileManagerLeftPaneUsesDefaultStar(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
			"backup":  {OpenAIBaseURL: "https://example.com/v1"},
		},
	}

	m := newPMModel(cfg)
	left := m.renderLeftPane(0)
	if !strings.Contains(left, "★") {
		t.Fatalf("expected default star in left pane, got %q", left)
	}
	if strings.Contains(left, "default") {
		t.Fatalf("expected default text badge to be removed, got %q", left)
	}
}

func TestProfileManagerHelpTextChangesByFocusArea(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})

	if got := m.helpText(); !strings.Contains(got, "Enter Edit") || strings.Contains(got, "A Add") || strings.Contains(got, "T Test") {
		t.Fatalf("unexpected profiles help text: %q", got)
	}

	m.focusArea = pmFocusFields
	if got := m.helpText(); !strings.Contains(got, "F2 Save") {
		t.Fatalf("unexpected fields help text: %q", got)
	}
}

func TestProfileManagerGlobalLetterShortcutsAreDisabled(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})
	m.focusArea = pmFocusProfiles

	for _, r := range []rune("acdtf") {
		cmd, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if handled || cmd != nil {
			t.Fatalf("expected %q not to trigger a profile action", string(r))
		}
		if m.focusArea != pmFocusProfiles {
			t.Fatalf("expected focus to stay on profiles after %q, got %d", string(r), m.focusArea)
		}
	}
}

func TestProfileManagerFieldInputKeepsTextLetters(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: ""},
		},
	})
	m.focusArea = pmFocusFields
	m.focusField = pmFieldOpenAIBaseURL
	m.fields[m.focusField].value = ""
	m.fields[m.focusField].cursor = 0

	for _, r := range []rune("jkaftd") {
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	if got := m.fields[m.focusField].value; got != "jkaftd" {
		t.Fatalf("expected field input to keep shortcut letters, got %q", got)
	}
}

func TestModelsModalHelpAvoidsTextLetterShortcuts(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {},
		},
	})
	m.modelsDraft = []string{"gpt-4o"}
	m.openModelsModal()

	view := m.overlayModal("")
	for _, blocked := range []string{"N Add", "R Edit", "D Delete", "T Default", "C Clear", "G Fetch"} {
		if strings.Contains(view, blocked) {
			t.Fatalf("expected models modal help to avoid text shortcut %s, got %q", blocked, view)
		}
	}
	for _, want := range []string{"Search", "type text", "F5 Fetch", "F6 Add", "F7 Edit", "F8 Delete", "Enter Select"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected models modal help to contain %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "Ctrl+F Fetch") {
		t.Fatalf("expected models modal help to avoid Ctrl+F Fetch, got %q", view)
	}
	if strings.Contains(view, "F9 Default") {
		t.Fatalf("expected models modal help to omit F9 Default, got %q", view)
	}
}

func TestModelsModalEnterSelectsDefaultAndCloses(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {},
		},
	})
	m.modelsDraft = []string{"gpt-4o", "claude-sonnet-4-20250514"}
	m.defaultModel = "gpt-4o"
	m.openModelsModal()
	m.modelSearchFocused = false
	m.modalCursor = 1

	m.handleModalKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.modalOpen {
		t.Fatal("expected enter to close models modal")
	}
	if got := m.defaultModel; got != "claude-sonnet-4-20250514" {
		t.Fatalf("expected enter to select cursor model as default, got %q", got)
	}
	if got := m.fields[pmFieldModelsCSV].value; !strings.Contains(got, "claude-sonnet-4") {
		t.Fatalf("expected model summary to show selected default, got %q", got)
	}
}

func TestProfileManagerRenderRightPaneShowsLastTestSummary(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})
	m.lastTestSummary = "Connected · 184ms"
	m.lastTestOK = true
	m.statusKind = pmStatusTestSuccess

	right := m.renderRightPane(0)
	if !strings.Contains(right, "Connected · 184ms") {
		t.Fatalf("expected last test summary in action row, got %q", right)
	}
	if !strings.Contains(right, "36 models") && strings.Contains(right, "models loaded") {
		t.Fatalf("expected compact model count wording, got %q", right)
	}
}

func TestProfileManagerFooterOnlyShowsShortcuts(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})

	if got := m.helpText(); strings.Contains(got, "Ready") {
		t.Fatalf("expected footer help text without Ready, got %q", got)
	}
}

func TestProfileManagerRightPaneShowsNeutralStatusWhenIdle(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})

	right := m.renderRightPane(0)
	if !strings.Contains(right, "No recent activity") {
		t.Fatalf("expected neutral status copy in right pane, got %q", right)
	}
	if !strings.Contains(right, "Status") {
		t.Fatalf("expected status heading in right pane, got %q", right)
	}
}

func TestProfileManagerStatusSummaryNormalizesSaveAndFailure(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})

	m.status = "Saved profile gptload."
	if got := m.statusSummaryText(); got != "✓ Saved profile" {
		t.Fatalf("expected normalized save summary, got %q", got)
	}

	m.lastTestSummary = "Connection failed"
	m.lastTestOK = false
	m.statusKind = pmStatusTestError
	m.status = "✗ Test failed · 401 Unauthorized"
	if got := m.statusSummaryText(); got != "✗ 401 Unauthorized" {
		t.Fatalf("expected preserved failure summary, got %q", got)
	}
}

func TestProfileManagerSaveStatusOverridesPreviousTestSummary(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})
	m.lastTestSummary = "Connection failed"
	m.lastTestOK = false
	m.statusKind = pmStatusTestError

	m.setUserStatus(pmStatusSuccess, "Saved profile gptload.")

	if got := m.statusSummaryText(); got != "✓ Saved profile" {
		t.Fatalf("expected save summary to override stale test summary, got %q", got)
	}
	if m.lastTestSummary != "" {
		t.Fatalf("expected previous test summary to be cleared, got %q", m.lastTestSummary)
	}
}

func TestProfileManagerLeftButtonsUseTwoRowsWithDefault(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})

	left := m.renderLeftPane(0)
	for _, want := range []string{"Add", "Copy", "Default", "Del"} {
		if !strings.Contains(left, want) {
			t.Fatalf("expected left actions to contain %q, got %q", want, left)
		}
	}
	for _, blocked := range []string{"[A] Add", "[C] Copy", "[F] Default", "[D] Del"} {
		if strings.Contains(left, blocked) {
			t.Fatalf("expected left actions to avoid letter shortcut %q, got %q", blocked, left)
		}
	}
	if !strings.Contains(left, "F4 Copy") || !strings.Contains(left, "\n F6 Default") {
		t.Fatalf("expected two-row left actions with default button, got %q", left)
	}
}

func TestProfileManagerViewScrollsLongProfileList(t *testing.T) {
	profiles := map[string]*config.Profile{}
	for _, name := range []string{
		"p00", "p01", "p02", "p03", "p04", "p05", "p06", "p07",
		"p08", "p09", "p10", "p11", "p12", "p13", "p14",
	} {
		profiles[name] = &config.Profile{OpenAIBaseURL: "https://api.openai.com/v1"}
	}
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "p00",
		Profiles:       profiles,
	})
	m.width = 100
	m.height = 18
	m.focusArea = pmFocusProfiles
	m.switchProfile(11)

	view := m.View()
	if !strings.Contains(view, "> p11") {
		t.Fatalf("expected selected profile to stay visible, got %q", view)
	}
	if !strings.Contains(view, "↑ more") || !strings.Contains(view, "↓ more") {
		t.Fatalf("expected scroll indicators in long profile list, got %q", view)
	}
	if strings.Contains(view, "p00") {
		t.Fatalf("expected early profiles to be scrolled out, got %q", view)
	}
}

func TestProfileManagerViewFitsNarrowWidthWithLongSecrets(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "hub.linuxdo-0.02-static",
		Profiles: map[string]*config.Profile{
			"hub.linuxdo-0.02-static": {
				OpenAIBaseURL: "https://hub.linux.do/v1",
				APIKey:        strings.Repeat("k", 80),
				Models:        []string{"gpt-5.5"},
				DefaultModel:  "gpt-5.5",
			},
		},
	})
	m.width = 100
	m.height = 32

	view := m.View()
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("expected line to fit width %d, got %d: %q", m.width, got, line)
		}
	}
	if !strings.Contains(view, "★") {
		t.Fatalf("expected default profile star in view, got %q", view)
	}
	if strings.Contains(view, "default") {
		t.Fatalf("expected compact default marker without text badge, got %q", view)
	}
}

func TestProfileManagerRightPaneShowsActionsAndHelpSections(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})
	m.focusArea = pmFocusFields
	m.focusField = pmFieldOpenAIBaseURL

	right := m.renderRightPane(0)
	for _, want := range []string{"Actions", "Status", "Help", "F8 Test", "F2 Save"} {
		if !strings.Contains(right, want) {
			t.Fatalf("expected %q in right pane, got %q", want, right)
		}
	}
	if strings.Contains(right, "[T] Test Connection") {
		t.Fatalf("expected right pane to avoid letter shortcut for test, got %q", right)
	}
	if strings.Index(right, "Help") > strings.Index(right, "Actions") {
		t.Fatalf("expected Help above Actions, got %q", right)
	}

	right = m.renderRightPane(24)
	lines := strings.Split(right, "\n")
	helpLine := -1
	actionLine := -1
	for i, line := range lines {
		if helpLine == -1 && strings.Contains(line, "Help") {
			helpLine = i
		}
		if actionLine == -1 && strings.Contains(line, "Actions") {
			actionLine = i
		}
	}
	if helpLine == -1 || actionLine == -1 || helpLine >= actionLine {
		t.Fatalf("expected Help to stick directly above Actions, help=%d actions=%d pane=%q", helpLine, actionLine, right)
	}
	if actionLine < 2 || strings.TrimSpace(lines[actionLine-1]) != "" || strings.TrimSpace(lines[actionLine-2]) == "" {
		t.Fatalf("expected exactly one blank line between Help content and Actions, pane=%q", right)
	}
}

func TestProfileManagerRightPaneMarksRequiredFields(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})

	right := m.renderRightPane(0)
	if !strings.Contains(right, "* Profile Name") {
		t.Fatalf("expected required marker before profile name, got %q", right)
	}
	if !strings.Contains(right, "* Base URL") {
		t.Fatalf("expected required marker before base URL, got %q", right)
	}
	if strings.Contains(right, "* API Key") {
		t.Fatalf("did not expect API key to be marked required, got %q", right)
	}
}

func TestProfileManagerClickProviderTypeFieldOpensModal(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})
	m.width = 100
	m.height = 30
	_ = m.View()

	x := m.rightContentX + pmLabelWidth + 1
	y := m.rightContentY + m.fieldStartRelY[pmFieldProviderType]
	m.handleMainMouse(tea.MouseMsg{X: x, Y: y})

	if !m.modalOpen || m.modalKind != pmModalKindProviderType {
		t.Fatalf("expected provider type modal, open=%v kind=%d", m.modalOpen, m.modalKind)
	}
}

func TestProfileManagerModelSummaryTruncatesLongDefault(t *testing.T) {
	got := formatModelsSummary([]string{"zai-org/GLM-5.1-FP8"}, "zai-org/GLM-5.1-FP8")
	if !strings.Contains(got, "1 models · zai-org/GLM-5") {
		t.Fatalf("expected truncated model summary, got %q", got)
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("expected ellipsis in model summary, got %q", got)
	}
}
