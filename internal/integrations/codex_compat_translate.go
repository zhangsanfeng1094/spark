package integrations

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func responsesToChatCompletions(req map[string]any) map[string]any {
	model := stringValue(req["model"])
	if model == "" {
		model = "unknown"
	}
	messages := responsesInputToMessages(req["input"])
	if len(messages) == 0 {
		messages = []map[string]any{{"role": "user", "content": ""}}
	}
	out := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   boolValue(req["stream"]),
	}
	if max, ok := intValue(req["max_output_tokens"]); ok {
		out["max_tokens"] = max
	}
	if v, ok := req["temperature"]; ok {
		out["temperature"] = v
	}
	if v, ok := req["top_p"]; ok {
		out["top_p"] = v
	}
	if v, ok := req["stop"]; ok {
		out["stop"] = v
	}
	if v, ok := req["tools"]; ok {
		if tools := responsesToolsToChatTools(v); len(tools) > 0 {
			out["tools"] = tools
		}
	}
	if v, ok := req["tool_choice"]; ok {
		if tc, ok := responsesToolChoiceToChatToolChoice(v); ok {
			out["tool_choice"] = tc
		}
	}
	return out
}

func responsesToolsToChatTools(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if stringValue(m["type"]) != "function" {
			// chat/completions does not accept most Responses built-in tools
			continue
		}
		fn := map[string]any{}
		if name := stringValue(m["name"]); name != "" {
			fn["name"] = name
		}
		if desc := stringValue(m["description"]); desc != "" {
			fn["description"] = desc
		}
		if params, ok := m["parameters"]; ok {
			fn["parameters"] = params
		}
		if len(fn) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
		})
	}
	return out
}

func responsesToolChoiceToChatToolChoice(raw any) (any, bool) {
	switch v := raw.(type) {
	case string:
		switch v {
		case "auto", "none", "required":
			return v, true
		default:
			return nil, false
		}
	case map[string]any:
		if stringValue(v["type"]) != "function" {
			return nil, false
		}
		name := stringValue(v["name"])
		if name == "" {
			if fn, ok := v["function"].(map[string]any); ok {
				name = stringValue(fn["name"])
			}
		}
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
}

func responsesInputToMessages(input any) []map[string]any {
	if input == nil {
		return nil
	}
	switch v := input.(type) {
	case string:
		return []map[string]any{{"role": "user", "content": v}}
	case []any:
		out := make([]map[string]any, 0, len(v))
		type pendingToolCall struct {
			Name      string
			Arguments string
		}
		pendingCalls := map[string]pendingToolCall{}
		appendSyntheticAssistantToolCall := func(callID, name, arguments string) {
			if callID == "" {
				return
			}
			if name == "" {
				name = "unknown_tool"
			}
			if arguments == "" {
				arguments = "{}"
			}
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{
						"id":   callID,
						"type": "function",
						"function": map[string]any{
							"name":      name,
							"arguments": arguments,
						},
					},
				},
			})
		}
		for _, item := range v {
			msg, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemType := stringValue(msg["type"])
			switch itemType {
			case "function_call_output":
				toolCallID := stringValue(msg["call_id"])
				if toolCallID == "" {
					toolCallID = stringValue(msg["tool_call_id"])
				}
				if toolCallID == "" {
					continue
				}
				output := normalizeMessageContent(msg["output"])
				if output == "" {
					output = normalizeMessageContent(msg["content"])
				}
				if output == "" {
					output = "{}"
				}
				call := pendingCalls[toolCallID]
				appendSyntheticAssistantToolCall(toolCallID, call.Name, call.Arguments)
				out = append(out, map[string]any{
					"role":         "tool",
					"tool_call_id": toolCallID,
					"content":      output,
				})
				delete(pendingCalls, toolCallID)
				continue
			case "function_call":
				toolCallID := stringValue(msg["call_id"])
				if toolCallID == "" {
					toolCallID = stringValue(msg["id"])
				}
				if toolCallID == "" {
					continue
				}
				name := stringValue(msg["name"])
				arguments := stringValue(msg["arguments"])
				pendingCalls[toolCallID] = pendingToolCall{
					Name:      name,
					Arguments: arguments,
				}
				continue
			}
			role := stringValue(msg["role"])
			if role == "" {
				role = "user"
			}
			if role == "developer" {
				role = "system"
			}
			if role != "system" && role != "user" && role != "assistant" && role != "tool" {
				role = "user"
			}
			if role == "assistant" {
				toolCallsRaw, ok := msg["tool_calls"].([]any)
				if ok && len(toolCallsRaw) > 0 {
					toolCalls := make([]map[string]any, 0, len(toolCallsRaw))
					for _, item := range toolCallsRaw {
						tc, ok := item.(map[string]any)
						if !ok {
							continue
						}
						if stringValue(tc["type"]) != "" && stringValue(tc["type"]) != "function" {
							continue
						}
						tcID := stringValue(tc["id"])
						if tcID == "" {
							tcID = fmt.Sprintf("call_%d", time.Now().UnixNano())
						}
						fn, _ := tc["function"].(map[string]any)
						name := stringValue(fn["name"])
						if name == "" {
							continue
						}
						args := stringValue(fn["arguments"])
						if args == "" {
							args = "{}"
						}
						pendingCalls[tcID] = pendingToolCall{
							Name:      name,
							Arguments: args,
						}
						toolCalls = append(toolCalls, map[string]any{
							"id":   tcID,
							"type": "function",
							"function": map[string]any{
								"name":      name,
								"arguments": args,
							},
						})
					}
					if len(toolCalls) > 0 {
						// Do not emit standalone assistant tool_calls here.
						// We'll synthesize assistant tool_calls directly before each tool message.
						continue
					}
				}
			}
			content := normalizeMessageContent(msg["content"])
			if content == "" {
				if t := stringValue(msg["text"]); t != "" {
					content = t
				}
			}
			if role == "tool" {
				toolCallID := stringValue(msg["tool_call_id"])
				if toolCallID == "" {
					toolCallID = stringValue(msg["call_id"])
				}
				if toolCallID == "" {
					continue
				}
				if content == "" {
					content = "{}"
				}
				call := pendingCalls[toolCallID]
				appendSyntheticAssistantToolCall(toolCallID, call.Name, call.Arguments)
				out = append(out, map[string]any{
					"role":         "tool",
					"tool_call_id": toolCallID,
					"content":      content,
				})
				delete(pendingCalls, toolCallID)
				continue
			}
			if content == "" {
				continue
			}
			out = append(out, map[string]any{
				"role":    role,
				"content": content,
			})
		}
		return out
	default:
		return []map[string]any{{"role": "user", "content": fmt.Sprint(v)}}
	}
}

