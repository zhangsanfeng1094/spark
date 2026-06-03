package gateway

import (
	"encoding/json"
	"fmt"

	"spark/internal/compat/logutil"
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
	return logutil.StructureJSONForLog(v)
}
