package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"spark/internal/compat/ir"
)

func mustJSONForLog(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	return string(data)
}

func truncateForLog(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func structureJSONForLog(v any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(redactLogValue("", normalizeLogValue(v))); err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	return strings.TrimSpace(buf.String())
}

func StructureJSONForLog(v any) string {
	return structureJSONForLog(v)
}

func normalizeLogValue(v any) any {
	switch v.(type) {
	case nil, map[string]any, []any, []map[string]any, string, bool, int, int64, float64, json.Number:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return v
		}
		var out any
		if err := json.Unmarshal(data, &out); err != nil {
			return v
		}
		return out
	}
}

func redactLogValue(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		if shouldSummarizeLogComposite(key) {
			return logCompositePlaceholder(key, len(x))
		}
		out := make(map[string]any, len(x))
		for k, value := range x {
			out[k] = redactLogValue(k, value)
		}
		return out
	case []any:
		if shouldSummarizeLogComposite(key) {
			return logCompositePlaceholder(key, len(x))
		}
		out := make([]any, 0, len(x))
		for _, value := range x {
			out = append(out, redactLogValue(key, value))
		}
		return out
	case []map[string]any:
		if shouldSummarizeLogComposite(key) {
			return logCompositePlaceholder(key, len(x))
		}
		out := make([]any, 0, len(x))
		for _, value := range x {
			out = append(out, redactLogValue(key, value))
		}
		return out
	case string:
		if shouldSummarizeLogScalar(key) {
			return logPlaceholder(key, x)
		}
		return x
	default:
		return x
	}
}

func shouldSummarizeLogScalar(key string) bool {
	switch normalizeLogKey(key) {
	case "arguments", "content", "description", "instructions", "output", "output_text", "partial_json", "prompt", "reasoning", "reasoning_content", "system", "text", "thinking":
		return true
	default:
		return false
	}
}

func shouldSummarizeLogComposite(key string) bool {
	switch normalizeLogKey(key) {
	case "input":
		return true
	default:
		return false
	}
}

func logPlaceholder(key, value string) string {
	switch normalizeLogKey(key) {
	case "arguments", "partial_json":
		return fmt.Sprintf("<json len=%d>", len(value))
	case "description":
		return fmt.Sprintf("<description len=%d>", len(value))
	case "input":
		return fmt.Sprintf("<input len=%d>", len(value))
	case "reasoning", "reasoning_content", "thinking":
		return fmt.Sprintf("<reasoning len=%d>", len(value))
	case "output", "output_text":
		return fmt.Sprintf("<output len=%d>", len(value))
	default:
		return fmt.Sprintf("<text len=%d>", len(value))
	}
}

func logCompositePlaceholder(key string, count int) string {
	switch normalizeLogKey(key) {
	case "input":
		return fmt.Sprintf("<input items=%d>", count)
	default:
		return fmt.Sprintf("<%s items=%d>", normalizeLogKey(key), count)
	}
}

func normalizeLogKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
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

func logCompatStage(logf func(string, ...any), stage string, v any) {
	callLogf(logf, "middleware stage=%s structure=%s", stage, structureJSONForLog(v))
}

func logCompatUsage(logf func(string, ...any), stage string, u ir.Usage) {
	callLogf(logf, "middleware stage=%s %s raw_usage=%s", stage, formatIRUsageForLog(u), structureJSONForLog(u.Raw))
}

func formatIRUsageForLog(u ir.Usage) string {
	return fmt.Sprintf("usage input=%d output=%d total=%d cache_creation=%d cache_read=%d",
		u.InputTokens, u.OutputTokens, u.TotalTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens)
}
