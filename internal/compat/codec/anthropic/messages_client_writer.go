package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"spark/internal/compatir"
)

func MessagesClientResponse(resp compatir.Response, requestedModel string) map[string]any {
	id := resp.ID
	if id == "" {
		id = fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	model := resp.Model
	if model == "" {
		model = requestedModel
	}

	toolCalls := responseToolCalls(resp.Output)
	content := make([]map[string]any, 0, 1+len(toolCalls))
	if text := responseText(resp.Output); text != "" {
		content = append(content, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	for i, tc := range toolCalls {
		input := map[string]any{}
		if strings.TrimSpace(tc.Arguments) != "" {
			_ = json.Unmarshal([]byte(tc.Arguments), &input)
		}
		toolID := tc.ID
		if toolID == "" {
			toolID = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), i)
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    toolID,
			"name":  tc.Name,
			"input": input,
		})
	}

	return map[string]any{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   anthropicStopReason(resp.StopReason, len(toolCalls) > 0),
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}
}

func responseText(blocks []compatir.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == compatir.BlockText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func responseToolCalls(blocks []compatir.ContentBlock) []compatir.ToolCall {
	out := make([]compatir.ToolCall, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != compatir.BlockToolCall || block.ToolCall == nil || block.ToolCall.Name == "" {
			continue
		}
		out = append(out, *block.ToolCall)
	}
	return out
}

func anthropicStopReason(reason compatir.StopReason, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_use"
	}
	switch reason {
	case compatir.StopReasonMaxTokens:
		return "max_tokens"
	case compatir.StopReasonToolUse:
		return "tool_use"
	default:
		return "end_turn"
	}
}
