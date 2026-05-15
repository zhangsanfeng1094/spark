package gateway

import (
	"spark/internal/compat/client/codex"
	"spark/internal/compat/policy"
	openai_chat_target "spark/internal/compat/target/openai_chat"
)

func CodexResponsesToOpenAIChatRequest(req map[string]any, reasoning policy.ReasoningPolicy) map[string]any {
	irReq := codex.ResponsesInbound(req)
	return openai_chat_target.ChatOutbound{
		Reasoning: reasoning,
	}.BuildRequest(irReq)
}

func CodexResponsesToOpenAIChatRequestForTarget(req map[string]any, upstreamBase string) map[string]any {
	irReq := codex.ResponsesInbound(req)
	return openai_chat_target.ChatOutbound{
		Reasoning: policy.OpenAIChatReasoningPolicy(upstreamBase, irReq.Model),
	}.BuildRequest(irReq)
}
