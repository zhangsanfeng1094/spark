package codex

import (
	"testing"

	"spark/internal/compat/ir"
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

func TestResponsesInboundPrependsInstructionsAsSystemMessage(t *testing.T) {
	req := ResponsesInbound(map[string]any{
		"model":        "deepseek-v4-flash",
		"instructions": "stable codex instructions",
		"input":        "hello",
	})

	if len(req.Messages) != 2 {
		t.Fatalf("messages mismatch: %#v", req.Messages)
	}
	if req.Messages[0].Role != ir.RoleSystem || req.Messages[0].Text() != "stable codex instructions" {
		t.Fatalf("system message mismatch: %#v", req.Messages[0])
	}
	if req.Messages[1].Role != ir.RoleUser || req.Messages[1].Text() != "hello" {
		t.Fatalf("user message mismatch: %#v", req.Messages[1])
	}
}

func TestResponsesInboundPreservesPromptCacheKeyInMetadata(t *testing.T) {
	req := ResponsesInbound(map[string]any{
		"model":            "deepseek-v4-flash",
		"input":            "hello",
		"prompt_cache_key": "cache-key-123",
	})

	if req.Metadata["prompt_cache_key"] != "cache-key-123" {
		t.Fatalf("prompt cache key metadata mismatch: %#v", req.Metadata)
	}
}

func TestResponsesInboundDropsEmptyInputTextBlocks(t *testing.T) {
	req := ResponsesInbound(map[string]any{
		"model": "deepseek-v4-flash",
		"input": []any{
			map[string]any{
				"role": "system",
				"content": []any{
					map[string]any{"type": "input_text", "text": ""},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "hello"},
				},
			},
		},
	})

	if len(req.Messages) != 1 {
		t.Fatalf("messages mismatch: %#v", req.Messages)
	}
	if req.Messages[0].Role != ir.RoleUser || req.Messages[0].Text() != "hello" {
		t.Fatalf("user message mismatch: %#v", req.Messages[0])
	}
}

func TestResponsesInboundMapsReasoningRequestConfig(t *testing.T) {
	req := ResponsesInbound(map[string]any{
		"model": "gpt-5.1",
		"input": "hello",
		"reasoning": map[string]any{
			"effort":  "high",
			"summary": "auto",
		},
	})

	if req.Generation.Reasoning.Effort != ir.ReasoningEffortHigh {
		t.Fatalf("reasoning effort mismatch: %#v", req.Generation.Reasoning)
	}
	if req.Generation.Reasoning.Summary != ir.ReasoningSummary("auto") {
		t.Fatalf("reasoning summary mismatch: %#v", req.Generation.Reasoning)
	}
	if req.Generation.Reasoning.Raw["reasoning"] == nil {
		t.Fatalf("reasoning raw config missing: %#v", req.Generation.Reasoning.Raw)
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
	if assistant.Role != ir.RoleAssistant || assistant.ReasoningText() != "think first" {
		t.Fatalf("assistant reasoning mismatch: %#v", assistant)
	}
	if ids := assistant.ToolCallIDs(); len(ids) != 1 || ids[0] != "call_123" {
		t.Fatalf("tool call ids mismatch: %#v", ids)
	}
	tool := req.Messages[1]
	if tool.Role != ir.RoleTool || len(tool.Content) != 1 || tool.Content[0].ToolResult.ToolCallID != "call_123" {
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
	if req.ToolChoice.Mode != ir.ToolChoiceFunction || req.ToolChoice.Name != "sum" {
		t.Fatalf("tool choice mismatch: %#v", req.ToolChoice)
	}
}