func normalizeMessageContent(raw any) string {
	if raw == nil {
		return ""
	}
	switch c := raw.(type) {
	case string:
		return c
	case json.RawMessage:
		return strings.TrimSpace(string(c))
	case map[string]any:
		itemType := stringValue(c["type"])
		switch itemType {
		case "", "input_text", "output_text", "text":
			if t := stringValue(c["text"]); t != "" {
				return t
			}
		}
		if data, err := json.Marshal(c); err == nil {
			return string(data)
		}
		if t := stringValue(c["content"]); t != "" {
			return t
		}
		return ""
	case []any:
		parts := make([]string, 0, len(c))
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemType := stringValue(m["type"])
			switch itemType {
			case "input_text", "output_text", "text":
				if t := stringValue(m["text"]); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	case []byte:
		return strings.TrimSpace(string(c))
	default:
		if data, err := json.Marshal(c); err == nil {
			return string(data)
		}
		return fmt.Sprint(c)
	}
}

type chatToolCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
}

type chatToolCallDelta struct {
	Index          int
	CallID         string
	Name           string
	ArgumentsDelta string
}

func extractChatToolCalls(resp map[string]any) []chatToolCall {
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil
	}
	c0, ok := choices[0].(map[string]any)
	if !ok {
		return nil
	}
	msg, ok := c0["message"].(map[string]any)
	if !ok {
		return nil
	}
	items, ok := msg["tool_calls"].([]any)
	if !ok {
		return nil
	}
	out := make([]chatToolCall, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if stringValue(m["type"]) != "" && stringValue(m["type"]) != "function" {
			continue
		}
		id := stringValue(m["id"])
		if id == "" {
			id = fmt.Sprintf("fc_%d_%d", time.Now().UnixNano(), i)
		}
		fn, _ := m["function"].(map[string]any)
		name := stringValue(fn["name"])
		if name == "" {
			continue
		}
		args := stringValue(fn["arguments"])
		if args == "" {
			args = "{}"
		}
		out = append(out, chatToolCall{
			ID:        id,
			CallID:    id,
			Name:      name,
			Arguments: args,
		})
	}
	return out
}

func extractChatToolCallDeltas(chunk map[string]any) []chatToolCallDelta {
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil
	}
	c0, ok := choices[0].(map[string]any)
	if !ok {
		return nil
	}
	delta, ok := c0["delta"].(map[string]any)
	if !ok {
		return nil
	}
	rawCalls, ok := delta["tool_calls"].([]any)
	if !ok {
		return nil
	}
	out := make([]chatToolCallDelta, 0, len(rawCalls))
	for _, item := range rawCalls {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		index := -1
		switch v := m["index"].(type) {
		case float64:
			index = int(v)
		case int:
			index = v
		}
		fn, _ := m["function"].(map[string]any)
		out = append(out, chatToolCallDelta{
			Index:          index,
			CallID:         stringValue(m["id"]),
			Name:           stringValue(fn["name"]),
			ArgumentsDelta: stringValue(fn["arguments"]),
		})
	}
	return out
}

func extractChatText(resp map[string]any) string {
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	c0, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	msg, ok := c0["message"].(map[string]any)
	if ok {
		if text := normalizeMessageContent(msg["content"]); text != "" {
			return text
		}
	}
	if text := normalizeMessageContent(c0["text"]); text != "" {
		return text
	}
	return ""
}

func extractChatDelta(chunk map[string]any) string {
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	c0, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	delta, ok := c0["delta"].(map[string]any)
	if ok {
		if text := normalizeMessageContent(delta["content"]); text != "" {
			return text
		}
		if text := normalizeMessageContent(delta["text"]); text != "" {
			return text
		}
	}
	if text := normalizeMessageContent(c0["text"]); text != "" {
		return text
	}
	return ""
}

func extractChatReasoningDelta(chunk map[string]any) string {
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	c0, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	delta, ok := c0["delta"].(map[string]any)
	if !ok {
		return ""
	}
	if text := normalizeMessageContent(delta["reasoning"]); text != "" {
		return text
	}
	return ""
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}
