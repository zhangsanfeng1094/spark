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
	content := responseContentBlocks(resp.Output)

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

func responseContentBlocks(blocks []compatir.ContentBlock) []map[string]any {
	content := make([]map[string]any, 0, len(blocks))
	toolIndex := 0
	for _, block := range blocks {
		switch block.Type {
		case compatir.BlockReasoning:
			if block.Reasoning == nil || block.Reasoning.Text == "" {
				continue
			}
			thinking := map[string]any{
				"type":     "thinking",
				"thinking": block.Reasoning.Text,
			}
			if block.Reasoning.Signature != "" {
				thinking["signature"] = block.Reasoning.Signature
			}
			content = append(content, thinking)
		case compatir.BlockText:
			if block.Text == "" {
				continue
			}
			content = append(content, map[string]any{
				"type": "text",
				"text": block.Text,
			})
		case compatir.BlockToolCall:
			if block.ToolCall == nil || block.ToolCall.Name == "" {
				continue
			}
			content = append(content, anthropicToolUseBlock(*block.ToolCall, toolIndex))
			toolIndex++
		}
	}
	return content
}

func anthropicToolUseBlock(tc compatir.ToolCall, index int) map[string]any {
	input := map[string]any{}
	if strings.TrimSpace(tc.Arguments) != "" {
		_ = json.Unmarshal([]byte(tc.Arguments), &input)
	}
	toolID := tc.ID
	if toolID == "" {
		toolID = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), index)
	}
	return map[string]any{
		"type":  "tool_use",
		"id":    toolID,
		"name":  tc.Name,
		"input": input,
	}
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
