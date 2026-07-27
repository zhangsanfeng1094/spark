package openai_chat

import (
	"testing"

	"spark/internal/compat/ir"
	"spark/internal/compat/policy"
)

func TestChatOutboundRequiresEmptyReasoningForMimoToolCalls(t *testing.T) {
	req := ir.Request{
		Model: "mimo-v2.5-pro",
		Messages: []ir.Message{
			{
				Role: ir.RoleAssistant,
				Content: []ir.ContentBlock{
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
	req := ir.Request{
		Model: "gpt-4.1",
		Messages: []ir.Message{
			{
				Role: ir.RoleAssistant,
				Content: []ir.ContentBlock{
					ir.Reasoning("think first"),
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

func TestChatOutboundWritesReasoningRequestControls(t *testing.T) {
	enabled := true
	budget := 1024
	req := ir.Request{
		Model: "mimo-v2.5-pro",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text("hello")}},
		},
		Generation: ir.GenerationConfig{
			Reasoning: ir.ReasoningConfig{
				Enabled:      &enabled,
				Effort:       ir.ReasoningEffortHigh,
				BudgetTokens: &budget,
			},
		},
	}

	out := ChatOutbound{
		Reasoning: policy.PreserveReasoningContent(),
	}.BuildRequest(req)
	if out["reasoning_effort"] != "high" {
		t.Fatalf("reasoning effort mismatch: %#v", out)
	}
	thinking, ok := out["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" || thinking["budget_tokens"] != 1024 {
		t.Fatalf("thinking config mismatch: %#v", out["thinking"])
	}
}

func TestChatOutboundDropsUnsupportedReasoningControls(t *testing.T) {
	includeThoughts := true
	req := ir.Request{
		Model: "gpt-4.1",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text("hello")}},
		},
		Generation: ir.GenerationConfig{
			Reasoning: ir.ReasoningConfig{
				IncludeThoughts: &includeThoughts,
			},
		},
	}

	out := ChatOutbound{
		Reasoning: policy.OpenAIChatReasoningPolicy("https://api.openai.com/v1", req.Model),
	}.BuildRequest(req)
	if _, ok := out["includeThoughts"]; ok {
		t.Fatalf("did not expect Gemini includeThoughts in OpenAI Chat request: %#v", out)
	}
	if _, ok := out["thinking"]; ok {
		t.Fatalf("did not expect thinking object for unsupported include thoughts: %#v", out)
	}
	if _, ok := out["reasoning_effort"]; ok {
		t.Fatalf("did not expect reasoning_effort for unsupported include thoughts: %#v", out)
	}
}

func TestChatOutboundIncludesUsageForStreams(t *testing.T) {
	req := ir.Request{
		Model:  "gpt-4.1",
		Stream: true,
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text("hello")}},
		},
		Generation: ir.GenerationConfig{
			Raw: map[string]any{
				"stream_options": map[string]any{"include_usage": false},
			},
		},
	}

	out := ChatOutbound{Reasoning: policy.PreserveReasoningContent()}.BuildRequest(req)
	streamOptions, ok := out["stream_options"].(map[string]any)
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream options mismatch: %#v", out["stream_options"])
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

	if resp.StopReason != ir.StopReasonToolUse {
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
	if events[0].Type != ir.StreamEventUsage || events[0].Usage.InputTokens != 7 {
		t.Fatalf("usage event mismatch: %#v", events[0])
	}
	if events[1].Delta.Reasoning == nil || events[1].Delta.Reasoning.Text != "think " {
		t.Fatalf("reasoning event mismatch: %#v", events[1])
	}
	if events[2].Delta.ToolCall == nil || events[2].Delta.ToolCall.ID != "call_1" {
		t.Fatalf("tool call event mismatch: %#v", events[2])
	}
	if events[3].StopReason != ir.StopReasonToolUse {
		t.Fatalf("stop event mismatch: %#v", events[3])
	}
}

func TestChatResponseReadsAlternateReasoningFields(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  any
		want string
	}{
		{"reasoning_text string", "reasoning_text", "copilot think", "copilot think"},
		{"thinking string", "thinking", "anthropic think", "anthropic think"},
		{"thought string", "thought", "qwen3 think", "qwen3 think"},
		{"reasoning_details array", "reasoning_details", []any{
			map[string]any{"type": "summary_text", "text": "summary think"},
		}, "summary think"},
		{"reasoning_details multiple items", "reasoning_details", []any{
			map[string]any{"type": "summary_text", "text": "first"},
			map[string]any{"type": "summary_text", "text": "second"},
		}, "first\nsecond"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := ChatResponse(map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{tc.key: tc.val},
					},
				},
			})
			if len(resp.Output) != 1 {
				t.Fatalf("expected 1 output block, got %#v", resp.Output)
			}
			if resp.Output[0].Type != ir.BlockReasoning {
				t.Fatalf("expected BlockReasoning, got %q", resp.Output[0].Type)
			}
			if resp.Output[0].Reasoning == nil || resp.Output[0].Reasoning.Text != tc.want {
				t.Fatalf("reasoning text mismatch: %#v", resp.Output[0].Reasoning)
			}
		})
	}
}

