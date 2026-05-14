package openai

import (
	"testing"

	"spark/internal/compat/policy"
	"spark/internal/compatir"
)

func TestChatOutboundRequiresEmptyReasoningForMimoToolCalls(t *testing.T) {
	req := compatir.Request{
		Model: "mimo-v2.5-pro",
		Messages: []compatir.Message{
			{
				Role: compatir.RoleAssistant,
				Content: []compatir.ContentBlock{
					{
						Type: compatir.BlockToolCall,
						ToolCall: &compatir.ToolCall{
							ID:        "call_1",
							Type:      compatir.ToolTypeFunction,
							Name:      "sum",
							Arguments: `{"a":1}`,
						},
					},
				},
			},
		},
	}

	out := ChatOutbound{
		Reasoning: policy.OpenAIChatReasoningPolicy("https://gateway.example/v1", req.Model),
	}.BuildRequest(req)
	msgs := out["messages"].([]map[string]any)
	if got, ok := msgs[0]["reasoning_content"]; !ok || got != "" {
		t.Fatalf("expected empty reasoning_content, got %#v", msgs[0])
	}
}

func TestChatOutboundStripsReasoningForGenericOpenAI(t *testing.T) {
	req := compatir.Request{
		Model: "gpt-4.1",
		Messages: []compatir.Message{
			{
				Role: compatir.RoleAssistant,
				Content: []compatir.ContentBlock{
					compatir.Reasoning("think first"),
					{
						Type: compatir.BlockToolCall,
						ToolCall: &compatir.ToolCall{
							ID:        "call_1",
							Type:      compatir.ToolTypeFunction,
							Name:      "sum",
							Arguments: `{"a":1}`,
						},
					},
				},
			},
		},
	}

	out := ChatOutbound{
		Reasoning: policy.OpenAIChatReasoningPolicy("https://api.openai.com/v1", req.Model),
	}.BuildRequest(req)
	msgs := out["messages"].([]map[string]any)
	if _, ok := msgs[0]["reasoning_content"]; ok {
		t.Fatalf("did not expect reasoning_content, got %#v", msgs[0])
	}
}

func TestChatResponseMapsReasoningToolCallsAndUsage(t *testing.T) {
	resp := ChatResponse(map[string]any{
		"id":    "chatcmpl_1",
		"model": "mimo-v2.5-pro",
		"choices": []any{
			map[string]any{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"reasoning_content": "think first",
					"content":           "calling",
					"tool_calls": []any{
						map[string]any{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      "sum",
								"arguments": `{"a":1}`,
							},
						},
					},
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(4),
			"total_tokens":      float64(14),
		},
	})

	if resp.StopReason != compatir.StopReasonToolUse {
		t.Fatalf("stop reason mismatch: %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 4 || resp.Usage.TotalTokens != 14 {
		t.Fatalf("usage mismatch: %#v", resp.Usage)
	}
	if len(resp.Output) != 3 {
		t.Fatalf("output blocks mismatch: %#v", resp.Output)
	}
	if resp.Output[0].Reasoning == nil || resp.Output[0].Reasoning.Text != "think first" {
		t.Fatalf("reasoning block mismatch: %#v", resp.Output[0])
	}
	if resp.Output[2].ToolCall == nil || resp.Output[2].ToolCall.ID != "call_1" {
		t.Fatalf("tool call block mismatch: %#v", resp.Output[2])
	}
}

func TestChatResponseNormalizesStructuredContent(t *testing.T) {
	resp := ChatResponse(map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "first"},
						map[string]any{"type": "output_text", "text": "second"},
					},
				},
			},
		},
	})

	if len(resp.Output) != 1 || resp.Output[0].Text != "first\nsecond" {
		t.Fatalf("structured content mismatch: %#v", resp.Output)
	}
}

func TestChatStreamEventsMapReasoningToolCallsAndUsage(t *testing.T) {
	events := ChatStreamEvents(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"reasoning_content": "think ",
					"tool_calls": []any{
						map[string]any{
							"index": float64(0),
							"id":    "call_1",
							"type":  "function",
							"function": map[string]any{
								"name":      "sum",
								"arguments": `{"a":`,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     float64(7),
			"completion_tokens": float64(2),
		},
	})

	if len(events) != 4 {
		t.Fatalf("events mismatch: %#v", events)
	}
	if events[0].Type != compatir.StreamEventUsage || events[0].Usage.InputTokens != 7 {
		t.Fatalf("usage event mismatch: %#v", events[0])
	}
	if events[1].Delta.Reasoning == nil || events[1].Delta.Reasoning.Text != "think " {
		t.Fatalf("reasoning event mismatch: %#v", events[1])
	}
	if events[2].Delta.ToolCall == nil || events[2].Delta.ToolCall.ID != "call_1" {
		t.Fatalf("tool call event mismatch: %#v", events[2])
	}
	if events[3].StopReason != compatir.StopReasonToolUse {
		t.Fatalf("stop event mismatch: %#v", events[3])
	}
}
