package gateway

import (
	"spark/internal/compat/ir"
	"spark/internal/compat/logutil"
)

func logCompatStage(logf func(string, ...any), stage string, v any) {
	callLogf(logf, "middleware stage=%s structure=%s", stage, structureJSONForLog(v))
}

func logCompatUsage(logf func(string, ...any), stage string, u ir.Usage) {
	callLogf(logf, "middleware stage=%s %s raw_usage=%s", stage, formatIRUsageForLog(u), structureJSONForLog(u.Raw))
}

func formatIRUsageForLog(u ir.Usage) string {
	return logutil.FormatIRUsageForLog(u)
}

func formatUsageForLog(usage map[string]any) string {
	return logutil.FormatUsageForLog(usage)
}
