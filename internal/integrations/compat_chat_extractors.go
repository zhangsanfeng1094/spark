package integrations

import (
	"fmt"
	"time"

	openaitarget "spark/internal/compat/target/openai"
	"spark/internal/compatir"
)

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
	irResp := openaitarget.ChatResponse(resp)
	out := make([]chatToolCall, 0, len(irResp.Output))
	for i, block := range irResp.Output {
		if block.Type != compatir.BlockToolCall || block.ToolCall == nil || block.ToolCall.Name == "" {
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
		out = append(out, chatToolCall{
			ID:        id,
			CallID:    id,
			Name:      block.ToolCall.Name,
			Arguments: args,
		})
	}
	return out
}

func extractChatToolCallDeltas(chunk map[string]any) []chatToolCallDelta {
	out := make([]chatToolCallDelta, 0, 2)
	for _, event := range openaitarget.ChatStreamEvents(chunk) {
		if event.Type != compatir.StreamEventContentDelta ||
			event.Delta.Type != compatir.BlockToolCall ||
			event.Delta.ToolCall == nil {
			continue
		}
		out = append(out, chatToolCallDelta{
			Index:          streamToolIndex(event.Delta.Raw, len(out)),
			CallID:         event.Delta.ToolCall.ID,
			Name:           event.Delta.ToolCall.Name,
			ArgumentsDelta: event.Delta.ToolCall.Arguments,
		})
	}
	return out
}

func extractChatText(resp map[string]any) string {
	irResp := openaitarget.ChatResponse(resp)
	for _, block := range irResp.Output {
		if block.Type == compatir.BlockText && block.Text != "" {
			return block.Text
		}
	}
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	c0, _ := choices[0].(map[string]any)
	return normalizeMessageContent(c0["text"])
}

func extractChatDelta(chunk map[string]any) string {
	for _, event := range openaitarget.ChatStreamEvents(chunk) {
		if event.Type == compatir.StreamEventContentDelta && event.Delta.Type == compatir.BlockText && event.Delta.Text != "" {
			return event.Delta.Text
		}
	}
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	c0, _ := choices[0].(map[string]any)
	if delta, _ := c0["delta"].(map[string]any); delta != nil {
		if text := normalizeMessageContent(delta["text"]); text != "" {
			return text
		}
	}
	return normalizeMessageContent(c0["text"])
}

func extractChatReasoningDelta(chunk map[string]any) string {
	text, _ := extractChatReasoningDeltaValue(chunk)
	return text
}

func extractChatReasoningDeltaValue(chunk map[string]any) (string, bool) {
	for _, event := range openaitarget.ChatStreamEvents(chunk) {
		if event.Type == compatir.StreamEventContentDelta &&
			event.Delta.Type == compatir.BlockReasoning &&
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
		if text, ok := extractReasoningTextFromMap(delta); ok {
			return text, true
		}
	}
	return extractChatReasoningTextValue(chunk)
}

func extractChatReasoningText(resp map[string]any) string {
	text, _ := extractChatReasoningTextValue(resp)
	return text
}

func extractChatReasoningTextValue(resp map[string]any) (string, bool) {
	irResp := openaitarget.ChatResponse(resp)
	for _, block := range irResp.Output {
		if block.Type == compatir.BlockReasoning && block.Reasoning != nil {
			return block.Reasoning.Text, true
		}
	}
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return "", false
	}
	c0, _ := choices[0].(map[string]any)
	return extractReasoningTextFromMap(c0)
}

func extractReasoningTextFromMap(m map[string]any) (string, bool) {
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if raw, ok := m[key]; ok {
			return normalizeMessageContent(raw), true
		}
	}
	return "", false
}

func streamToolIndex(raw map[string]any, fallback int) int {
	switch idx := raw["index"].(type) {
	case int:
		return idx
	case float64:
		return int(idx)
	}
	return fallback
}
