package anthropic

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

func ensureRaw(raw map[string]any) map[string]any {
	if raw != nil {
		return raw
	}
	return map[string]any{}
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
