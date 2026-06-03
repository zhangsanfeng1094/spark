package reasoning

import (
	"sync"

	"spark/internal/compat/policy"
)

type chatReasoningStats struct {
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

type ChatReasoningAdapter struct {
	UpstreamBase string
	Cache        *ReasoningCache
}

func (a ChatReasoningAdapter) ApplyToChatRequest(chatReq map[string]any) {
	if a.Cache != nil {
		a.Cache.ApplyToChatRequest(a.UpstreamBase, chatReq)
		return
	}
	var cache ReasoningCache
	cache.ApplyToChatRequest(a.UpstreamBase, chatReq)
}

func (a ChatReasoningAdapter) RememberForToolCallIDs(ids []string, reasoning string) {
	if a.Cache == nil {
		return
	}
	a.Cache.RememberForToolCallIDs(ids, reasoning)
}

func chatReasoningSummary(chatReq map[string]any) chatReasoningStats {
	stats := chatReasoningStats{}
	if chatReq == nil {
		return stats
	}
	_, stats.TopLevelThinking = chatReq["thinking"]
	for _, msg := range chatMessages(chatReq["messages"]) {
		stats.MessageCount++
		if stringValue(msg["role"]) != "assistant" {
			continue
		}
		if len(toolCallIDsFromChatMessage(msg)) > 0 {
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
	passReasoningContent := policy.RequiresOpenAIChatReasoningEcho(upstreamBase, stringValue(chatReq["model"]))
	for _, msg := range chatMessages(chatReq["messages"]) {
		if stringValue(msg["role"]) != "assistant" {
			continue
		}
		ids := toolCallIDsFromChatMessage(msg)
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

func chatMessages(raw any) []map[string]any {
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

func toolCallIDsFromChatMessage(msg map[string]any) []string {
	switch items := msg["tool_calls"].(type) {
	case []map[string]any:
		ids := make([]string, 0, len(items))
		for _, item := range items {
			if id := toolCallIDFromMap(item); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	case []any:
		ids := make([]string, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				if id := toolCallIDFromMap(m); id != "" {
					ids = append(ids, id)
				}
			}
		}
		return ids
	default:
		return nil
	}
}

func toolCallIDFromMap(m map[string]any) string {
	if id := stringValue(m["id"]); id != "" {
		return id
	}
	return stringValue(m["call_id"])
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
