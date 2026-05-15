package integrations

import "spark/internal/compat/gateway"

type chatToolCall = gateway.ChatToolCall
type chatToolCallDelta = gateway.ChatToolCallDelta

func extractChatToolCalls(resp map[string]any) []chatToolCall {
	return gateway.ExtractChatToolCalls(resp)
}

func extractChatToolCallDeltas(chunk map[string]any) []chatToolCallDelta {
	return gateway.ExtractChatToolCallDeltas(chunk)
}

func extractChatText(resp map[string]any) string {
	return gateway.ExtractChatText(resp)
}

func extractChatDelta(chunk map[string]any) string {
	return gateway.ExtractChatDelta(chunk)
}

func extractChatReasoningDelta(chunk map[string]any) string {
	return gateway.ExtractChatReasoningDelta(chunk)
}

func extractChatReasoningDeltaValue(chunk map[string]any) (string, bool) {
	return gateway.ExtractChatReasoningDeltaValue(chunk)
}

func extractChatReasoningText(resp map[string]any) string {
	return gateway.ExtractChatReasoningText(resp)
}

func extractChatReasoningTextValue(resp map[string]any) (string, bool) {
	return gateway.ExtractChatReasoningTextValue(resp)
}
