package gateway

import (
	"spark/internal/compat/client/codex"
	anthropic_messages_target "spark/internal/compat/target/anthropic_messages"
)

func CodexResponsesToAnthropicMessagesRequest(req map[string]any) map[string]any {
	irReq := codex.ResponsesInbound(req)
	return anthropic_messages_target.MessagesOutbound{}.BuildRequest(irReq)
}
