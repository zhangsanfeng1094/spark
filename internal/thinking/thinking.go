// Package thinking resolves Spark profile policy and patches an ingress request
// before it is handed to Bifrost.  It intentionally knows client wire shapes,
// but not upstream model capability or provider mappings.
package thinking

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"spark/internal/config"
)

const (
	ModeClient = "client"
	ModeAuto   = "auto"
	ModeForce  = "force"
	ModeOff    = "off"
)

// ParseShorthand accepts the compact values used by --think and model suffixes.
// It deliberately returns false for ordinary model names such as qwen3:8b.
func ParseShorthand(value string) (*config.ThinkingConfig, bool) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "off" {
		return &config.ThinkingConfig{Mode: ModeOff}, true
	}
	if isEffort(v) {
		return &config.ThinkingConfig{Mode: ModeForce, Effort: v}, true
	}
	if strings.HasSuffix(v, "k") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "k"))
		if err == nil && n > 0 {
			n *= 1024
			return &config.ThinkingConfig{Mode: ModeForce, BudgetTokens: &n}, true
		}
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return &config.ThinkingConfig{Mode: ModeForce, BudgetTokens: &n}, true
	}
	return nil, false
}

// SplitModelSuffix removes a valid :think shorthand without changing ordinary
// colon-containing model names.
func SplitModelSuffix(model string) (string, *config.ThinkingConfig) {
	i := strings.LastIndex(model, ":")
	if i < 1 || i == len(model)-1 {
		return model, nil
	}
	cfg, ok := ParseShorthand(model[i+1:])
	if !ok {
		return model, nil
	}
	return model[:i], cfg
}

// Resolve applies the documented precedence. Profile off cannot be overridden.
// The returned config is safe for the caller to retain and never aliases input.
func Resolve(profile, session *config.ThinkingConfig) *config.ThinkingConfig {
	if normalizeMode(profile) == ModeOff {
		return clone(profile)
	}
	if session != nil {
		return clone(session)
	}
	if normalizeMode(profile) == ModeAuto || normalizeMode(profile) == ModeForce {
		return clone(profile)
	}
	return nil
}

// Apply patches raw in place. Protocol is the client protocol, never the
// selected upstream protocol, so it remains correct on Bifrost convert routes.
func Apply(raw map[string]any, protocol config.APIProtocol, policy *config.ThinkingConfig) {
	if raw == nil || policy == nil {
		return
	}
	mode := normalizeMode(policy)
	if mode == "" || mode == ModeClient {
		return
	}
	if mode == ModeAuto && hasThinking(raw, protocol) {
		return
	}
	if mode != ModeAuto && mode != ModeForce && mode != ModeOff {
		return
	}
	switch protocol {
	case config.ProtocolAnthropic:
		applyAnthropic(raw, policy, mode)
	case config.ProtocolOpenAIResponses:
		applyResponses(raw, policy, mode)
	case config.ProtocolOpenAIChat:
		applyChat(raw, policy, mode)
	case config.ProtocolGemini:
		applyGemini(raw, policy, mode)
	}
}

func applyAnthropic(raw map[string]any, policy *config.ThinkingConfig, mode string) {
	if mode == ModeOff {
		raw["thinking"] = map[string]any{"type": "disabled"}
		return
	}
	if budget := validBudget(policy); budget != nil {
		raw["thinking"] = map[string]any{"type": "enabled", "budget_tokens": *budget}
		max, _ := number(raw["max_tokens"])
		if max <= *budget {
			raw["max_tokens"] = *budget + 1
		}
		delete(raw, "temperature")
		delete(raw, "top_k")
		return
	}
	if effort := validEffort(policy.Effort); effort != "" {
		raw["thinking"] = map[string]any{"type": "adaptive"}
		output := object(raw, "output_config")
		output["effort"] = effort
	}
}