func TestChatStreamEventsReadsAlternateReasoningFields(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  any
		want string
	}{
		{"reasoning_text string", "reasoning_text", "copilot think", "copilot think"},
		{"thinking string", "thinking", "anthropic think", "anthropic think"},
		{"thought string", "thought", "qwen3 think", "qwen3 think"},
		{"reasoning_details array", "reasoning_details", []any{
			map[string]any{"type": "summary_text", "text": "summary think"},
		}, "summary think"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := ChatStreamEvents(map[string]any{
				"choices": []any{
					map[string]any{
						"delta": map[string]any{tc.key: tc.val},
					},
				},
			})
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %#v", events)
			}
			if events[0].Delta.Type != ir.BlockReasoning {
				t.Fatalf("expected BlockReasoning delta, got %q", events[0].Delta.Type)
			}
			if events[0].Delta.Reasoning == nil || events[0].Delta.Reasoning.Text != tc.want {
				t.Fatalf("reasoning text mismatch: %#v", events[0].Delta.Reasoning)
			}
		})
	}
}

func TestChatResponseReasoningFieldPrecedence(t *testing.T) {
	cases := []struct {
		name    string
		message map[string]any
		want    string
	}{
		{
			"reasoning_content beats thinking",
			map[string]any{"reasoning_content": "first", "thinking": "second"},
			"first",
		},
		{
			"thinking beats thought",
			map[string]any{"thinking": "first", "thought": "second"},
			"first",
		},
		{
			"reasoning beats reasoning_text",
			map[string]any{"reasoning": "first", "reasoning_text": "second"},
			"first",
		},
		{
			"reasoning_text beats thinking",
			map[string]any{"reasoning_text": "first", "thinking": "second"},
			"first",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := ChatResponse(map[string]any{
				"choices": []any{
					map[string]any{"message": tc.message},
				},
			})
			if len(resp.Output) != 1 || resp.Output[0].Reasoning == nil {
				t.Fatalf("expected one reasoning block, got %#v", resp.Output)
			}
			if resp.Output[0].Reasoning.Text != tc.want {
				t.Fatalf("precedence mismatch: want %q, got %q", tc.want, resp.Output[0].Reasoning.Text)
			}
		})
	}
}

func TestExtractReasoningTextFromMapBroadened(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]any
		want string
		ok   bool
	}{
		{"reasoning_content", map[string]any{"reasoning_content": "a"}, "a", true},
		{"reasoning", map[string]any{"reasoning": "b"}, "b", true},
		{"reasoning_text", map[string]any{"reasoning_text": "c"}, "c", true},
		{"thinking", map[string]any{"thinking": "d"}, "d", true},
		{"thought", map[string]any{"thought": "e"}, "e", true},
		{"reasoning_details array", map[string]any{"reasoning_details": []any{
			map[string]any{"type": "summary_text", "text": "f"},
		}}, "f", true},
		{"empty map", map[string]any{}, "", false},
		{"nil value falls through", map[string]any{"reasoning_content": nil, "thinking": "g"}, "g", true},
		{"empty string falls through", map[string]any{"reasoning_content": "", "thought": "h"}, "h", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractReasoningTextFromMap(tc.m)
			if ok != tc.ok {
				t.Fatalf("ok mismatch: want %v, got %v", tc.ok, ok)
			}
			if got != tc.want {
				t.Fatalf("text mismatch: want %q, got %q", tc.want, got)
			}
		})
	}
}
