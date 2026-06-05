package usage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendReadAndSummaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	if err := Append(path, Record{Timestamp: now, Client: "codex", InputTokens: 10, OutputTokens: 5, CachedInputTokens: 3}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := Append(path, Record{Timestamp: now.AddDate(0, 0, -10), Client: "claude", TotalTokens: 8, CachedInputTokens: 1}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	summaries, err := Summaries(path, now)
	if err != nil {
		t.Fatalf("Summaries failed: %v", err)
	}
	byWindow := map[Window]Summary{}
	for _, summary := range summaries {
		byWindow[summary.Window] = summary
	}
	if got := byWindow[WindowToday].TotalTokens; got != 15 {
		t.Fatalf("today total = %d, want 15", got)
	}
	if got := byWindow[Window7D].Requests; got != 1 {
		t.Fatalf("7d requests = %d, want 1", got)
	}
	if got := byWindow[Window30D].TotalTokens; got != 23 {
		t.Fatalf("30d total = %d, want 23", got)
	}
	if got := byWindow[WindowAll].CachedInputTokens; got != 4 {
		t.Fatalf("all cached = %d, want 4", got)
	}
}

func TestQueryWindowsUsesSQLiteAggregates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{Timestamp: now.Add(-2 * time.Hour), Client: "codex", Model: "gpt-5", InputTokens: 100, OutputTokens: 50, CachedInputTokens: 20},
		{Timestamp: now.Add(-1 * time.Hour), Client: "claude", Model: "sonnet", TotalTokens: 300},
		{Timestamp: now.AddDate(0, 0, -1), Client: "", Model: "", TotalTokens: 40},
		{Timestamp: now.AddDate(0, 0, -10), Client: "codex", Model: "gpt-5", TotalTokens: 900},
	}
	for _, record := range records {
		if err := Append(path, record); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	windows, count, err := QueryWindows(path, now)
	if err != nil {
		t.Fatalf("QueryWindows failed: %v", err)
	}
	if count != 4 {
		t.Fatalf("count = %d, want 4", count)
	}
	if got := windows[WindowToday].Summary.TotalTokens; got != 450 {
		t.Fatalf("today total = %d, want 450", got)
	}
	if got := windows[Window7D].Breakdowns[0].TotalTokens; got != 300 {
		t.Fatalf("top 7d breakdown total = %d, want 300", got)
	}
	if got := windows[WindowToday].Series[11].TotalTokens; got != 300 {
		t.Fatalf("11:00 hourly total = %d, want 300", got)
	}
	if got := windows[WindowAll].HeavyRequests[0].TotalTokens; got != 900 {
		t.Fatalf("top heavy total = %d, want 900", got)
	}
}

func TestQueryWindowsWithFilterRestrictsModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{Timestamp: now.Add(-2 * time.Hour), Client: "codex", Model: "glm-5.1", InputTokens: 100, OutputTokens: 50},
		{Timestamp: now.Add(-1 * time.Hour), Client: "codex", Model: "gpt-5", TotalTokens: 300},
		{Timestamp: now.AddDate(0, 0, -1), Client: "claude", Model: "glm-5.1", TotalTokens: 40},
	}
	for _, record := range records {
		if err := Append(path, record); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	windows, count, err := QueryWindowsWithFilter(path, now, QueryFilter{Model: " glm-5.1 "})
	if err != nil {
		t.Fatalf("QueryWindowsWithFilter failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if got := windows[WindowAll].Summary.TotalTokens; got != 190 {
		t.Fatalf("all total = %d, want 190", got)
	}
	if got := windows[WindowToday].Summary.TotalTokens; got != 150 {
		t.Fatalf("today total = %d, want 150", got)
	}
	for _, row := range windows[WindowAll].Breakdowns {
		if row.Model != "glm-5.1" {
			t.Fatalf("unexpected model in breakdown: %#v", row)
		}
	}
	for _, row := range windows[WindowAll].HeavyRequests {
		if row.Model != "glm-5.1" {
			t.Fatalf("unexpected model in heavy requests: %#v", row)
		}
	}
}

func TestSummarizeForClients(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{Timestamp: now, Client: "codex", TotalTokens: 10},
		{Timestamp: now, Client: "claude", TotalTokens: 20},
	}

	summaries := SummarizeForClients(records, []string{ClientAll(), "claude", "codex"}, now)
	byKey := map[string]Summary{}
	for _, summary := range summaries {
		if summary.Window == WindowToday {
			byKey[summary.Client] = summary
		}
	}
	if got := byKey[ClientAll()].TotalTokens; got != 30 {
		t.Fatalf("all total = %d, want 30", got)
	}
	if got := byKey["claude"].TotalTokens; got != 20 {
		t.Fatalf("claude total = %d, want 20", got)
	}
	if got := byKey["codex"].TotalTokens; got != 10 {
		t.Fatalf("codex total = %d, want 10", got)
	}
}

