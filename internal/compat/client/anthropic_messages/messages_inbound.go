package anthropic_messages

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"spark/internal/compat/ir"
)

func MessagesInbound(req map[string]any) ir.Request {
	model := stringValue(req["model"])
	if model == "" {
		model = "unknown"
	}
	out := ir.Request{
		Model:    model,
		Messages: anthropicMessages(req),
		Tools:    anthropicTools(req["tools"]),
		Source:   ir.ProtocolAnthropicMessages,
		Raw:      req,
	}
	if max, ok := intValue(req["max_tokens"]); ok && max > 0 {
		out.Generation.MaxTokens = &max
	}
	if temp, ok := float64Value(req["temperature"]); ok {
		out.Generation.Temperature = temp
	} else if _, ok := req["temperature"]; ok {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["temperature"] = req["temperature"]
	}
	if topP, ok := float64Value(req["top_p"]); ok {
		out.Generation.TopP = topP
	} else if _, ok := req["top_p"]; ok {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["top_p"] = req["top_p"]
	}
	if stop, ok := req["stop_sequences"]; ok {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["stop"] = stop
	}
	if thinking, ok := req["thinking"]; ok {
		applyAnthropicThinkingConfig(&out.Generation.Reasoning, thinking)
	}
	if effort := outputConfigEffort(req["output_config"]); effort != "" {
		out.Generation.Reasoning.Effort = effort
	}
	if choice, ok := anthropicToolChoice(req["tool_choice"]); ok {
		out.ToolChoice = choice
	}
	if len(out.Messages) == 0 {
		out.Messages = []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text("")}}}
	}
	return out
}

func applyAnthropicThinkingConfig(reasoning *ir.ReasoningConfig, raw any) {
	if reasoning == nil {
		return
	}
	reasoning.Raw = ensureRaw(reasoning.Raw)
	reasoning.Raw["thinking"] = raw
	config := mapValue(raw)
	switch stringValue(config["type"]) {
	case "enabled":
		enabled := true
		reasoning.Enabled = &enabled
	case "disabled":
		enabled := false
		reasoning.Enabled = &enabled
	}
	if budget, ok := intValue(config["budget_tokens"]); ok {
		reasoning.BudgetTokens = &budget
	}
}

func outputConfigEffort(raw any) ir.ReasoningEffort {
	config, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	effort, _ := ir.ParseReasoningEffort(stringValue(config["effort"]))
	return effort
}

func anthropicMessages(req map[string]any) []ir.Message {
	out := make([]ir.Message, 0, 8)
	if sysBlocks := anthropicSystemBlocks(req["system"]); len(sysBlocks) > 0 {
		out = append(out, ir.Message{Role: ir.RoleSystem, Content: sysBlocks})
	}
	items, _ := req["messages"].([]any)
	for _, raw := range items {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := anthropicRole(msg["role"])
		blocks := anthropicContentBlocks(msg["content"])
		if role == ir.RoleAssistant {
			out = append(out, ir.Message{Role: ir.RoleAssistant, Content: assistantBlocks(blocks)})
			continue
		}
		if textBlocks := onlyBlocks(blocks, ir.BlockText); len(textBlocks) > 0 {
			out = append(out, ir.Message{Role: role, Content: textBlocks})
		}
		for _, block := range onlyBlocks(blocks, ir.BlockToolResult) {
			out = append(out, ir.Message{Role: ir.RoleTool, Content: []ir.ContentBlock{block}})
		}
	}
	return out
}

func anthropicSystemToString(raw any) string {
	return strings.Join(anthropicSystemTexts(raw), "\n")
}

func anthropicSystemTexts(raw any) []string {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		if t := strings.TrimSpace(v); t != "" {
			return []string{t}
		}
		return nil
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if stringValue(m["type"]) == "text" {
				if t := stringValue(m["text"]); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return parts
	default:
		if text := normalizeMessageContent(v); text != "" {
			return []string{text}
		}
		return nil
	}
}

func anthropicSystemBlocks(raw any) []ir.ContentBlock {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		if t := strings.TrimSpace(v); t != "" {
			return []ir.ContentBlock{ir.Text(t)}
		}
		return nil
	case []any:
		blocks := make([]ir.ContentBlock, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if stringValue(m["type"]) != "text" {
				continue
			}
			t := stringValue(m["text"])
			if t == "" {
				continue
			}
			block := ir.Text(t)
			if cc := mapValue(m["cache_control"]); cc != nil {
				block.CacheControl = cc
			}
			blocks = append(blocks, block)
		}
		return blocks
	default:
		if text := normalizeMessageContent(v); text != "" {
			return []ir.ContentBlock{ir.Text(text)}
		}
		return nil
	}
}

func anthropicRole(raw any) ir.Role {
	switch stringValue(raw) {
	case "assistant":
		return ir.RoleAssistant
	case "system":
		return ir.RoleSystem
	case "tool":
		return ir.RoleTool
	case "user", "":
		return ir.RoleUser
	default:
		return ir.RoleUser
	}
}

