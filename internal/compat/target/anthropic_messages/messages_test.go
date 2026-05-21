package anthropic_messages

import (
	"testing"

	"spark/internal/compat/ir"
	"spark/internal/usage"
)

func TestMessagesOutboundBuildsAnthropicRequest(t *testing.T) {
	maxTokens := 512
	req := ir.Request{
		Model: "claude-sonnet-4",
		Messages: []ir.Message{
			{Role: ir.RoleSystem, Content: []ir.ContentBlock{ir.Text("be concise")}},
			{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text("hello")}},
			{
				Role: ir.RoleAssistant,
				Content: []ir.ContentBlock{
					{
						Type: ir.BlockToolCall,
						ToolCall: &ir.ToolCall{
							ID:        "toolu_1",
							Type:      ir.ToolTypeFunction,
							Name:      "sum",
							Arguments: `{"a":1}`,
						},
					},
				},
			},
			{Role: ir.RoleTool, Content: []ir.ContentBlock{{
				Type:       ir.BlockToolResult,
				ToolResult: &ir.ToolResult{ToolCallID: "toolu_1", Output: `{"result":1}`},
			}}},
		},
		Tools: []ir.Tool{{
			Type: ir.ToolTypeFunction,
			Function: ir.FunctionTool{
				Name:       "sum",
				Parameters: map[string]any{"type": "object"},
			},
		}},
		ToolChoice: ir.ToolChoice{Mode: ir.ToolChoiceFunction, Name: "sum"},
		Generation: ir.GenerationConfig{
			MaxTokens: &maxTokens,
			Reasoning: ir.ReasoningConfig{
				Effort: ir.ReasoningEffortLow,
			},
		},
	}

	out := MessagesOutbound{}.BuildRequest(req)
	if out["model"] != "claude-sonnet-4" || out["system"] != "be concise" || out["max_tokens"] != 512 {
		t.Fatalf("basic request fields mismatch: %#v", out)
	}
	msgs := out["messages"].([]map[string]any)
	if len(msgs) != 3 {
		t.Fatalf("messages mismatch: %#v", msgs)
	}
	if msgs[0]["role"] != "user" {
		t.Fatalf("first message should be user: %#v", msgs[0])
	}
	assistantContent := msgs[1]["content"].([]map[string]any)
	if assistantContent[0]["type"] != "tool_use" || assistantContent[0]["name"] != "sum" {
		t.Fatalf("tool_use mismatch: %#v", assistantContent[0])
	}
	toolChoice := out["tool_choice"].(map[string]any)
	if toolChoice["type"] != "tool" || toolChoice["name"] != "sum" {
		t.Fatalf("tool choice mismatch: %#v", toolChoice)
	}
	if thinking := out["thinking"].(map[string]any); thinking["type"] != "enabled" {
		t.Fatalf("thinking mismatch: %#v", thinking)
	}
}

func TestMessageResponseMapsContentToolUseAndUsage(t *testing.T) {
	resp := MessageResponse(map[string]any{
		"id":          "msg_1",
		"model":       "claude-sonnet-4",
		"stop_reason": "tool_use",
		"usage": map[string]any{
			"input_tokens":  10,
			"output_tokens": 3,
		},
		"content": []any{
			map[string]any{"type": "thinking", "thinking": "think"},
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": "sum", "input": map[string]any{"a": float64(1)}},
		},
	})

	if resp.ID != "msg_1" || resp.StopReason != ir.StopReasonToolUse {
		t.Fatalf("response metadata mismatch: %#v", resp)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 3 || resp.Usage.TotalTokens != 13 {
		t.Fatalf("usage mismatch: %#v", resp.Usage)
	}
	if len(resp.Output) != 3 || resp.Output[0].Type != ir.BlockReasoning || resp.Output[1].Text != "hello" {
		t.Fatalf("content mismatch: %#v", resp.Output)
	}
	if got := resp.Output[2].ToolCall; got == nil || got.Name != "sum" || got.Arguments != `{"a":1}` {
		t.Fatalf("tool call mismatch: %#v", resp.Output[2])
	}
}

func TestMessageResponseIncludesCacheTokensInUsage(t *testing.T) {
	resp := MessageResponse(map[string]any{
		"model": "claude-test",
		"usage": map[string]any{
			"input_tokens":                float64(21),
			"cache_creation_input_tokens": float64(1000),
			"cache_read_input_tokens":     float64(200),
			"output_tokens":               float64(393),
		},
	})

	if resp.Usage.InputTokens != 1221 || resp.Usage.OutputTokens != 393 || resp.Usage.TotalTokens != 1614 {
		t.Fatalf("usage mismatch: %#v", resp.Usage)
	}
	if resp.Usage.CacheCreationInputTokens != 1000 || resp.Usage.CacheReadInputTokens != 200 {
		t.Fatalf("cache usage mismatch: %#v", resp.Usage)
	}
}

func TestMessageStreamEventsMapDeltas(t *testing.T) {
	events := MessageStreamEvents(map[string]any{
		"type":  "content_block_start",
		"index": float64(1),
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    "toolu_1",
			"name":  "exec_command",
			"input": map[string]any{},
		},
	})
	if len(events) != 1 || events[0].Delta.ToolCall == nil || events[0].Delta.ToolCall.Arguments != "" {
		t.Fatalf("tool start should not treat empty input as arguments: %#v", events)
	}

	events = MessageStreamEvents(map[string]any{
		"type":  "content_block_delta",
		"index": float64(1),
		"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"a":`},
	})
	if len(events) != 1 || events[0].Delta.ToolCall == nil || events[0].Delta.ToolCall.Arguments != `{"a":` {
		t.Fatalf("tool delta mismatch: %#v", events)
	}

	events = MessageStreamEvents(map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn"},
		"usage": map[string]any{"input_tokens": float64(2), "output_tokens": float64(4)},
	})
	if len(events) != 2 || events[0].Usage == nil || events[1].StopReason != ir.StopReasonEndTurn {
		t.Fatalf("message delta mismatch: %#v", events)
	}
}

func TestMessageStreamEventsRecordsCachedUsage(t *testing.T) {
	var records []usage.Record
	usage.SetRecorder(func(record usage.Record) error {
		records = append(records, record)
		return nil
	})
	defer usage.SetRecorder(nil)

	events := MessageStreamEvents(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"model": "claude-test",
			"usage": map[string]any{
				"input_tokens":                float64(21),
				"cache_creation_input_tokens": float64(1000),
				"cache_read_input_tokens":     float64(200),
				"output_tokens":               float64(3),
			},
		},
	})

	if len(events) != 1 || events[0].Usage == nil {
		t.Fatalf("expected usage event, got %#v", events)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	if records[0].InputTokens != 1221 || records[0].OutputTokens != 3 || records[0].TotalTokens != 1224 || records[0].CachedInputTokens != 200 {
		t.Fatalf("unexpected usage record: %#v", records[0])
	}
}
