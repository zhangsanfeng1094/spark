package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
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
	if err := encoder.Encode(redactLogValue("", v)); err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	return strings.TrimSpace(buf.String())
}

func StructureJSONForLog(v any) string {
	return structureJSONForLog(v)
}

func redactLogValue(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, value := range x {
			out[k] = redactLogValue(k, value)
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, value := range x {
			out = append(out, redactLogValue(key, value))
		}
		return out
	case []map[string]any:
		out := make([]any, 0, len(x))
		for _, value := range x {
			out = append(out, redactLogValue(key, value))
		}
		return out
	case string:
		if shouldRedactLogScalar(key) {
			return logPlaceholder(key, x)
		}
		return x
	default:
		return x
	}
}

func shouldRedactLogScalar(key string) bool {
	switch strings.ToLower(key) {
	case "content", "text", "output", "output_text", "reasoning_content", "thinking", "arguments":
		return true
	default:
		return false
	}
}

func logPlaceholder(key, value string) string {
	switch strings.ToLower(key) {
	case "arguments":
		return fmt.Sprintf("<json len=%d>", len(value))
	case "reasoning_content", "thinking":
		return fmt.Sprintf("<reasoning len=%d>", len(value))
	case "output", "output_text":
		return fmt.Sprintf("<output len=%d>", len(value))
	default:
		return fmt.Sprintf("<text len=%d>", len(value))
	}
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
