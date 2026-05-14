package compatir

import (
	"reflect"
	"testing"
)

func TestMessageToolCallIDsPreserveStableIDs(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			Text("calling"),
			{
				Type: BlockToolCall,
				ToolCall: &ToolCall{
					ID:        "call_1",
					Type:      ToolTypeFunction,
					Name:      "sum",
					Arguments: `{"a":1}`,
				},
			},
			{
				Type: BlockToolCall,
				ToolCall: &ToolCall{
					ID:        "call_2",
					Type:      ToolTypeFunction,
					Name:      "diff",
					Arguments: `{"a":1}`,
				},
			},
		},
	}

	got := msg.ToolCallIDs()
	want := []string{"call_1", "call_2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool call ids mismatch: got %#v want %#v", got, want)
	}
}

func TestReasoningBlockStaysSeparateFromText(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			Reasoning("think first"),
			Text("final answer"),
		},
	}

	if got := msg.Text(); got != "final answer" {
		t.Fatalf("text mismatch: %q", got)
	}
	if got := msg.ReasoningText(); got != "think first" {
		t.Fatalf("reasoning mismatch: %q", got)
	}
}

func TestUsageAddMergesTokenCountsAndRawFields(t *testing.T) {
	got := Usage{
		InputTokens:  10,
		OutputTokens: 4,
		Raw: map[string]any{
			"provider": "first",
		},
	}.Add(Usage{
		InputTokens:  3,
		OutputTokens: 2,
		Raw: map[string]any{
			"provider": "second",
			"cached":   true,
		},
	})

	if got.InputTokens != 13 || got.OutputTokens != 6 || got.TotalTokens != 19 {
		t.Fatalf("usage mismatch: %#v", got)
	}
	if got.Raw["provider"] != "second" || got.Raw["cached"] != true {
		t.Fatalf("raw merge mismatch: %#v", got.Raw)
	}
}
