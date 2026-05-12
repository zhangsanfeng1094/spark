package tui

import (
	"strings"
	"testing"

	"spark/internal/config"
)

func TestProfileManagerLeftPaneUsesDefaultBadge(t *testing.T) {
	cfg := &config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
			"backup":  {OpenAIBaseURL: "https://example.com/v1"},
		},
	}

	m := newPMModel(cfg)
	left := m.renderLeftPane(0)
	if !strings.Contains(left, "default") {
		t.Fatalf("expected default badge in left pane, got %q", left)
	}
	if strings.Contains(left, "★") {
		t.Fatalf("expected star marker to be removed, got %q", left)
	}
}

func TestProfileManagerHelpTextChangesByFocusArea(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "gptload",
		Profiles: map[string]*config.Profile{
			"gptload": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})

	if got := m.helpText(); !strings.Contains(got, "Enter Edit") {
		t.Fatalf("unexpected profiles help text: %q", got)
	}

	m.focusArea = pmFocusFields
	if got := m.helpText(); !strings.Contains(got, "Ctrl+S Save") {
		t.Fatalf("unexpected fields help text: %q", got)
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
	m.status = "✗ Test failed · 401 Unauthorized"
	if got := m.statusSummaryText(); got != "✗ 401 Unauthorized" {
		t.Fatalf("expected preserved failure summary, got %q", got)
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
	if !strings.Contains(left, "[A] Add") || !strings.Contains(left, "[C] Copy") || !strings.Contains(left, "[F] Default") || !strings.Contains(left, "[D] Del") {
		t.Fatalf("expected two-row left actions with default button, got %q", left)
	}
	if !strings.Contains(left, "[C] Copy") || !strings.Contains(left, "\n [F] Default") {
		t.Fatalf("expected action rows to break between copy and default, got %q", left)
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
	for _, want := range []string{"Actions", "Status", "Help", "[T] Test Connection", "[Ctrl+S] Save"} {
		if !strings.Contains(right, want) {
			t.Fatalf("expected %q in right pane, got %q", want, right)
		}
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
