package anthropic

import (
	"testing"

	"spark/internal/compatir"
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
	if req.ToolChoice.Mode != compatir.ToolChoiceFunction || req.ToolChoice.Name != "sum" {
		t.Fatalf("tool choice mismatch: %#v", req.ToolChoice)
	}
}

func TestMessagesClientResponseMapsToolUseAndUsage(t *testing.T) {
	msg := MessagesClientResponse(compatir.Response{
		ID:    "chatcmpl_1",
		Model: "gpt-4.1",
		Output: []compatir.ContentBlock{
			compatir.Text("calling tool"),
			{
				Type: compatir.BlockToolCall,
				ToolCall: &compatir.ToolCall{
					ID:        "call_1",
					Type:      compatir.ToolTypeFunction,
					Name:      "sum",
					Arguments: `{"a":1,"b":2}`,
				},
			},
		},
		StopReason: compatir.StopReasonToolUse,
		Usage: compatir.Usage{
			InputTokens:  12,
			OutputTokens: 6,
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
}
