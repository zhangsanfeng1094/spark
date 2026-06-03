package logutil

import (
	"encoding/json"
	"fmt"

	"spark/internal/compat/ir"
)

func FormatIRUsageForLog(u ir.Usage) string {
	return fmt.Sprintf("usage input=%d output=%d total=%d cache_creation=%d cache_read=%d",
		u.InputTokens, u.OutputTokens, u.TotalTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens)
}

func FormatUsageForLog(usage map[string]any) string {
	input := intFromAny(usage["input_tokens"])
	if input == 0 {
		input = intFromAny(usage["prompt_tokens"])
	}
	output := intFromAny(usage["output_tokens"])
	if output == 0 {
		output = intFromAny(usage["completion_tokens"])
	}
	total := intFromAny(usage["total_tokens"])
	if total == 0 && (input > 0 || output > 0) {
		total = input + output
	}
	cached := intFromAny(usage["cached_input_tokens"])
	if cached == 0 {
		cached = intFromAny(usage["cache_read_input_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(usage["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(usage["prompt_tokens_details"])["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(usage["input_tokens_details"])["cached_tokens"])
	}
	cacheCreation := intFromAny(usage["cache_creation_input_tokens"])
	reasoning := intFromAny(usage["reasoning_output_tokens"])
	if reasoning == 0 {
		reasoning = intFromAny(mapValue(usage["output_tokens_details"])["reasoning_tokens"])
	}
	return fmt.Sprintf("usage input=%d output=%d total=%d cached=%d cache_creation=%d reasoning=%d", input, output, total, cached, cacheCreation, reasoning)
}

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		i, _ := x.Int64()
		return int(i)
	default:
		return 0
	}
}
