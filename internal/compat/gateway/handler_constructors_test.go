package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"spark/internal/compat/gateway/core"
	reasoningfeature "spark/internal/compat/gateway/features/reasoning"
)

func TestNewCodexResponsesHandlerDefaultsToOpenAIChatRoute(t *testing.T) {
	handler := NewCodexResponsesHandler(CodexResponsesOptions{})

	if handler.mode != ResponsesModeChatCompletionsOnly {
		t.Fatalf("unexpected default mode: %q", handler.mode)
	}
	if handler.route.Client != core.ClientCodexResponses || handler.route.Target != core.TargetOpenAIChat {
		t.Fatalf("unexpected default route: %#v", handler.route)
	}
}

func TestNewCodexResponsesHandlerSelectsAnthropicMessagesRoute(t *testing.T) {
	handler := NewCodexResponsesHandler(CodexResponsesOptions{
		Mode: ResponsesModeAnthropicMessagesOnly,
	})

	if handler.route.Client != core.ClientCodexResponses || handler.route.Target != core.TargetAnthropicMessages {
		t.Fatalf("unexpected route: %#v", handler.route)
	}
}

func TestCodexResponsesHandlerWithoutCallbacksDoesNotPanic(t *testing.T) {
	handler := NewCodexResponsesHandler(CodexResponsesOptions{})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"m"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected bad gateway for missing executor, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "upstream executor missing") {
		t.Fatalf("expected missing executor error, got %s", rec.Body.String())
	}
}

func TestNewAnthropicMessagesToOpenAIChatHandlerPreservesRuntimeDependencies(t *testing.T) {
	var cache reasoningfeature.ReasoningCache
	logf := func(string, ...any) {}
	post := func(context.Context, map[string]any) (*http.Response, error) { return nil, nil }

	handler := NewAnthropicMessagesToOpenAIChatHandler(AnthropicMessagesOptions{
		PreferredModel:      "mimo-v2.5-pro",
		UpstreamBase:        "https://gateway.example/v1",
		ReasoningCache:      &cache,
		Logf:                logf,
		PostChatCompletions: post,
	})

	if handler.preferredModel != "mimo-v2.5-pro" || handler.upstreamBase != "https://gateway.example/v1" {
		t.Fatalf("runtime config not preserved: %#v", handler)
	}
	if handler.reasoningCache != &cache || handler.logf == nil || handler.postChatCompletions == nil {
		t.Fatalf("runtime dependencies not preserved: %#v", handler)
	}
}
