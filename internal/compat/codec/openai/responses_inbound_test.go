package openai

import (
	"testing"

	"spark/internal/compatir"
)

func TestResponsesInboundStringInput(t *testing.T) {
	maxTokens := float64(32)
	req := ResponsesInbound(map[string]any{
		"model":             "glm-5:cloud",
		"input":             "hello",
		"stream":            true,
		"max_output_tokens": maxTokens,
	})

	if req.Model != "glm-5:cloud" || !req.Stream {
		t.Fatalf("request basics mismatch: %#v", req)
	}
	if req.Generation.MaxTokens == nil || *req.Generation.MaxTokens != 32 {
		t.Fatalf("max tokens mismatch: %#v", req.Generation.MaxTokens)
	}
	if streamOptions, ok := req.Generation.Raw["stream_options"].(map[string]any); !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream options mismatch: %#v", req.Generation.Raw)
	}
	if len(req.Messages) != 1 || req.Messages[0].Text() != "hello" {
		t.Fatalf("messages mismatch: %#v", req.Messages)
	}
}

func TestResponsesInboundReasoningAndFunctionCallOutput(t *testing.T) {
	req := ResponsesInbound(map[string]any{
		"model": "mimo-v2.5-pro",
		"input": []any{
			map[string]any{
				"type": "reasoning",
				"summary": []any{
					map[string]any{"type": "summary_text", "text": "think first"},
				},
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_123",
				"name":      "sum",
				"arguments": `{"a":1,"b":2}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_123",
				"output":  `{"result":3}`,
			},
		},
	})

	if len(req.Messages) != 2 {
		t.Fatalf("messages mismatch: %#v", req.Messages)
	}
	assistant := req.Messages[0]
	if assistant.Role != compatir.RoleAssistant || assistant.ReasoningText() != "think first" {
		t.Fatalf("assistant reasoning mismatch: %#v", assistant)
	}
	if ids := assistant.ToolCallIDs(); len(ids) != 1 || ids[0] != "call_123" {
		t.Fatalf("tool call ids mismatch: %#v", ids)
	}
	tool := req.Messages[1]
	if tool.Role != compatir.RoleTool || len(tool.Content) != 1 || tool.Content[0].ToolResult.ToolCallID != "call_123" {
		t.Fatalf("tool result mismatch: %#v", tool)
	}
}

func TestResponsesInboundToolsMappedAndFiltered(t *testing.T) {
	req := ResponsesInbound(map[string]any{
		"tools": []any{
			map[string]any{
				"type":        "function",
				"name":        "sum",
				"description": "add numbers",
				"parameters":  map[string]any{"type": "object"},
			},
			map[string]any{
				"type": "web_search_preview",
			},
		},
		"tool_choice": map[string]any{
			"type": "function",
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
