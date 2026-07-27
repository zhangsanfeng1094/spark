package policy

import (
	"strings"

	"spark/internal/compat/ir"
)

type ReasoningMode string

const (
	ReasoningStrip               ReasoningMode = "strip"
	ReasoningPreserve            ReasoningMode = "preserve"
	ReasoningRequireForToolCalls ReasoningMode = "require_for_tool_calls"
)

type ReasoningPolicy struct {
	Mode                 ReasoningMode
	Field                string
	AllowReasoningEffort bool
	AllowThinking        bool
}

func OpenAIChatReasoningPolicy(upstreamBase, model string) ReasoningPolicy {
	allowEffort := SupportsOpenAIChatReasoningEffort(upstreamBase, model)
	if RequiresOpenAIChatReasoningEcho(upstreamBase, model) {
		return ReasoningPolicy{
			Mode:                 ReasoningRequireForToolCalls,
			Field:                openAIChatReasoningField(upstreamBase, model),
			AllowReasoningEffort: allowEffort,
		}
	}
	return ReasoningPolicy{
		Mode:                 ReasoningStrip,
		Field:                openAIChatReasoningField(upstreamBase, model),
		AllowReasoningEffort: allowEffort,
	}
}

// openAIChatReasoningField picks the reasoning-text field name to write back
// for a given OpenAI-Chat upstream. Mirrors opencode's per-model
// capabilities.interleaved.field: read side stays tolerant of many field
// shapes (see openai_chat.reasoningContentKeys), write side only emits the
// one field the upstream understands. Default "reasoning_content" is the
// most widely-recognized DeepSeek-extension field.
func openAIChatReasoningField(upstreamBase, model string) string {
	target := strings.ToLower(upstreamBase + " " + model)
	switch {
	case strings.Contains(target, "copilot"):
		return "reasoning_text"
	case strings.Contains(target, "qwen"):
		return "thought"
	default:
		return "reasoning_content"
	}
}

// PreserveReasoningContent returns the generic bridge preserve policy used
// when no per-upstream URL is configured (model-routed openai_chat fallback,
// see anthropic_messages_handler.reasoningPolicy and bridge/registry). The
// reasoning text is written back as "reasoning_content" — the most widely
// recognized DeepSeek-extension field — NOT anthropic's "thinking", because
// this path serves openai_chat models (mimo/gpt-4.1/etc.), not an
// anthropic-protocol upstream. The per-upstream field selector
// openAIChatReasoningField handles genuine provider-specific write-back.
func PreserveReasoningContent() ReasoningPolicy {
	return ReasoningPolicy{
		Mode:                 ReasoningPreserve,
		Field:                "reasoning_content",
		AllowReasoningEffort: true,
		AllowThinking:        true,
	}
}

func StripReasoningContent() ReasoningPolicy {
	return ReasoningPolicy{
		Mode:  ReasoningStrip,
		Field: "reasoning_content",
	}
}

func (p ReasoningPolicy) ChatReasoningContent(msg ir.Message) (string, bool) {
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

func SupportsOpenAIChatReasoningEffort(upstreamBase, model string) bool {
	target := strings.ToLower(upstreamBase + " " + model)
	if !strings.Contains(target, "api.openai.com") {
		return false
	}
	model = strings.ToLower(model)
	return strings.HasPrefix(model, "o") ||
		strings.HasPrefix(model, "gpt-5") ||
		strings.HasPrefix(model, "gpt-4.1")
}

func (p ReasoningPolicy) ChatReasoningControls(reasoning ir.ReasoningConfig) (map[string]any, []string) {
	if !reasoning.HasControls() {
		return nil, nil
	}
	out := map[string]any{}
	dropped := make([]string, 0, 4)
	if reasoning.Effort != "" {
		if p.AllowReasoningEffort {
			out["reasoning_effort"] = string(reasoning.Effort)
		} else {
			dropped = append(dropped, "reasoning_effort")
		}
	}
	if reasoning.Enabled != nil || reasoning.BudgetTokens != nil {
		if p.AllowThinking {
			thinking := map[string]any{}
			if reasoning.Enabled != nil {
				if *reasoning.Enabled {
					thinking["type"] = "enabled"
				} else {
					thinking["type"] = "disabled"
				}
			}
			if reasoning.BudgetTokens != nil {
				thinking["budget_tokens"] = *reasoning.BudgetTokens
			}
			if len(thinking) > 0 {
				out["thinking"] = thinking
			}
		} else {
			if reasoning.Enabled != nil {
				dropped = append(dropped, "thinking.type")
			}
			if reasoning.BudgetTokens != nil {
				dropped = append(dropped, "thinking.budget_tokens")
			}
		}
	}
	if reasoning.IncludeThoughts != nil {
		dropped = append(dropped, "include_thoughts")
	}
	if reasoning.Summary != "" {
		dropped = append(dropped, "reasoning.summary")
	}
	return out, dropped
}
