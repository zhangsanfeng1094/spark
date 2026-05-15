package gemini_generate_content

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

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func listValue(v any) []any {
	items, _ := v.([]any)
	return items
}

func ensureRaw(raw map[string]any) map[string]any {
	if raw != nil {
		return raw
	}
	return map[string]any{}
}

func normalizeContent(raw any) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case []byte:
		return strings.TrimSpace(string(v))
	case json.RawMessage:
		return strings.TrimSpace(string(v))
	case map[string]any:
		if text := stringValue(v["text"]); text != "" {
			return text
		}
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := normalizeContent(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}

func jsonObjectString(v any) string {
	if v == nil {
		return "{}"
	}
	if s := strings.TrimSpace(stringValue(v)); s != "" {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil || len(data) == 0 {
		return "{}"
	}
	return string(data)
}

func objectFromJSONString(s string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(s) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
