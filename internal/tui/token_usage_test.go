package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTokenUsageViewIncludesMetrics(t *testing.T) {
	view, err := RenderTokenUsageSnapshot([]TokenUsageSummary{
		{
			Window:            "today",
			Client:            "all",
			Label:             "Today",
			Requests:          2,
			InputTokens:       1000,
			OutputTokens:      500,
			TotalTokens:       1500,
			CachedInputTokens: 250,
		},
	}, 90, 24, 0)
	if err != nil {
		t.Fatalf("RenderTokenUsageSnapshot failed: %v", err)
	}
	plain := StripANSI(view)
	for _, want := range []string{"Token usage", "Today", "All", "Total tokens", "1,500", "Cached tokens", "250", "Requests"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected view to contain %q, got %q", want, plain)
		}
	}
}

func TestTokenUsageModelSwitchesWindowsAndQuits(t *testing.T) {
	m := newTokenUsageModel([]TokenUsageSummary{
		{Window: "today", Client: "all", Label: "Today"},
		{Window: "7d", Client: "all", Label: "7d"},
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

func TestTokenUsageModelSwitchesClients(t *testing.T) {
	m := newTokenUsageModel([]TokenUsageSummary{
		{Window: "today", Client: "all", Label: "Today", TotalTokens: 30},
		{Window: "today", Client: "claude", Label: "Today", TotalTokens: 10},
		{Window: "today", Client: "codex", Label: "Today", TotalTokens: 20},
	})
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*tokenUsageModel)
	if updated.client != 1 {
		t.Fatalf("client = %d, want 1", updated.client)
	}
	if got := updated.currentSummary().Client; got != "claude" {
		t.Fatalf("current client = %q, want claude", got)
	}
}

func TestTokenUsageModelRefreshReloadsSummaries(t *testing.T) {
	loads := 0
	m := newTokenUsageModel([]TokenUsageSummary{
		{Window: "today", Client: "all", Label: "Today", TotalTokens: 30},
	}, func() ([]TokenUsageSummary, error) {
		loads++
		return []TokenUsageSummary{
			{Window: "today", Client: "all", Label: "Today", TotalTokens: 90},
		}, nil
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
	if got := updated.currentSummary().TotalTokens; got != 90 {
		t.Fatalf("total tokens = %d, want 90", got)
	}
	if updated.status != "Refreshed token usage" {
		t.Fatalf("status = %q, want refreshed", updated.status)
	}
}
