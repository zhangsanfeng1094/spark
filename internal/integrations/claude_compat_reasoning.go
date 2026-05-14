package integrations

import "strings"

func (p *anthropicCompatProxy) applyReasoningContent(chatReq map[string]any) {
	if p == nil || chatReq == nil {
		return
	}
	passReasoningContent := p.shouldPassReasoningContent(chatReq)
	for _, msg := range chatMessages(chatReq["messages"]) {
		if stringValue(msg["role"]) != "assistant" {
			continue
		}
		ids := toolCallIDsFromChatMessage(msg)
		if len(ids) == 0 {
			continue
		}
		if reasoning, ok := p.reasoningForToolCallIDs(ids); ok {
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

func (p *anthropicCompatProxy) rememberReasoningForToolCalls(calls []chatToolCall, reasoning string) {
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
	p.rememberReasoningForToolCallIDs(ids, reasoning)
}

func (p *anthropicCompatProxy) rememberReasoningForToolCallIDs(ids []string, reasoning string) {
	if p == nil || reasoning == "" || len(ids) == 0 {
		return
	}
	p.reasoningMu.Lock()
	defer p.reasoningMu.Unlock()
	if p.reasoningByToolCall == nil {
		p.reasoningByToolCall = make(map[string]string, len(ids))
	}
	for _, id := range ids {
		if id != "" {
			p.reasoningByToolCall[id] = reasoning
		}
	}
}

func (p *anthropicCompatProxy) reasoningForToolCallIDs(ids []string) (string, bool) {
	if p == nil || len(ids) == 0 {
		return "", false
	}
	p.reasoningMu.Lock()
	defer p.reasoningMu.Unlock()
	for _, id := range ids {
		if reasoning := p.reasoningByToolCall[id]; reasoning != "" {
			return reasoning, true
		}
	}
	return "", false
}

func (p *anthropicCompatProxy) shouldPassReasoningContent(chatReq map[string]any) bool {
	if p == nil {
		return false
	}
	return shouldPassReasoningContentForTarget(p.upstreamBase, stringValue(chatReq["model"]))
}

func (p *responsesCompatProxy) applyReasoningContent(chatReq map[string]any) {
	if p == nil || chatReq == nil {
		return
	}
	passReasoningContent := shouldPassReasoningContentForTarget(p.upstreamBase, stringValue(chatReq["model"]))
	for _, msg := range chatMessages(chatReq["messages"]) {
		if stringValue(msg["role"]) != "assistant" {
			continue
		}
		if len(toolCallIDsFromChatMessage(msg)) == 0 {
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

func shouldPassReasoningContentForTarget(upstreamBase, modelName string) bool {
	base := strings.ToLower(upstreamBase)
	model := strings.ToLower(modelName)
	return strings.Contains(base, "xiaomimimo") ||
		strings.Contains(base, "mimo") ||
		strings.Contains(base, "deepseek") ||
		strings.Contains(model, "mimo") ||
		strings.Contains(model, "deepseek")
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

func toolCallIDsFromStreamState(toolStates map[int]*toolStreamState, toolOrder []int) []string {
	ids := make([]string, 0, len(toolOrder))
	for _, idx := range toolOrder {
		if st := toolStates[idx]; st != nil && st.id != "" {
			ids = append(ids, st.id)
		}
	}
	return ids
}
