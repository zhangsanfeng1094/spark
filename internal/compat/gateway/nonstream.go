package gateway

import (
	"time"

	"spark/internal/compat/client/codex"
	anthropic_messages_target "spark/internal/compat/target/anthropic_messages"
	openai_chat_target "spark/internal/compat/target/openai_chat"
	"spark/internal/usage"
)

func CodexResponsesFromOpenAIChatResponse(resp map[string]any) map[string]any {
	irResp := openai_chat_target.ChatResponse(resp)
	usage.RecordIR(irResp.Usage, irResp.Model, false, time.Now().UTC())
	return codex.ResponsesClientResponse(irResp)
}

func CodexResponsesFromAnthropicMessagesResponse(resp map[string]any) map[string]any {
	irResp := anthropic_messages_target.MessageResponse(resp)
	usage.RecordIR(irResp.Usage, irResp.Model, false, time.Now().UTC())
	return codex.ResponsesClientResponse(irResp)
}
