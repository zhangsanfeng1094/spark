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
	if sys := anthropicSystemToString(req["system"]); sys != "" {
		out = append(out, ir.Message{Role: ir.RoleSystem, Content: []ir.ContentBlock{ir.Text(sys)}})
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
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
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
		return strings.Join(parts, "\n")
	default:
		return normalizeMessageContent(v)
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
					blocks = append(blocks, ir.Text(t))
				}
			case "thinking":
				if t := stringValue(m["thinking"]); t != "" {
					blocks = append(blocks, ir.Reasoning(t))
				}
			case "reasoning":
				if t := stringValue(m["text"]); t != "" {
					blocks = append(blocks, ir.Reasoning(t))
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
				blocks = append(blocks, ir.ContentBlock{
					Type: ir.BlockToolCall,
					ToolCall: &ir.ToolCall{
						ID:        id,
						Type:      ir.ToolTypeFunction,
						Name:      name,
						Arguments: args,
						Raw:       m,
					},
					Raw: m,
				})
			case "tool_result":
				toolCallID := stringValue(m["tool_use_id"])
				if toolCallID == "" {
					continue
				}
				content := normalizeMessageContent(m["content"])
				if content == "" {
					content = "{}"
				}
				blocks = append(blocks, ir.ContentBlock{
					Type: ir.BlockToolResult,
					ToolResult: &ir.ToolResult{
						ToolCallID: toolCallID,
						Output:     content,
						Raw:        m,
					},
					Raw: m,
				})
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
