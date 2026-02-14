package integrations

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func anthropicToChatCompletions(req map[string]any) map[string]any {
	out := map[string]any{
		"model":    stringValue(req["model"]),
		"messages": anthropicMessagesToChatMessages(req),
		"stream":   false,
	}
	if out["model"] == "" {
		out["model"] = "unknown"
	}
	if max, ok := intValue(req["max_tokens"]); ok && max > 0 {
		out["max_tokens"] = max
	}
	if v, ok := req["temperature"]; ok {
		out["temperature"] = v
	}
	if v, ok := req["top_p"]; ok {
		out["top_p"] = v
	}
	if v, ok := req["stop_sequences"]; ok {
		out["stop"] = v
	}
	if tools := anthropicToolsToChatTools(req["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}
	if tc, ok := anthropicToolChoiceToChatToolChoice(req["tool_choice"]); ok {
		out["tool_choice"] = tc
	}
	return out
}

func anthropicMessagesToChatMessages(req map[string]any) []map[string]any {
	out := make([]map[string]any, 0, 8)
	if sys := anthropicSystemToString(req["system"]); sys != "" {
		out = append(out, map[string]any{
			"role":    "system",
			"content": sys,
		})
	}
	items, _ := req["messages"].([]any)
	for _, raw := range items {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := stringValue(msg["role"])
		if role == "" {
			role = "user"
		}
		text, toolCalls, toolResults := anthropicContentToChatParts(role, msg["content"])
		if role == "assistant" {
			assistant := map[string]any{
				"role":    "assistant",
				"content": text,
			}
			if len(toolCalls) > 0 {
				assistant["tool_calls"] = toolCalls
			}
			out = append(out, assistant)
			continue
		}
		if text != "" {
			out = append(out, map[string]any{
				"role":    role,
				"content": text,
			})
		}
		out = append(out, toolResults...)
	}
	if len(out) == 0 {
		return []map[string]any{{"role": "user", "content": ""}}
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

func anthropicContentToChatParts(role string, raw any) (string, []map[string]any, []map[string]any) {
	textParts := make([]string, 0, 4)
	toolCalls := make([]map[string]any, 0, 2)
	toolResults := make([]map[string]any, 0, 2)
	switch v := raw.(type) {
	case string:
		return v, nil, nil
	case []any:
		for idx, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch stringValue(m["type"]) {
			case "text", "input_text", "output_text":
				if t := stringValue(m["text"]); t != "" {
					textParts = append(textParts, t)
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
				toolCalls = append(toolCalls, map[string]any{
					"id":   id,
					"type": "function",
					"function": map[string]any{
						"name":      name,
						"arguments": args,
					},
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
				toolResults = append(toolResults, map[string]any{
					"role":         "tool",
					"tool_call_id": toolCallID,
					"content":      content,
				})
			}
		}
	}
	return strings.Join(textParts, "\n"), toolCalls, toolResults
}

func anthropicToolsToChatTools(raw any) []map[string]any {
	items, _ := raw.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(m["name"])
		if name == "" {
			continue
		}
		fn := map[string]any{
			"name": name,
		}
		if desc := stringValue(m["description"]); desc != "" {
			fn["description"] = desc
		}
		if schema, ok := m["input_schema"]; ok {
			fn["parameters"] = schema
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
		})
	}
	return out
}

func anthropicToolChoiceToChatToolChoice(raw any) (any, bool) {
	if raw == nil {
		return nil, false
	}
	switch v := raw.(type) {
	case string:
		switch v {
		case "auto":
			return "auto", true
		case "any":
			return "required", true
		case "none":
			return "none", true
		default:
			return nil, false
		}
	case map[string]any:
		switch stringValue(v["type"]) {
		case "auto":
			return "auto", true
		case "any":
			return "required", true
		case "tool":
			name := stringValue(v["name"])
			if name == "" {
				return nil, false
			}
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": name,
				},
			}, true
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func chatToAnthropicMessage(chatResp map[string]any, requestedModel string) map[string]any {
	id := stringValue(chatResp["id"])
	if id == "" {
		id = fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	model := stringValue(chatResp["model"])
	if model == "" {
		model = requestedModel
	}
	text := extractChatText(chatResp)
	toolCalls := extractChatToolCalls(chatResp)
	content := make([]map[string]any, 0, 1+len(toolCalls))
	if text != "" {
		content = append(content, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	for i, tc := range toolCalls {
		input := map[string]any{}
		if strings.TrimSpace(tc.Arguments) != "" {
			_ = json.Unmarshal([]byte(tc.Arguments), &input)
		}
		id := tc.CallID
		if id == "" {
			id = tc.ID
		}
		if id == "" {
			id = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), i)
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    id,
			"name":  tc.Name,
			"input": input,
		})
	}
	stopReason := chatStopReason(chatResp, len(toolCalls) > 0)
	usage := mapValue(chatResp["usage"])
	return map[string]any{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  intFromAny(usage["prompt_tokens"]),
			"output_tokens": intFromAny(usage["completion_tokens"]),
		},
	}
}

func chatStopReason(chatResp map[string]any, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_use"
	}
	choices, _ := chatResp["choices"].([]any)
	if len(choices) == 0 {
		return "end_turn"
	}
	c0, _ := choices[0].(map[string]any)
	switch stringValue(c0["finish_reason"]) {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}
