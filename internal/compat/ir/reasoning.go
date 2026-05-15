package ir

type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
)

type ReasoningSummary string

type ReasoningConfig struct {
	Enabled         *bool
	Effort          ReasoningEffort
	BudgetTokens    *int
	IncludeThoughts *bool
	Summary         ReasoningSummary
	Raw             map[string]any
}

func ParseReasoningEffort(value string) (ReasoningEffort, bool) {
	switch ReasoningEffort(value) {
	case ReasoningEffortNone,
		ReasoningEffortMinimal,
		ReasoningEffortLow,
		ReasoningEffortMedium,
		ReasoningEffortHigh,
		ReasoningEffortXHigh:
		return ReasoningEffort(value), true
	default:
		return "", false
	}
}

func (c ReasoningConfig) HasControls() bool {
	return c.Enabled != nil ||
		c.Effort != "" ||
		c.BudgetTokens != nil ||
		c.IncludeThoughts != nil ||
		c.Summary != "" ||
		len(c.Raw) > 0
}
