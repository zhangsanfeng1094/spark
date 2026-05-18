package gateway

import (
	"errors"
	"strings"
	"testing"

	"spark/internal/compat/policy"
)

func TestCodexResponsesToOpenAIChatRequest(t *testing.T) {
	out := CodexResponsesToOpenAIChatRequest(map[string]any{
		"model": "mimo-v2.5-pro",
		"input": []any{
			map[string]any{
				"type": "reasoning",
				"summary": []any{
					map[string]any{"type": "summary_text", "text": "think first"},
				},
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "sum",
				"arguments": `{"a":1}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  `{"result":1}`,
			},
		},
	}, policy.PreserveReasoningContent())

	if out["model"] != "mimo-v2.5-pro" {
		t.Fatalf("model mismatch: %#v", out)
	}
	msgs := out["messages"].([]map[string]any)
	if msgs[0]["reasoning_content"] != "think first" {
		t.Fatalf("reasoning mismatch: %#v", msgs[0])
	}
	if len(msgs[0]["tool_calls"].([]map[string]any)) != 1 {
		t.Fatalf("tool calls mismatch: %#v", msgs[0])
	}
}

func TestSelectRouteCodexResponsesToOpenAIChat(t *testing.T) {
	selection, err := SelectRoute(Route{Client: ClientCodexResponses, Target: TargetOpenAIChat})
	if err != nil {
		t.Fatalf("select route: %v", err)
	}
	if selection.Route.Client != ClientCodexResponses || selection.Route.Target != TargetOpenAIChat {
		t.Fatalf("route mismatch: %#v", selection.Route)
	}
	if selection.Translator == nil || selection.Stream == nil || selection.NonStream == nil {
		t.Fatalf("expected translator and writers: %#v", selection)
	}
	chatReq, err := selection.Translator.ToChat(map[string]any{
		"model": "mimo-v2.5-pro",
		"input": "hello",
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if chatReq["model"] != "mimo-v2.5-pro" {
		t.Fatalf("translator did not build chat request: %#v", chatReq)
	}
}

func TestSelectRouteUnsupportedCombination(t *testing.T) {
	_, err := SelectRoute(Route{Client: ClientCodexResponses, Target: TargetAnthropicMessages})
	var routeErr UnsupportedRouteError
	if !errors.As(err, &routeErr) {
		t.Fatalf("expected unsupported route error, got %v", err)
	}
	if !strings.Contains(err.Error(), "client=codex_responses target=anthropic_messages") {
		t.Fatalf("unclear route error: %v", err)
	}
}
