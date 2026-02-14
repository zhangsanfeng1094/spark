package integrations

import (
	"context"
	"net/http"
)

// RequestTranslator maps an external API request to OpenAI chat/completions.
type RequestTranslator interface {
	ToChat(req map[string]any) (map[string]any, error)
}

// NonStreamResponseTranslator maps a chat/completions response back to
// the external API format.
type NonStreamResponseTranslator interface {
	FromChat(chatResp map[string]any, requestedModel string) (map[string]any, error)
}

// StreamTranslator exposes chunk-level extraction primitives used by stream
// adapters. This keeps stream state machines independent from provider shape.
type StreamTranslator interface {
	TextDelta(chunk map[string]any) string
	ReasoningDelta(chunk map[string]any) string
	ToolCallDeltas(chunk map[string]any) []chatToolCallDelta
	ToolCalls(chunk map[string]any) []chatToolCall
	FullText(resp map[string]any) string
}

// ChatExecutor sends a chat/completions request to upstream and returns the
// final upstream response after provider-specific retry policy.
type ChatExecutor interface {
	Do(ctx context.Context, chatReq map[string]any) (*http.Response, error)
}
