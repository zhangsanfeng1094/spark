package anthropic_messages

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"spark/internal/compat/ir"
)

func MessagesClientResponse(resp ir.Response, requestedModel string) map[string]any {
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
		"usage":         anthropicUsageMap(resp.Usage),
	}
}

func anthropicUsageMap(usage ir.Usage) map[string]any {
	out := map[string]any{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
	}
	if usage.CacheCreationInputTokens > 0 {
		out["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
	}
	if usage.CacheReadInputTokens > 0 {
		out["cache_read_input_tokens"] = usage.CacheReadInputTokens
	}
	return out
}

func responseText(blocks []ir.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == ir.BlockText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func responseContentBlocks(blocks []ir.ContentBlock) []map[string]any {
	content := make([]map[string]any, 0, len(blocks))
	toolIndex := 0
	for _, block := range blocks {
		switch block.Type {
		case ir.BlockReasoning:
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
		case ir.BlockText:
			if block.Text == "" {
				continue
			}
			content = append(content, map[string]any{
				"type": "text",
				"text": block.Text,
			})
		case ir.BlockToolCall:
			if block.ToolCall == nil || block.ToolCall.Name == "" {
				continue
			}
			content = append(content, anthropicToolUseBlock(*block.ToolCall, toolIndex))
			toolIndex++
		}
	}
	return content
}

func anthropicToolUseBlock(tc ir.ToolCall, index int) map[string]any {
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

func responseToolCalls(blocks []ir.ContentBlock) []ir.ToolCall {
	out := make([]ir.ToolCall, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != ir.BlockToolCall || block.ToolCall == nil || block.ToolCall.Name == "" {
			continue
		}
		out = append(out, *block.ToolCall)
	}
	return out
}

func anthropicStopReason(reason ir.StopReason, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_use"
	}
	switch reason {
	case ir.StopReasonMaxTokens:
		return "max_tokens"
	case ir.StopReasonToolUse:
		return "tool_use"
	default:
		return "end_turn"
	}
}