func applyResponses(raw map[string]any, policy *config.ThinkingConfig, mode string) {
	if mode == ModeOff {
		delete(raw, "reasoning")
		return
	}
	if value := policyValue(policy); value != nil {
		raw["reasoning"] = map[string]any{"effort": value}
	}
}

func applyChat(raw map[string]any, policy *config.ThinkingConfig, mode string) {
	if mode == ModeOff {
		delete(raw, "reasoning_effort")
		return
	}
	if value := policyValue(policy); value != nil {
		raw["reasoning_effort"] = value
	}
}

func applyGemini(raw map[string]any, policy *config.ThinkingConfig, mode string) {
	cfg := object(raw, "generationConfig")
	if mode == ModeOff {
		cfg["thinkingBudget"] = 0
		delete(cfg, "thinkingLevel")
		return
	}
	if budget := validBudget(policy); budget != nil {
		cfg["thinkingBudget"] = *budget
		return
	}
	if effort := validEffort(policy.Effort); effort != "" {
		// If the client opted into level semantics, preserve that choice.
		if _, hasLevel := cfg["thinkingLevel"]; hasLevel {
			cfg["thinkingLevel"] = effort
		} else {
			cfg["thinkingBudget"] = effortBudget(effort)
		}
	}
}

func hasThinking(raw map[string]any, protocol config.APIProtocol) bool {
	switch protocol {
	case config.ProtocolAnthropic:
		_, ok := raw["thinking"]
		return ok
	case config.ProtocolOpenAIResponses:
		_, ok := raw["reasoning"]
		return ok
	case config.ProtocolOpenAIChat:
		_, ok := raw["reasoning_effort"]
		return ok
	case config.ProtocolGemini:
		cfg, _ := raw["generationConfig"].(map[string]any)
		if cfg == nil {
			return false
		}
		_, budget := cfg["thinkingBudget"]
		_, level := cfg["thinkingLevel"]
		return budget || level
	}
	return false
}

func policyValue(policy *config.ThinkingConfig) any {
	if budget := validBudget(policy); budget != nil {
		return *budget
	}
	if effort := validEffort(policy.Effort); effort != "" {
		return effort
	}
	return nil
}

func effortBudget(effort string) int {
	switch effort {
	case "minimal":
		return 1024
	case "low":
		return 2048
	case "medium":
		return 4096
	case "high":
		return 8192
	case "xhigh":
		return 12288
	case "max":
		return 16384
	default:
		return 0
	}
}

func object(raw map[string]any, key string) map[string]any {
	if v, ok := raw[key].(map[string]any); ok {
		return v
	}
	v := map[string]any{}
	raw[key] = v
	return v
}

func number(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func validBudget(c *config.ThinkingConfig) *int {
	if c != nil && c.BudgetTokens != nil && *c.BudgetTokens > 0 {
		n := *c.BudgetTokens
		return &n
	}
	return nil
}
func validEffort(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if isEffort(v) {
		return v
	}
	return ""
}
func isEffort(v string) bool {
	switch v {
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}
func normalizeMode(c *config.ThinkingConfig) string {
	if c == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(c.Mode))
}
func clone(c *config.ThinkingConfig) *config.ThinkingConfig {
	if c == nil {
		return nil
	}
	out := *c
	if c.BudgetTokens != nil {
		n := *c.BudgetTokens
		out.BudgetTokens = &n
	}
	return &out
}

// String is a compact preview suitable for a launcher or TUI summary.
func String(c *config.ThinkingConfig) string {
	if c == nil || normalizeMode(c) == "" || normalizeMode(c) == ModeClient {
		return "Client"
	}
	if normalizeMode(c) == ModeOff {
		return "Off"
	}
	if b := validBudget(c); b != nil {
		return fmt.Sprintf("%s · %d", strings.Title(normalizeMode(c)), *b)
	}
	return fmt.Sprintf("%s · %s", strings.Title(normalizeMode(c)), validEffort(c.Effort))
}
