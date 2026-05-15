package gateway

import (
	"spark/internal/compat/client/codex"
	openai_chat_target "spark/internal/compat/target/openai_chat"
)

func CodexResponsesFromOpenAIChatResponse(resp map[string]any) map[string]any {
	return codex.ResponsesClientResponse(openai_chat_target.ChatResponse(resp))
}
