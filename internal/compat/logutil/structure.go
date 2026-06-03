package logutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func StructureJSONForLog(v any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(redactLogValue("", normalizeLogValue(v))); err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	return strings.TrimSpace(buf.String())
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
