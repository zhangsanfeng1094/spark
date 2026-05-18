package openai_chat

import (
	"testing"

	"spark/internal/usage"
)

func TestChatResponseParsesIRUsageWithoutRecording(t *testing.T) {
	var records []usage.Record
	usage.SetRecorder(func(record usage.Record) error {
		records = append(records, record)
		return nil
	})
	defer usage.SetRecorder(nil)

	resp := ChatResponse(map[string]any{
		"id":    "chatcmpl-test",
		"model": "gpt-test",
		"usage": map[string]any{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(5),
			"total_tokens":      float64(15),
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(4),
			},
		},
	})

	if resp.Usage.TotalTokens != 15 {
		t.Fatalf("IR usage total = %d, want 15", resp.Usage.TotalTokens)
	}
	if resp.Usage.CacheReadInputTokens != 4 {
		t.Fatalf("IR cached tokens = %d, want 4", resp.Usage.CacheReadInputTokens)
	}
	if len(records) != 0 {
		t.Fatalf("ChatResponse should not record usage, got %#v", records)
	}
}

func TestChatStreamEventsRecordsIRUsage(t *testing.T) {
	var records []usage.Record
	usage.SetRecorder(func(record usage.Record) error {
		records = append(records, record)
		return nil
	})
	defer usage.SetRecorder(nil)

	events := ChatStreamEvents(map[string]any{
		"id":    "chatcmpl-test",
		"model": "gpt-test",
		"usage": map[string]any{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(5),
		},
	})

	if len(events) != 1 || events[0].Usage == nil {
		t.Fatalf("expected one usage event, got %#v", events)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	if !records[0].Stream || records[0].TotalTokens != 15 {
		t.Fatalf("unexpected stream record: %#v", records[0])
	}
}

func TestChatResponseParsesResponsesStyleUsageWithoutRecording(t *testing.T) {
	var records []usage.Record
	usage.SetRecorder(func(record usage.Record) error {
		records = append(records, record)
		return nil
	})
	defer usage.SetRecorder(nil)

	resp := ChatResponse(map[string]any{
		"model": "gpt-test",
		"usage": map[string]any{
			"input_tokens":  float64(8),
			"output_tokens": float64(3),
			"input_tokens_details": map[string]any{
				"cached_tokens": float64(2),
			},
		},
	})

	if resp.Usage.InputTokens != 8 || resp.Usage.OutputTokens != 3 || resp.Usage.TotalTokens != 11 {
		t.Fatalf("IR usage mismatch: %#v", resp.Usage)
	}
	if resp.Usage.CacheReadInputTokens != 2 {
		t.Fatalf("IR cached tokens mismatch: %#v", resp.Usage)
	}
	if len(records) != 0 {
		t.Fatalf("ChatResponse should not record usage, got %#v", records)
	}
}
