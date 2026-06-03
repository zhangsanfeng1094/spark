package openai_responses

import (
	"testing"

	"spark/internal/compat/ir"
	"spark/internal/compat/policy"
)

func TestOutboundBuildRequestMapsIRToResponses(t *testing.T) {
	max := 123
	req := ir.Request{
		Model: "gpt-4.1",
		Messages: []ir.Message{
			{Role: ir.RoleSystem, Content: []ir.ContentBlock{ir.Text("be concise")}},
			{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text("hello")}},
			{
				Role: ir.RoleAssistant,
				Content: []ir.ContentBlock{
					ir.Reasoning("think"),
					ir.Text("calling"),
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
			},
			{
				Role: ir.RoleTool,
				Content: []ir.ContentBlock{
					{
						Type: ir.BlockToolResult,
						ToolResult: &ir.ToolResult{
							ToolCallID: "call_1",
							Output:     `{"ok":true}`,
						},
					},
				},
			},
		},
		Tools: []ir.Tool{
			{
				Type: ir.ToolTypeFunction,
				Function: ir.FunctionTool{
					Name:       "sum",
					Parameters: map[string]any{"type": "object"},
				},
			},
		},
		ToolChoice: ir.ToolChoice{Mode: ir.ToolChoiceFunction, Name: "sum"},
		Generation: ir.GenerationConfig{
			MaxTokens: &max,
			Reasoning: ir.ReasoningConfig{
				Effort:  ir.ReasoningEffortHigh,
				Summary: ir.ReasoningSummary("auto"),
			},
		},
		Stream: true,
	}

	out := Outbound{Reasoning: policy.PreserveReasoningContent()}.BuildRequest(req)
	if out["model"] != "gpt-4.1" || out["instructions"] != "be concise" || out["max_output_tokens"] != 123 || out["stream"] != true {
		t.Fatalf("basic fields mismatch: %#v", out)
	}
	reasoning := out["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning mismatch: %#v", reasoning)
	}
	tools := out["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "sum" {
		t.Fatalf("tools mismatch: %#v", tools)
	}
	input := out["input"].([]map[string]any)
	if len(input) != 5 {
		t.Fatalf("input length mismatch: %#v", input)
	}
	if input[1]["type"] != "reasoning" || input[2]["role"] != "assistant" || input[3]["type"] != "function_call" || input[4]["type"] != "function_call_output" {
		t.Fatalf("assistant/tool mapping mismatch: %#v", input)
	}
}

func TestResponseMapsResponsesPayloadToIR(t *testing.T) {
	resp := Response(map[string]any{
		"id":     "resp_1",
		"model":  "gpt-4.1",
		"status": "completed",
		"output": []any{
			map[string]any{
				"type":    "reasoning",
				"summary": []any{map[string]any{"type": "summary_text", "text": "think"}},
			},
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{"type": "output_text", "text": "hello"},
				},
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "sum",
				"arguments": `{"a":1}`,
			},
		},
		"usage": map[string]any{
			"input_tokens":  float64(5),
			"output_tokens": float64(7),
			"total_tokens":  float64(12),
		},
	})

	if resp.ID != "resp_1" || resp.Model != "gpt-4.1" || resp.StopReason != ir.StopReasonEndTurn {
		t.Fatalf("response metadata mismatch: %#v", resp)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 7 || resp.Usage.TotalTokens != 12 {
		t.Fatalf("usage mismatch: %#v", resp.Usage)
	}
	if len(resp.Output) != 3 || resp.Output[0].Reasoning == nil || resp.Output[1].Text != "hello" || resp.Output[2].ToolCall == nil {
		t.Fatalf("output mismatch: %#v", resp.Output)
	}
}

func TestStreamEventsMapsResponsesChunks(t *testing.T) {
	events := StreamEvents(map[string]any{
		"type":         "response.output_text.delta",
		"output_index": float64(0),
		"delta":        "hel",
	})
	if len(events) != 1 || events[0].Delta.Text != "hel" {
		t.Fatalf("text delta mismatch: %#v", events)
	}

	events = StreamEvents(map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"status": "completed",
			"model":  "gpt-4.1",
			"usage": map[string]any{
				"input_tokens":  float64(1),
				"output_tokens": float64(2),
			},
		},
	})
	if len(events) != 2 || events[0].Type != ir.StreamEventUsage || events[1].Type != ir.StreamEventResponseDone {
		t.Fatalf("completed events mismatch: %#v", events)
	}
}
