package gateway

import (
	anthropicadapter "spark/internal/compat/client/anthropic_messages"
	"spark/internal/compat/policy"
	openai_chat_target "spark/internal/compat/target/openai_chat"
)

type CodexResponsesTranslator struct {
	Reasoning policy.ReasoningPolicy
}

func (t CodexResponsesTranslator) ToChat(req map[string]any) (map[string]any, error) {
	reasoning := t.Reasoning
	if reasoning.Mode == "" && reasoning.Field == "" && !reasoning.AllowReasoningEffort && !reasoning.AllowThinking {
		reasoning = policy.PreserveReasoningContent()
	}
	return CodexResponsesToOpenAIChatRequest(req, reasoning), nil
}

type AnthropicMessagesTranslator struct{}

func (AnthropicMessagesTranslator) ToChat(req map[string]any) (map[string]any, error) {
	irReq := anthropicadapter.MessagesInbound(req)
	return openai_chat_target.ChatOutbound{
		Reasoning: policy.PreserveReasoningContent(),
	}.BuildRequest(irReq), nil
}
