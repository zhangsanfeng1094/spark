package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTokenUsageViewIncludesDecisionMetrics(t *testing.T) {
	view, err := RenderTokenUsageSnapshot(testTokenUsageSnapshot(1500), 100, 30, 0)
	if err != nil {
		t.Fatalf("RenderTokenUsageSnapshot failed: %v", err)
	}
	plain := StripANSI(view)
	for _, want := range []string{
		"Token usage",
		"Today",
		"Total 1,500",
		"Avg/request 750",
		"Breakdown by source and model",
		"Codex",
		"gpt-5",
		"Hourly trend",
		"Largest requests",
		"Compat proxy usage only",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected view to contain %q, got %q", want, plain)
		}
	}
}

func TestTokenUsageModelSwitchesWindowsAndQuits(t *testing.T) {
	m := newTokenUsageModel(TokenUsageSnapshot{
		Windows: []TokenUsageWindow{
			{Window: "today", Label: "Today"},
			{Window: "7d", Label: "7d"},
		},
	})
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil {
		t.Fatal("did not expect command for right key")
	}
	updated := model.(*tokenUsageModel)
	if updated.window != 1 {
		t.Fatalf("window = %d, want 1", updated.window)
	}
	_, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected quit command for esc")
	}
}

func TestTokenUsageModelRefreshReloadsSnapshot(t *testing.T) {
	loads := 0
	m := newTokenUsageModel(testTokenUsageSnapshot(30), func() (TokenUsageSnapshot, error) {
		loads++
		return testTokenUsageSnapshot(90), nil
	})

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
	updated := model.(*tokenUsageModel)
	if updated.status != "Refreshing token usage..." {
		t.Fatalf("status = %q, want refreshing", updated.status)
	}

	model, cmd = updated.Update(cmd())
	if cmd != nil {
		t.Fatal("did not expect follow-up command")
	}
	updated = model.(*tokenUsageModel)
	if loads != 1 {
		t.Fatalf("loads = %d, want 1", loads)
	}
	if got := updated.currentWindow().Summary.TotalTokens; got != 90 {
		t.Fatalf("total tokens = %d, want 90", got)
	}
	if updated.status != "Refreshed token usage" {
		t.Fatalf("status = %q, want refreshed", updated.status)
	}
}

func TestTokenUsageViewShowsEmptyState(t *testing.T) {
	view, err := RenderTokenUsageSnapshot(TokenUsageSnapshot{}, 90, 24, 0)
	if err != nil {
		t.Fatalf("RenderTokenUsageSnapshot failed: %v", err)
	}
	plain := StripANSI(view)
	if !strings.Contains(plain, "No recorded compat proxy usage") {
		t.Fatalf("expected empty state, got %q", plain)
	}
}

func testTokenUsageSnapshot(total int) TokenUsageSnapshot {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	return TokenUsageSnapshot{
		SourcePath:  "/tmp/token_usage.jsonl",
		UpdatedAt:   now,
		RecordCount: 2,
		Windows: []TokenUsageWindow{
			{
				Window:     "today",
				Label:      "Today",
				TrendLabel: "Hourly trend",
				Summary: TokenUsageSummary{
					Requests:          2,
					InputTokens:       total * 2 / 3,
					OutputTokens:      total / 3,
					TotalTokens:       total,
					CachedInputTokens: total / 6,
				},
				Breakdowns: []TokenUsageBreakdown{
					{
						Client:            "codex",
						Model:             "gpt-5",
						Requests:          2,
						InputTokens:       total * 2 / 3,
						OutputTokens:      total / 3,
						TotalTokens:       total,
						CachedInputTokens: total / 6,
					},
				},
				DailySeries: []TokenUsageDailyPoint{
					{Label: "11:00", Requests: 1, TotalTokens: total / 3},
					{Label: "12:00", Requests: 1, TotalTokens: total},
				},
				HeavyRequests: []TokenUsageRequest{
					{
						Timestamp:         now,
						Client:            "codex",
						Model:             "gpt-5",
						InputTokens:       total * 2 / 3,
						OutputTokens:      total / 3,
						TotalTokens:       total,
						CachedInputTokens: total / 6,
					},
				},
			},
			{
				Window:  "7d",
				Label:   "7d",
				Summary: TokenUsageSummary{Requests: 2, TotalTokens: total},
			},
		},
	}
}
