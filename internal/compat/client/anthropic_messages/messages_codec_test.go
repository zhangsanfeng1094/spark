package anthropic_messages

import (
	"testing"

	"spark/internal/compat/ir"
)

func TestMessagesInboundMapsThinkingToReasoning(t *testing.T) {
	req := MessagesInbound(map[string]any{
		"model": "mimo-v2.5-pro",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "thinking", "thinking": "think first"},
					map[string]any{"type": "tool_use", "id": "call_1", "name": "sum", "input": map[string]any{"a": float64(1)}},
				},
			},
		},
	})

	if len(req.Messages) != 1 {
		t.Fatalf("messages mismatch: %#v", req.Messages)
	}
	msg := req.Messages[0]
	if msg.ReasoningText() != "think first" {
		t.Fatalf("reasoning mismatch: %#v", msg)
	}
	if ids := msg.ToolCallIDs(); len(ids) != 1 || ids[0] != "call_1" {
		t.Fatalf("tool call ids mismatch: %#v", ids)
	}
}

func TestMessagesInboundMapsToolsAndToolChoice(t *testing.T) {
	req := MessagesInbound(map[string]any{
		"tools": []any{
			map[string]any{
				"name":         "sum",
				"description":  "add numbers",
				"input_schema": map[string]any{"type": "object"},
			},
		},
		"tool_choice": map[string]any{
			"type": "tool",
			"name": "sum",
		},
	})

	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "sum" {
		t.Fatalf("tools mismatch: %#v", req.Tools)
	}
	if req.ToolChoice.Mode != ir.ToolChoiceFunction || req.ToolChoice.Name != "sum" {
		t.Fatalf("tool choice mismatch: %#v", req.ToolChoice)
	}
}

func TestMessagesInboundPreservesThinkingRequestConfig(t *testing.T) {
	thinking := map[string]any{
		"type":          "enabled",
		"budget_tokens": float64(1024),
	}
	req := MessagesInbound(map[string]any{
		"model":      "mimo-v2.5-pro",
		"max_tokens": float64(2048),
		"thinking":   thinking,
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "hello",
			},
		},
	})

	if req.Generation.Reasoning.Enabled == nil || !*req.Generation.Reasoning.Enabled {
		t.Fatalf("thinking enabled mismatch: %#v", req.Generation.Reasoning)
	}
	if req.Generation.Reasoning.BudgetTokens == nil || *req.Generation.Reasoning.BudgetTokens != 1024 {
		t.Fatalf("thinking budget mismatch: %#v", req.Generation.Reasoning)
	}
	if req.Generation.Reasoning.Raw["thinking"] == nil {
		t.Fatalf("thinking raw config missing: %#v", req.Generation.Reasoning.Raw)
	}
}

func TestMessagesInboundMapsOutputConfigEffortToReasoningEffort(t *testing.T) {
	req := MessagesInbound(map[string]any{
		"model": "mimo-v2.5-pro",
		"output_config": map[string]any{
			"effort": "high",
		},
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "hello",
			},
		},
	})

	if req.Generation.Reasoning.Effort != ir.ReasoningEffortHigh {
		t.Fatalf("reasoning effort mismatch: %#v", req.Generation.Reasoning)
	}
	if _, ok := req.Generation.Reasoning.Raw["output_config"]; ok {
		t.Fatalf("output_config should not be in reasoning raw: %#v", req.Generation.Reasoning.Raw)
	}
}

func TestMessagesClientResponseMapsToolUseAndUsage(t *testing.T) {
	msg := MessagesClientResponse(ir.Response{
		ID:    "chatcmpl_1",
		Model: "gpt-4.1",
		Output: []ir.ContentBlock{
			ir.Text("calling tool"),
			{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					ID:        "call_1",
					Type:      ir.ToolTypeFunction,
					Name:      "sum",
					Arguments: `{"a":1,"b":2}`,
				},
			},
		},
		StopReason: ir.StopReasonToolUse,
		Usage: ir.Usage{
			InputTokens:              12,
			OutputTokens:             6,
			CacheCreationInputTokens: 3,
			CacheReadInputTokens:     4,
		},
	}, "")

	if msg["stop_reason"] != "tool_use" {
		t.Fatalf("stop reason mismatch: %#v", msg["stop_reason"])
	}
	content := msg["content"].([]map[string]any)
	if len(content) != 2 || content[0]["type"] != "text" || content[1]["type"] != "tool_use" {
		t.Fatalf("content mismatch: %#v", content)
	}
	usage := msg["usage"].(map[string]any)
	if usage["input_tokens"] != 12 || usage["output_tokens"] != 6 {
		t.Fatalf("usage mismatch: %#v", usage)
	}
	if usage["cache_creation_input_tokens"] != 3 || usage["cache_read_input_tokens"] != 4 {
		t.Fatalf("cache usage mismatch: %#v", usage)
	}
}

func TestMessagesClientResponseMapsReasoningToThinkingBlock(t *testing.T) {
	msg := MessagesClientResponse(ir.Response{
		ID:    "chatcmpl_1",
		Model: "mimo-v2.5-pro",
		Output: []ir.ContentBlock{
			ir.Reasoning("think first"),
			ir.Text("final answer"),
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
		StopReason: ir.StopReasonToolUse,
	}, "")

	content := msg["content"].([]map[string]any)
	if len(content) != 3 {
		t.Fatalf("content length mismatch: %#v", content)
	}
	if content[0]["type"] != "thinking" || content[0]["thinking"] != "think first" {
		t.Fatalf("thinking block mismatch: %#v", content[0])
	}
	if _, ok := content[0]["reasoning_content"]; ok {
		t.Fatalf("thinking block leaked OpenAI field: %#v", content[0])
	}
	if content[1]["type"] != "text" || content[2]["type"] != "tool_use" {
		t.Fatalf("content order mismatch: %#v", content)
	}
}