func TestRecordFromUsageMap(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	record, ok := RecordFromUsageMap(map[string]any{
		"input_tokens":  float64(10),
		"output_tokens": float64(5),
		"input_tokens_details": map[string]any{
			"cached_tokens": float64(4),
		},
	}, "gpt-4.1", true, now)
	if !ok {
		t.Fatal("expected usage record")
	}
	if record.TotalTokens != 15 || record.CachedInputTokens != 4 || !record.Stream || record.Model != "gpt-4.1" {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestRecordFromUsageMapIncludesAnthropicCacheTokens(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	record, ok := RecordFromUsageMap(map[string]any{
		"input_tokens":                float64(21),
		"cache_creation_input_tokens": float64(1000),
		"cache_read_input_tokens":     float64(200),
		"output_tokens":               float64(393),
	}, "claude-test", false, now)
	if !ok {
		t.Fatal("expected usage record")
	}
	if record.InputTokens != 1221 || record.OutputTokens != 393 || record.TotalTokens != 1614 || record.CachedInputTokens != 200 {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestWindowBreakdownsDailySeriesAndHeavyRequests(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{Timestamp: now.Add(-1 * time.Hour), Client: "codex", Model: "gpt-5", InputTokens: 100, OutputTokens: 50, CachedInputTokens: 20},
		{Timestamp: now.Add(-2 * time.Hour), Client: "claude", Model: "sonnet", TotalTokens: 300, CachedInputTokens: 30},
		{Timestamp: now.AddDate(0, 0, -1), Client: "", Model: "", TotalTokens: 40},
		{Timestamp: now.AddDate(0, 0, -10), Client: "codex", Model: "gpt-5", TotalTokens: 900},
	}

	summary := SummarizeWindow(records, WindowToday, now)
	if summary.TotalTokens != 450 || summary.Requests != 2 {
		t.Fatalf("today summary = %#v, want 450 tokens across 2 requests", summary)
	}

	breakdowns := BreakdownsForWindow(records, Window7D, now)
	if len(breakdowns) != 3 {
		t.Fatalf("breakdown rows = %d, want 3", len(breakdowns))
	}
	if breakdowns[0].Client != "claude" || breakdowns[0].Model != "sonnet" || breakdowns[0].TotalTokens != 300 {
		t.Fatalf("top breakdown = %#v, want claude sonnet 300", breakdowns[0])
	}
	if breakdowns[2].Client != "unknown" || breakdowns[2].Model != "unknown model" {
		t.Fatalf("unknown breakdown = %#v", breakdowns[2])
	}

	daily := DailySeriesForWindow(records, Window7D, now)
	if len(daily) != 7 {
		t.Fatalf("daily points = %d, want 7", len(daily))
	}
	if got := daily[len(daily)-1].TotalTokens; got != 450 {
		t.Fatalf("today daily total = %d, want 450", got)
	}
	if got := daily[len(daily)-2].TotalTokens; got != 40 {
		t.Fatalf("yesterday daily total = %d, want 40", got)
	}

	hourly := HourlySeriesForToday(records, now)
	if len(hourly) != 13 {
		t.Fatalf("hourly points = %d, want 13", len(hourly))
	}
	if got := hourly[10].TotalTokens; got != 300 {
		t.Fatalf("10:00 hourly total = %d, want 300", got)
	}
	if got := hourly[11].TotalTokens; got != 150 {
		t.Fatalf("11:00 hourly total = %d, want 150", got)
	}

	heavy := HeavyRequestsForWindow(records, WindowAll, now, 2)
	if len(heavy) != 2 {
		t.Fatalf("heavy requests = %d, want 2", len(heavy))
	}
	if heavy[0].TotalTokens != 900 || heavy[1].TotalTokens != 300 {
		t.Fatalf("heavy requests sorted = %#v", heavy)
	}
}