func anthropicContentBlocks(raw any) []ir.ContentBlock {
	switch v := raw.(type) {
	case string:
		return []ir.ContentBlock{ir.Text(v)}
	case []any:
		blocks := make([]ir.ContentBlock, 0, len(v))
		for idx, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch stringValue(m["type"]) {
			case "text", "input_text", "output_text":
				if t := stringValue(m["text"]); t != "" {
					block := ir.Text(t)
					if cc := mapValue(m["cache_control"]); cc != nil {
						block.CacheControl = cc
					}
					blocks = append(blocks, block)
				}
			case "thinking":
				// Anthropic `thinking` block: the assistant turn's reasoning.
				// `signature` MUST be preserved unchanged across turns so the
				// server can re-derive the full thinking on the next request.
				// `display` (summarized|omitted) may also be replayed.
				// Per Anthropic docs the block is returned even when
				// `thinking` is empty (display:"omitted"); we keep it as long
				// as a signature exists so multi-turn round-trip is safe.
				text := stringValue(m["thinking"])
				sig := stringValue(m["signature"])
				if text == "" && sig == "" {
					continue
				}
				block := ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:      text,
						Signature: sig,
						Display:   ir.ReasoningDisplay(stringValue(m["display"])),
					},
					Raw: m,
				}
				if cc := mapValue(m["cache_control"]); cc != nil {
					block.CacheControl = cc
				}
				blocks = append(blocks, block)
			case "redacted_thinking":
				// Anthropic `redacted_thinking` block: opaque server-encrypted
				// payload in `data`. We carry it verbatim via Raw and flag the
				// block as redacted so the outbound side can emit the original
				// `redacted_thinking` shape instead of a plain thinking block.
				data := stringValue(m["data"])
				if data == "" {
					continue
				}
				block := ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:     data,
						Redacted: true,
					},
					Raw: m,
				}
				if cc := mapValue(m["cache_control"]); cc != nil {
					block.CacheControl = cc
				}
				blocks = append(blocks, block)
			case "reasoning":
				// Legacy/openai-style `reasoning` block; preserve text only.
				if t := stringValue(m["text"]); t != "" {
					block := ir.Reasoning(t)
					if cc := mapValue(m["cache_control"]); cc != nil {
						block.CacheControl = cc
					}
					blocks = append(blocks, block)
				}
			case "tool_use":
				name := stringValue(m["name"])
				if name == "" {
					continue
				}
				id := stringValue(m["id"])
				if id == "" {
					id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), idx)
				}
				args := "{}"
				if data, err := json.Marshal(m["input"]); err == nil && len(data) > 0 {
					args = string(data)
				}
				block := ir.ContentBlock{
					Type: ir.BlockToolCall,
					ToolCall: &ir.ToolCall{
						ID:        id,
						Type:      ir.ToolTypeFunction,
						Name:      name,
						Arguments: args,
						Raw:       m,
					},
					Raw: m,
				}
				if cc := mapValue(m["cache_control"]); cc != nil {
					block.CacheControl = cc
				}
				blocks = append(blocks, block)
			case "tool_result":
				toolCallID := stringValue(m["tool_use_id"])
				if toolCallID == "" {
					continue
				}
				content := normalizeMessageContent(m["content"])
				if content == "" {
					content = "{}"
				}
				block := ir.ContentBlock{
					Type: ir.BlockToolResult,
					ToolResult: &ir.ToolResult{
						ToolCallID: toolCallID,
						Output:     content,
						Raw:        m,
					},
					Raw: m,
				}
				if cc := mapValue(m["cache_control"]); cc != nil {
					block.CacheControl = cc
				}
				blocks = append(blocks, block)
			}
		}
		return blocks
	default:
		if text := normalizeMessageContent(v); text != "" {
			return []ir.ContentBlock{ir.Text(text)}
		}
		return nil
	}
}

func assistantBlocks(blocks []ir.ContentBlock) []ir.ContentBlock {
	out := make([]ir.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == ir.BlockToolResult {
			continue
		}
		out = append(out, block)
	}
	return out
}

func onlyBlocks(blocks []ir.ContentBlock, typ ir.BlockType) []ir.ContentBlock {
	out := make([]ir.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == typ {
			out = append(out, block)
		}
	}
	return out
}

func anthropicTools(raw any) []ir.Tool {
	items, _ := raw.([]any)
	out := make([]ir.Tool, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(m["name"])
		if name == "" {
			continue
		}
		tool := ir.Tool{
			Type: ir.ToolTypeFunction,
			Function: ir.FunctionTool{
				Name:        name,
				Description: stringValue(m["description"]),
			},
			Raw: m,
		}
		if schema, ok := m["input_schema"]; ok {
			tool.Function.Parameters = schema
		}
		if cc := mapValue(m["cache_control"]); cc != nil {
			tool.CacheControl = cc
		}
		out = append(out, tool)
	}
	return out
}

func anthropicToolChoice(raw any) (ir.ToolChoice, bool) {
	if raw == nil {
		return ir.ToolChoice{}, false
	}
	switch v := raw.(type) {
	case string:
		switch v {
		case "auto":
			return ir.ToolChoice{Mode: ir.ToolChoiceAuto}, true
		case "any":
			return ir.ToolChoice{Mode: ir.ToolChoiceRequired}, true
		case "none":
			return ir.ToolChoice{Mode: ir.ToolChoiceNone}, true
		default:
			return ir.ToolChoice{}, false
		}
	case map[string]any:
		switch stringValue(v["type"]) {
		case "auto":
			return ir.ToolChoice{Mode: ir.ToolChoiceAuto}, true
		case "any":
			return ir.ToolChoice{Mode: ir.ToolChoiceRequired}, true
		case "tool":
			name := stringValue(v["name"])
			if name == "" {
				return ir.ToolChoice{}, false
			}
			return ir.ToolChoice{Mode: ir.ToolChoiceFunction, Name: name}, true
		default:
			return ir.ToolChoice{}, false
		}
	default:
		return ir.ToolChoice{}, false
	}
}
