package integrations

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
		itemType := stringValue(c["type"])
		switch itemType {
		case "", "input_text", "output_text", "text":
			if t := stringValue(c["text"]); t != "" {
				return t
			}
		}
		if data, err := json.Marshal(c); err == nil {
			return string(data)
		}
		if t := stringValue(c["content"]); t != "" {
			return t
		}
		return ""
	case []any:
		parts := make([]string, 0, len(c))
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemType := stringValue(m["type"])
			switch itemType {
			case "input_text", "output_text", "text":
				if t := stringValue(m["text"]); t != "" {
					parts = append(parts, t)
				}
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

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}
