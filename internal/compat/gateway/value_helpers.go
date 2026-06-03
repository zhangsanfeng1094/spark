package gateway

import "encoding/json"

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
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
