package gemini_generate_content

import (
	"fmt"
	"time"

	"spark/internal/compat/ir"
)

func GenerateContentClientResponse(resp ir.Response) map[string]any {
	parts := geminiResponseParts(resp.Output)
	candidate := map[string]any{
		"content": map[string]any{
			"role":  "model",
			"parts": parts,
		},
		"finishReason": geminiFinishReason(resp.StopReason),
		"index":        0,
	}
	out := map[string]any{
		"candidates": []map[string]any{candidate},
	}
	if usage, ok := geminiUsageMetadata(resp.Usage); ok {
		out["usageMetadata"] = usage
	}
	if resp.ID != "" {
		out["responseId"] = resp.ID
	}
	if resp.Model != "" {
		out["modelVersion"] = resp.Model
	}
	return out
}

func geminiResponseParts(blocks []ir.ContentBlock) []map[string]any {
	parts := make([]map[string]any, 0, len(blocks))
	for i, block := range blocks {
		switch block.Type {
		case ir.BlockText:
			if block.Text != "" {
				parts = append(parts, map[string]any{"text": block.Text})
			}
		case ir.BlockReasoning:
			if block.Reasoning != nil && block.Reasoning.Signature != "" {
				parts = append(parts, map[string]any{
					"thought":          true,
					"thoughtSignature": block.Reasoning.Signature,
				})
			}
		case ir.BlockToolCall:
			if block.ToolCall == nil || block.ToolCall.Name == "" {
				continue
			}
			id := block.ToolCall.ID
			if id == "" {
				id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
			}
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"id":   id,
					"name": block.ToolCall.Name,
					"args": objectFromJSONString(block.ToolCall.Arguments),
				},
			})
		}
	}
	if len(parts) == 0 {
		parts = append(parts, map[string]any{"text": ""})
	}
	return parts
}

func geminiUsageMetadata(usage ir.Usage) (map[string]any, bool) {
	raw := usage.Raw
	input := usage.InputTokens
	if input == 0 {
		input = intFromAny(raw["promptTokenCount"])
	}
	if input == 0 {
		input = intFromAny(raw["input_tokens"])
	}
	output := usage.OutputTokens
	if output == 0 {
		output = intFromAny(raw["candidatesTokenCount"])
	}
	if output == 0 {
		output = intFromAny(raw["output_tokens"])
	}
	total := usage.TotalTokens
	if total == 0 {
		total = intFromAny(raw["totalTokenCount"])
	}
	if total == 0 && (input > 0 || output > 0) {
		total = input + output
	}
	if input == 0 && output == 0 && total == 0 {
		return nil, false
	}
	return map[string]any{
		"promptTokenCount":     input,
		"candidatesTokenCount": output,
		"totalTokenCount":      total,
	}, true
}

func geminiFinishReason(reason ir.StopReason) string {
	switch reason {
	case ir.StopReasonMaxTokens:
		return "MAX_TOKENS"
	case ir.StopReasonContentFilter:
		return "SAFETY"
	case ir.StopReasonError:
		return "OTHER"
	default:
		return "STOP"
	}
}

func intFromAny(v any) int {
	if i, ok := intValue(v); ok {
		return i
	}
	return 0
}
