package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"spark/internal/compat/ir"
	openai_chat_target "spark/internal/compat/target/openai_chat"
)

type ChatToolCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
}

type ChatToolCallDelta struct {
	Index          int
	CallID         string
	Name           string
	ArgumentsDelta string
}

func ExtractChatToolCalls(resp map[string]any) []ChatToolCall {
	irResp := openai_chat_target.ChatResponse(resp)
	out := make([]ChatToolCall, 0, len(irResp.Output))
	for i, block := range irResp.Output {
		if block.Type != ir.BlockToolCall || block.ToolCall == nil || block.ToolCall.Name == "" {
			continue
		}
		id := block.ToolCall.ID
		if id == "" {
			id = fmt.Sprintf("fc_%d_%d", time.Now().UnixNano(), i)
		}
		args := block.ToolCall.Arguments
		if args == "" {
			args = "{}"
		}
		out = append(out, ChatToolCall{
			ID:        id,
			CallID:    id,
			Name:      block.ToolCall.Name,
			Arguments: args,
		})
	}
	return out
}

func ExtractChatToolCallDeltas(chunk map[string]any) []ChatToolCallDelta {
	out := make([]ChatToolCallDelta, 0, 2)
	for _, event := range openai_chat_target.ChatStreamEvents(chunk) {
		if event.Type != ir.StreamEventContentDelta ||
			event.Delta.Type != ir.BlockToolCall ||
			event.Delta.ToolCall == nil {
			continue
		}
		out = append(out, ChatToolCallDelta{
			Index:          StreamToolIndex(event.Delta.Raw, len(out)),
			CallID:         event.Delta.ToolCall.ID,
			Name:           event.Delta.ToolCall.Name,
			ArgumentsDelta: event.Delta.ToolCall.Arguments,
		})
	}
	return out
}

func ExtractChatText(resp map[string]any) string {
	irResp := openai_chat_target.ChatResponse(resp)
	for _, block := range irResp.Output {
		if block.Type == ir.BlockText && block.Text != "" {
			return block.Text
		}
	}
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	c0, _ := choices[0].(map[string]any)
	return NormalizeMessageContent(c0["text"])
}

func ExtractChatDelta(chunk map[string]any) string {
	for _, event := range openai_chat_target.ChatStreamEvents(chunk) {
		if event.Type == ir.StreamEventContentDelta && event.Delta.Type == ir.BlockText && event.Delta.Text != "" {
			return event.Delta.Text
		}
	}
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	c0, _ := choices[0].(map[string]any)
	if delta, _ := c0["delta"].(map[string]any); delta != nil {
		if text := NormalizeMessageContent(delta["text"]); text != "" {
			return text
		}
	}
	return NormalizeMessageContent(c0["text"])
}

func ExtractChatReasoningDelta(chunk map[string]any) string {
	text, _ := ExtractChatReasoningDeltaValue(chunk)
	return text
}

func ExtractChatReasoningDeltaValue(chunk map[string]any) (string, bool) {
	for _, event := range openai_chat_target.ChatStreamEvents(chunk) {
		if event.Type == ir.StreamEventContentDelta &&
			event.Delta.Type == ir.BlockReasoning &&
			event.Delta.Reasoning != nil {
			return event.Delta.Reasoning.Text, true
		}
	}
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return "", false
	}
	c0, _ := choices[0].(map[string]any)
	if delta, _ := c0["delta"].(map[string]any); delta != nil {
		if text, ok := ExtractReasoningTextFromMap(delta); ok {
			return text, true
		}
	}
	return ExtractChatReasoningTextValue(chunk)
}

func ExtractChatReasoningText(resp map[string]any) string {
	text, _ := ExtractChatReasoningTextValue(resp)
	return text
}

func ExtractChatReasoningTextValue(resp map[string]any) (string, bool) {
	irResp := openai_chat_target.ChatResponse(resp)
	for _, block := range irResp.Output {
		if block.Type == ir.BlockReasoning && block.Reasoning != nil {
			return block.Reasoning.Text, true
		}
	}
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return "", false
	}
	c0, _ := choices[0].(map[string]any)
	return ExtractReasoningTextFromMap(c0)
}

func ExtractReasoningTextFromMap(m map[string]any) (string, bool) {
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if raw, ok := m[key]; ok {
			return NormalizeMessageContent(raw), true
		}
	}
	return "", false
}

func StreamToolIndex(raw map[string]any, fallback int) int {
	switch idx := raw["index"].(type) {
	case int:
		return idx
	case float64:
		return int(idx)
	}
	return fallback
}

func NormalizeMessageContent(raw any) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case json.RawMessage:
		return strings.TrimSpace(string(v))
	case map[string]any:
		itemType := stringValue(v["type"])
		switch itemType {
		case "", "input_text", "output_text", "text":
			if text := stringValue(v["text"]); text != "" {
				return text
			}
		}
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		if text := stringValue(v["content"]); text != "" {
			return text
		}
		return ""
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemType := stringValue(m["type"])
			switch itemType {
			case "input_text", "output_text", "text":
				if text := stringValue(m["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}
