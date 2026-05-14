package policy

import (
	"strings"

	"spark/internal/compatir"
)

type ReasoningMode string

const (
	ReasoningStrip               ReasoningMode = "strip"
	ReasoningPreserve            ReasoningMode = "preserve"
	ReasoningRequireForToolCalls ReasoningMode = "require_for_tool_calls"
)

type ReasoningPolicy struct {
	Mode  ReasoningMode
	Field string
}

func OpenAIChatReasoningPolicy(upstreamBase, model string) ReasoningPolicy {
	if RequiresOpenAIChatReasoningEcho(upstreamBase, model) {
		return ReasoningPolicy{
			Mode:  ReasoningRequireForToolCalls,
			Field: "reasoning_content",
		}
	}
	return ReasoningPolicy{
		Mode:  ReasoningStrip,
		Field: "reasoning_content",
	}
}

func PreserveReasoningContent() ReasoningPolicy {
	return ReasoningPolicy{
		Mode:  ReasoningPreserve,
		Field: "reasoning_content",
	}
}

func StripReasoningContent() ReasoningPolicy {
	return ReasoningPolicy{
		Mode:  ReasoningStrip,
		Field: "reasoning_content",
	}
}

func (p ReasoningPolicy) ChatReasoningContent(msg compatir.Message) (string, bool) {
	switch p.mode() {
	case ReasoningPreserve:
		reasoning := msg.ReasoningText()
		return reasoning, reasoning != ""
	case ReasoningRequireForToolCalls:
		if !msg.HasToolCalls() {
			return "", false
		}
		return msg.ReasoningText(), true
	default:
		return "", false
	}
}

func (p ReasoningPolicy) ChatReasoningField() string {
	if p.Field != "" {
		return p.Field
	}
	return "reasoning_content"
}

func (p ReasoningPolicy) mode() ReasoningMode {
	if p.Mode != "" {
		return p.Mode
	}
	return ReasoningStrip
}

func RequiresOpenAIChatReasoningEcho(upstreamBase, model string) bool {
	target := strings.ToLower(upstreamBase + " " + model)
	return strings.Contains(target, "xiaomimimo") ||
		strings.Contains(target, "mimo") ||
		strings.Contains(target, "deepseek")
}
