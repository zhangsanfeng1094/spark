package integrations

import "spark/internal/compat/gateway"

func (p *anthropicCompatProxy) applyReasoningContent(chatReq map[string]any) {
	if p == nil {
		return
	}
	p.reasoningCache.ApplyToChatRequest(p.upstreamBase, chatReq)
}

func (p *anthropicCompatProxy) rememberReasoningForToolCallIDs(ids []string, reasoning string) {
	if p == nil {
		return
	}
	p.reasoningCache.RememberForToolCallIDs(ids, reasoning)
}

func (p *responsesCompatProxy) applyReasoningContent(chatReq map[string]any) {
	if p == nil {
		return
	}
	var cache gateway.ReasoningCache
	cache.ApplyToChatRequest(p.upstreamBase, chatReq)
}
