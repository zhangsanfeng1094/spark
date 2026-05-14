package openai

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
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

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func intValue(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		i, err := x.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func float64Value(v any) (*float64, bool) {
	switch x := v.(type) {
	case float64:
		return &x, true
	case int:
		f := float64(x)
		return &f, true
	case int64:
		f := float64(x)
		return &f, true
	case json.Number:
		f, err := x.Float64()
		return &f, err == nil
	default:
		return nil, false
	}
}

func normalizeMessageContent(raw any) string {
	if raw == nil {
		return ""
	}
	switch c := raw.(type) {
	case string:
		return c
	case json.RawMessage:
		return strings.TrimSpace(string(c))
	case map[string]any:
		switch stringValue(c["type"]) {
		case "", "input_text", "output_text", "text":
			if t := stringValue(c["text"]); t != "" {
				return t
			}
		}
		if data, err := json.Marshal(c); err == nil {
			return string(data)
		}
		return fmt.Sprint(c)
	case []map[string]any:
		parts := make([]string, 0, len(c))
		for _, item := range c {
			if text := normalizeMessageContent(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []any:
		parts := make([]string, 0, len(c))
		for _, item := range c {
			if text := normalizeMessageContent(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []byte:
		return strings.TrimSpace(string(c))
	default:
		if data, err := json.Marshal(c); err == nil {
			return string(data)
		}
		return fmt.Sprint(c)
	}
}

func responseReasoningContent(msg map[string]any) string {
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if text := normalizeMessageContent(msg[key]); text != "" {
			return text
		}
	}
	if text := responseReasoningTextList(msg["summary"]); text != "" {
		return text
	}
	if stringValue(msg["type"]) == "reasoning" {
		if text := responseReasoningTextList(msg["content"]); text != "" {
			return text
		}
	}
	return stringValue(msg["text"])
}

func responseReasoningTextList(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case []map[string]any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := responseReasoningTextItem(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if text := responseReasoningTextItem(m); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		return responseReasoningTextItem(v)
	default:
		return ""
	}
}

func responseReasoningTextItem(item map[string]any) string {
	switch stringValue(item["type"]) {
	case "summary_text", "reasoning_text", "text", "output_text", "":
		return stringValue(item["text"])
	default:
		return ""
	}
}
