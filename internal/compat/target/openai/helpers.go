package openai

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"spark/internal/compatir"
)

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		if math.Trunc(x) == x {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return ""
	}
}

func intValue(v any) int {
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

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func listValue(v any) []any {
	items, _ := v.([]any)
	return items
}

func usageFromChat(raw any) compatir.Usage {
	usage := mapValue(raw)
	return compatir.Usage{
		InputTokens:  intValue(usage["prompt_tokens"]),
		OutputTokens: intValue(usage["completion_tokens"]),
		TotalTokens:  intValue(usage["total_tokens"]),
		Raw:          usage,
	}
}

func stopReasonFromChat(reason string) compatir.StopReason {
	switch reason {
	case "stop":
		return compatir.StopReasonEndTurn
	case "length":
		return compatir.StopReasonMaxTokens
	case "tool_calls", "function_call":
		return compatir.StopReasonToolUse
	case "content_filter":
		return compatir.StopReasonContentFilter
	default:
		return compatir.StopReasonUnknown
	}
}

func reasoningText(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"text", "content", "summary"} {
			if s := stringValue(v[key]); s != "" {
				return s
			}
		}
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := reasoningText(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func normalizeContent(raw any) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case json.RawMessage:
		return strings.TrimSpace(string(v))
	case map[string]any:
		switch stringValue(v["type"]) {
		case "", "input_text", "output_text", "text":
			if text := stringValue(v["text"]); text != "" {
				return text
			}
		}
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	case []map[string]any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := normalizeContent(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := normalizeContent(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}
