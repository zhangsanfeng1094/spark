package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendReadAndSummaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	if err := Append(path, Record{Timestamp: now, Client: "codex", InputTokens: 10, OutputTokens: 5, CachedInputTokens: 3}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := Append(path, Record{Timestamp: now.AddDate(0, 0, -10), Client: "claude", TotalTokens: 8, CachedInputTokens: 1}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	if _, err := f.WriteString("{bad json\n"); err != nil {
		t.Fatalf("write bad line failed: %v", err)
	}
	_ = f.Close()

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
