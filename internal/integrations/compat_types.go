package integrations

import (
	"context"
	"net/http"
)

// RequestTranslator maps an external API request to OpenAI chat/completions.
type RequestTranslator interface {
	ToChat(req map[string]any) (map[string]any, error)
}

// ChatExecutor sends a chat/completions request to upstream and returns the
// final upstream response after provider-specific retry policy.
type ChatExecutor interface {
	Do(ctx context.Context, chatReq map[string]any) (*http.Response, error)
}
