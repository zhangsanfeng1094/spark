package codex

import (
	"testing"

	"spark/internal/compat/ir"
)

func TestResponsesClientResponseKeepsReasoningSeparateFromOutputText(t *testing.T) {
	out := ResponsesClientResponse(ir.Response{
		ID:    "chatcmpl_1",
		Model: "deepseek-reasoner",
		Output: []ir.ContentBlock{
			ir.Reasoning("think first"),
			ir.Text("final answer"),
		},
	})

	if out["output_text"] != "final answer" {
		t.Fatalf("output_text mismatch: %#v", out["output_text"])
	}
	output := out["output"].([]map[string]any)
	if len(output) != 2 || output[0]["type"] != "reasoning" || output[1]["type"] != "message" {
		t.Fatalf("output item order mismatch: %#v", output)
	}
}

func TestResponsesClientResponseMapsToolCalls(t *testing.T) {
	out := ResponsesClientResponse(ir.Response{
		ID:    "chatcmpl_1",
		Model: "GLM-4.7",
		Output: []ir.ContentBlock{
			{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					ID:        "call_1",
					Type:      ir.ToolTypeFunction,
					Name:      "sum",
					Arguments: `{"a":1}`,
				},
			},
		},
	})

	output := out["output"].([]map[string]any)
	if len(output) != 1 {
		t.Fatalf("output mismatch: %#v", output)
	}
	item := output[0]
	if item["type"] != "function_call" || item["call_id"] != "call_1" || item["name"] != "sum" {
		t.Fatalf("function_call item mismatch: %#v", item)
	}
}

func TestResponsesUsageMapsChatUsageDetails(t *testing.T) {
	got, ok := ResponsesUsage(ir.Usage{
		InputTokens:  10,
		OutputTokens: 4,
		TotalTokens:  14,
		Raw: map[string]any{
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(3),
			},
			"completion_tokens_details": map[string]any{
				"reasoning_tokens": float64(2),
			},
		},
	})
	if !ok {
		t.Fatal("expected usage")
	}
	if got["input_tokens"] != 10 || got["output_tokens"] != 4 || got["total_tokens"] != 14 {
		t.Fatalf("usage tokens mismatch: %#v", got)
	}
	if got["cached_input_tokens"] != 3 || got["reasoning_output_tokens"] != 2 {
		t.Fatalf("usage details mismatch: %#v", got)
	}
}
