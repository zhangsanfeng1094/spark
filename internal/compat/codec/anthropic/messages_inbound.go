package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"spark/internal/compatir"
)

func MessagesInbound(req map[string]any) compatir.Request {
	model := stringValue(req["model"])
	if model == "" {
		model = "unknown"
	}
	out := compatir.Request{
		Model:    model,
		Messages: anthropicMessages(req),
		Tools:    anthropicTools(req["tools"]),
		Source:   compatir.ProtocolAnthropicMessages,
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
	if choice, ok := anthropicToolChoice(req["tool_choice"]); ok {
		out.ToolChoice = choice
	}
	if len(out.Messages) == 0 {
		out.Messages = []compatir.Message{{Role: compatir.RoleUser, Content: []compatir.ContentBlock{compatir.Text("")}}}
	}
	return out
}

func anthropicMessages(req map[string]any) []compatir.Message {
	out := make([]compatir.Message, 0, 8)
	if sys := anthropicSystemToString(req["system"]); sys != "" {
		out = append(out, compatir.Message{Role: compatir.RoleSystem, Content: []compatir.ContentBlock{compatir.Text(sys)}})
	}
	items, _ := req["messages"].([]any)
	for _, raw := range items {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := anthropicRole(msg["role"])
		blocks := anthropicContentBlocks(msg["content"])
		if role == compatir.RoleAssistant {
			out = append(out, compatir.Message{Role: compatir.RoleAssistant, Content: assistantBlocks(blocks)})
			continue
		}
		if textBlocks := onlyBlocks(blocks, compatir.BlockText); len(textBlocks) > 0 {
			out = append(out, compatir.Message{Role: role, Content: textBlocks})
		}
		for _, block := range onlyBlocks(blocks, compatir.BlockToolResult) {
			out = append(out, compatir.Message{Role: compatir.RoleTool, Content: []compatir.ContentBlock{block}})
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

func anthropicRole(raw any) compatir.Role {
	switch stringValue(raw) {
	case "assistant":
		return compatir.RoleAssistant
	case "system":
		return compatir.RoleSystem
	case "tool":
		return compatir.RoleTool
	case "user", "":
		return compatir.RoleUser
	default:
		return compatir.RoleUser
	}
}

func anthropicContentBlocks(raw any) []compatir.ContentBlock {
	switch v := raw.(type) {
	case string:
		return []compatir.ContentBlock{compatir.Text(v)}
	case []any:
		blocks := make([]compatir.ContentBlock, 0, len(v))
		for idx, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch stringValue(m["type"]) {
			case "text", "input_text", "output_text":
				if t := stringValue(m["text"]); t != "" {
					blocks = append(blocks, compatir.Text(t))
				}
			case "thinking":
				if t := stringValue(m["thinking"]); t != "" {
					blocks = append(blocks, compatir.Reasoning(t))
				}
			case "reasoning":
				if t := stringValue(m["text"]); t != "" {
					blocks = append(blocks, compatir.Reasoning(t))
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
				blocks = append(blocks, compatir.ContentBlock{
					Type: compatir.BlockToolCall,
					ToolCall: &compatir.ToolCall{
						ID:        id,
						Type:      compatir.ToolTypeFunction,
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
				blocks = append(blocks, compatir.ContentBlock{
					Type: compatir.BlockToolResult,
					ToolResult: &compatir.ToolResult{
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
			return []compatir.ContentBlock{compatir.Text(text)}
		}
		return nil
	}
}

func assistantBlocks(blocks []compatir.ContentBlock) []compatir.ContentBlock {
	out := make([]compatir.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == compatir.BlockToolResult {
			continue
		}
		out = append(out, block)
	}
	return out
}

func onlyBlocks(blocks []compatir.ContentBlock, typ compatir.BlockType) []compatir.ContentBlock {
	out := make([]compatir.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == typ {
			out = append(out, block)
		}
	}
	return out
}

func anthropicTools(raw any) []compatir.Tool {
	items, _ := raw.([]any)
	out := make([]compatir.Tool, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(m["name"])
		if name == "" {
			continue
		}
		tool := compatir.Tool{
			Type: compatir.ToolTypeFunction,
			Function: compatir.FunctionTool{
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

func anthropicToolChoice(raw any) (compatir.ToolChoice, bool) {
	if raw == nil {
		return compatir.ToolChoice{}, false
	}
	switch v := raw.(type) {
	case string:
		switch v {
		case "auto":
			return compatir.ToolChoice{Mode: compatir.ToolChoiceAuto}, true
		case "any":
			return compatir.ToolChoice{Mode: compatir.ToolChoiceRequired}, true
		case "none":
			return compatir.ToolChoice{Mode: compatir.ToolChoiceNone}, true
		default:
			return compatir.ToolChoice{}, false
		}
	case map[string]any:
		switch stringValue(v["type"]) {
		case "auto":
			return compatir.ToolChoice{Mode: compatir.ToolChoiceAuto}, true
		case "any":
			return compatir.ToolChoice{Mode: compatir.ToolChoiceRequired}, true
		case "tool":
			name := stringValue(v["name"])
			if name == "" {
				return compatir.ToolChoice{}, false
			}
			return compatir.ToolChoice{Mode: compatir.ToolChoiceFunction, Name: name}, true
		default:
			return compatir.ToolChoice{}, false
		}
	default:
		return compatir.ToolChoice{}, false
	}
}
