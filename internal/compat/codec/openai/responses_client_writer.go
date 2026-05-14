package openai

import (
	"fmt"
	"strings"
	"time"

	"spark/internal/compatir"
)

func ResponsesClientResponse(resp compatir.Response) map[string]any {
	id := resp.ID
	if id == "" {
		id = fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	model := resp.Model
	if model == "" {
		model = "unknown"
	}

	reasoningText := responseReasoningOutputText(resp.Output)
	text := responseOutputText(resp.Output)
	outputItems := make([]map[string]any, 0, 3)
	if reasoningText != "" {
		outputItems = append(outputItems, map[string]any{
			"id":      "rs_" + id,
			"type":    "reasoning",
			"summary": []map[string]any{{"type": "summary_text", "text": reasoningText}},
		})
	}
	if text != "" {
		outputItems = append(outputItems, map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "output_text",
					"text": text,
				},
			},
		})
	}
	for i, block := range resp.Output {
		if block.Type != compatir.BlockToolCall || block.ToolCall == nil || block.ToolCall.Name == "" {
			continue
		}
		callID := block.ToolCall.ID
		if callID == "" {
			callID = fmt.Sprintf("fc_%d_%d", time.Now().UnixNano(), i)
		}
		args := block.ToolCall.Arguments
		if args == "" {
			args = "{}"
		}
		outputItems = append(outputItems, map[string]any{
			"id":        callID,
			"type":      "function_call",
			"call_id":   callID,
			"name":      block.ToolCall.Name,
			"arguments": args,
			"status":    "completed",
		})
	}

	out := map[string]any{
		"id":          id,
		"object":      "response",
		"status":      "completed",
		"model":       model,
		"output_text": text,
		"output":      outputItems,
	}
	if usage, ok := ResponsesUsage(resp.Usage); ok {
		out["usage"] = usage
	}
	return out
}

func ResponsesUsage(usage compatir.Usage) (map[string]any, bool) {
	raw := usage.Raw
	input := usage.InputTokens
	if input == 0 {
		input = intFromAny(raw["input_tokens"])
	}
	if input == 0 {
		input = intFromAny(raw["prompt_tokens"])
	}
	output := usage.OutputTokens
	if output == 0 {
		output = intFromAny(raw["output_tokens"])
	}
	if output == 0 {
		output = intFromAny(raw["completion_tokens"])
	}
	total := usage.TotalTokens
	if total == 0 {
		total = intFromAny(raw["total_tokens"])
	}
	if total == 0 && (input > 0 || output > 0) {
		total = input + output
	}
	cached := usage.CacheReadInputTokens
	if cached == 0 {
		cached = intFromAny(raw["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(raw["cached_input_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(raw["prompt_tokens_details"])["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(raw["input_tokens_details"])["cached_tokens"])
	}

	reasoning := intFromAny(raw["reasoning_tokens"])
	if reasoning == 0 {
		reasoning = intFromAny(raw["reasoning_output_tokens"])
	}
	if reasoning == 0 {
		reasoning = intFromAny(mapValue(raw["completion_tokens_details"])["reasoning_tokens"])
	}
	if reasoning == 0 {
		reasoning = intFromAny(mapValue(raw["output_tokens_details"])["reasoning_tokens"])
	}

	if input == 0 && output == 0 && total == 0 && cached == 0 && reasoning == 0 {
		return nil, false
	}
	out := map[string]any{
		"input_tokens":            input,
		"output_tokens":           output,
		"total_tokens":            total,
		"cached_input_tokens":     cached,
		"reasoning_output_tokens": reasoning,
	}
	if cached > 0 {
		out["input_tokens_details"] = map[string]any{
			"cached_tokens": cached,
		}
	}
	if reasoning > 0 {
		out["output_tokens_details"] = map[string]any{
			"reasoning_tokens": reasoning,
		}
	}
	return out, true
}

func ResponsesUsageFromChatPayload(payload map[string]any) (map[string]any, bool) {
	usage := mapValue(payload["usage"])
	if len(usage) == 0 {
		usage = mapValue(mapValue(payload["response"])["usage"])
	}
	if len(usage) == 0 {
		return nil, false
	}
	return ResponsesUsage(compatir.Usage{Raw: usage})
}

func MergeResponsesUsage(base map[string]any, incoming map[string]any) map[string]any {
	if len(base) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return base
	}
	out := make(map[string]any, len(base)+len(incoming))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range incoming {
		if intFromAny(v) != 0 {
			out[k] = v
			continue
		}
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	if details := mapValue(incoming["input_tokens_details"]); len(details) > 0 {
		merged := mapValue(out["input_tokens_details"])
		if len(merged) == 0 {
			merged = map[string]any{}
		}
		for k, v := range details {
			if intFromAny(v) != 0 || merged[k] == nil {
				merged[k] = v
			}
		}
		out["input_tokens_details"] = merged
		if cached := intFromAny(merged["cached_tokens"]); cached != 0 {
			out["cached_input_tokens"] = cached
		}
	}
	if cached := intFromAny(mapValue(out["input_tokens_details"])["cached_tokens"]); cached != 0 {
		out["cached_input_tokens"] = cached
	}
	if details := mapValue(incoming["output_tokens_details"]); len(details) > 0 {
		merged := mapValue(out["output_tokens_details"])
		if len(merged) == 0 {
			merged = map[string]any{}
		}
		for k, v := range details {
			if intFromAny(v) != 0 || merged[k] == nil {
				merged[k] = v
			}
		}
		out["output_tokens_details"] = merged
		if reasoning := intFromAny(merged["reasoning_tokens"]); reasoning != 0 {
			out["reasoning_output_tokens"] = reasoning
		}
	}
	if reasoning := intFromAny(mapValue(out["output_tokens_details"])["reasoning_tokens"]); reasoning != 0 {
		out["reasoning_output_tokens"] = reasoning
	}
	return out
}

func responseOutputText(blocks []compatir.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == compatir.BlockText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func responseReasoningOutputText(blocks []compatir.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != compatir.BlockReasoning || block.Reasoning == nil || block.Reasoning.Text == "" {
			continue
		}
		parts = append(parts, block.Reasoning.Text)
	}
	return strings.Join(parts, "\n")
}

func intFromAny(v any) int {
	if i, ok := intValue(v); ok {
		return i
	}
	return 0
}

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
