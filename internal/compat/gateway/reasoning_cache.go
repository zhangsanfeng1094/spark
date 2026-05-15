package gateway

import (
	"strings"
	"sync"
)

type ChatReasoningStats struct {
	MessageCount               int
	AssistantReasoningMessages int
	AssistantReasoningChars    int
	AssistantToolMessages      int
	TopLevelThinking           bool
}

type ReasoningCache struct {
	mu         sync.Mutex
	byToolCall map[string]string
}

func ChatReasoningSummary(chatReq map[string]any) ChatReasoningStats {
	stats := ChatReasoningStats{}
	if chatReq == nil {
		return stats
	}
	_, stats.TopLevelThinking = chatReq["thinking"]
	for _, msg := range ChatMessages(chatReq["messages"]) {
		stats.MessageCount++
		if stringValue(msg["role"]) != "assistant" {
			continue
		}
		if len(ToolCallIDsFromChatMessage(msg)) > 0 {
			stats.AssistantToolMessages++
		}
		if reasoning := stringValue(msg["reasoning_content"]); reasoning != "" {
			stats.AssistantReasoningMessages++
			stats.AssistantReasoningChars += len(reasoning)
		}
	}
	return stats
}

func (c *ReasoningCache) ApplyToChatRequest(upstreamBase string, chatReq map[string]any) {
	if chatReq == nil {
		return
	}
	passReasoningContent := ShouldPassReasoningContentForTarget(upstreamBase, stringValue(chatReq["model"]))
	for _, msg := range ChatMessages(chatReq["messages"]) {
		if stringValue(msg["role"]) != "assistant" {
			continue
		}
		ids := ToolCallIDsFromChatMessage(msg)
		if len(ids) == 0 {
			continue
		}
		if reasoning, ok := c.ReasoningForToolCallIDs(ids); ok {
			msg["reasoning_content"] = reasoning
			continue
		}
		if !passReasoningContent {
			delete(msg, "reasoning_content")
			continue
		}
		if _, ok := msg["reasoning_content"]; !ok {
			msg["reasoning_content"] = ""
		}
	}
}

func (c *ReasoningCache) RememberForToolCalls(calls []ChatToolCall, reasoning string) {
	if len(calls) == 0 {
		return
	}
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		id := call.CallID
		if id == "" {
			id = call.ID
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	c.RememberForToolCallIDs(ids, reasoning)
}

func (c *ReasoningCache) RememberForToolCallIDs(ids []string, reasoning string) {
	if c == nil || reasoning == "" || len(ids) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byToolCall == nil {
		c.byToolCall = make(map[string]string, len(ids))
	}
	for _, id := range ids {
		if id != "" {
			c.byToolCall[id] = reasoning
		}
	}
}

func (c *ReasoningCache) ReasoningForToolCallIDs(ids []string) (string, bool) {
	if c == nil || len(ids) == 0 {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		if reasoning := c.byToolCall[id]; reasoning != "" {
			return reasoning, true
		}
	}
	return "", false
}

func ShouldPassReasoningContentForTarget(upstreamBase, modelName string) bool {
	target := strings.ToLower(upstreamBase + " " + modelName)
	return strings.Contains(target, "xiaomimimo") ||
		strings.Contains(target, "mimo") ||
		strings.Contains(target, "deepseek")
}

func ChatMessages(raw any) []map[string]any {
	switch items := raw.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if msg, ok := item.(map[string]any); ok {
				out = append(out, msg)
			}
		}
		return out
	default:
		return nil
	}
}

func ToolCallIDsFromChatMessage(msg map[string]any) []string {
	switch items := msg["tool_calls"].(type) {
	case []map[string]any:
		ids := make([]string, 0, len(items))
		for _, item := range items {
			if id := ToolCallIDFromMap(item); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	case []any:
		ids := make([]string, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				if id := ToolCallIDFromMap(m); id != "" {
					ids = append(ids, id)
				}
			}
		}
		return ids
	default:
		return nil
	}
}

func ToolCallIDFromMap(m map[string]any) string {
	if id := stringValue(m["id"]); id != "" {
		return id
	}
	return stringValue(m["call_id"])
}
