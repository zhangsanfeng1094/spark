package anthropic_messages

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"spark/internal/compat/ir"
)

const defaultMaxTokens = 4096

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

func usageFromAnthropic(raw any) ir.Usage {
	usage := mapValue(raw)
	input := intValue(usage["input_tokens"])
	output := intValue(usage["output_tokens"])
	total := input + output
	cacheCreation := intValue(usage["cache_creation_input_tokens"])
	cacheRead := intValue(usage["cache_read_input_tokens"])
	return ir.Usage{
		InputTokens:              input,
		OutputTokens:             output,
		TotalTokens:              total,
		CacheCreationInputTokens: cacheCreation,
		CacheReadInputTokens:     cacheRead,
		Raw:                      usage,
	}
}

func stopReasonFromAnthropic(reason string) ir.StopReason {
	switch reason {
	case "end_turn", "stop_sequence":
		return ir.StopReasonEndTurn
	case "max_tokens":
		return ir.StopReasonMaxTokens
	case "tool_use":
		return ir.StopReasonToolUse
	default:
		return ir.StopReasonUnknown
	}
}

func reasoningBudget(effort ir.ReasoningEffort) int {
	switch effort {
	case ir.ReasoningEffortMinimal, ir.ReasoningEffortLow:
		return 1024
	case ir.ReasoningEffortMedium:
		return 4096
	case ir.ReasoningEffortHigh:
		return 8192
	case ir.ReasoningEffortXHigh:
		return 16384
	default:
		return 0
	}
}

func parseJSONMap(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
