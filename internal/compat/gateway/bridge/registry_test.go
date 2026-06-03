package bridge

import (
	"errors"
	"strings"
	"testing"

	"spark/internal/compat/gateway/core"
)

func TestSelectRouteComposesClientAndTargetCodecs(t *testing.T) {
	tests := []struct {
		name   string
		target core.TargetProtocol
		check  func(t *testing.T, out map[string]any)
	}{
		{
			name:   "openai chat target",
			target: core.TargetOpenAIChat,
			check: func(t *testing.T, out map[string]any) {
				if out["model"] != "mimo-v2.5-pro" {
					t.Fatalf("translator did not build chat request: %#v", out)
				}
				if _, ok := out["messages"]; !ok {
					t.Fatalf("missing chat messages: %#v", out)
				}
			},
		},
		{
			name:   "anthropic messages target",
			target: core.TargetAnthropicMessages,
			check: func(t *testing.T, out map[string]any) {
				if out["model"] != "mimo-v2.5-pro" || out["system"] != "be concise" || out["max_tokens"] != 123 {
					t.Fatalf("translator did not build anthropic request: %#v", out)
				}
			},
		},
		{
			name:   "openai responses target",
			target: core.TargetOpenAIResponses,
			check: func(t *testing.T, out map[string]any) {
				if out["model"] != "mimo-v2.5-pro" || out["instructions"] != "be concise" || out["max_output_tokens"] != 123 {
					t.Fatalf("translator did not build responses request: %#v", out)
				}
				if out["input"] == nil {
					t.Fatalf("missing responses input: %#v", out)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selection, err := SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: tt.target})
			if err != nil {
				t.Fatalf("select route: %v", err)
			}
			if selection.Route.Client != core.ClientCodexResponses || selection.Route.Target != tt.target {
				t.Fatalf("route mismatch: %#v", selection.Route)
			}
			if selection.Translator == nil || selection.Stream == nil || selection.NonStream == nil {
				t.Fatalf("expected translator and writers: %#v", selection)
			}
			out, err := selection.Translator.Translate(map[string]any{
				"model":               "mimo-v2.5-pro",
				"instructions":        "be concise",
				"input":               "hello",
				"max_output_tokens":   float64(123),
				"parallel_tool_calls": false,
			})
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			tt.check(t, out)
		})
	}
}

func TestSelectRouteUnsupportedCombination(t *testing.T) {
	_, err := SelectRoute(core.Route{Client: core.ClientProtocol("unknown"), Target: core.TargetAnthropicMessages})
	var routeErr core.UnsupportedRouteError
	if !errors.As(err, &routeErr) {
		t.Fatalf("expected unsupported route error, got %v", err)
	}
	if !strings.Contains(err.Error(), "client=unknown target=anthropic_messages") {
		t.Fatalf("unclear route error: %v", err)
	}
}

func TestCodecValidationReportsMissingFields(t *testing.T) {
	if err := validateClientCodec(ClientCodec{Protocol: core.ClientCodexResponses}); !strings.Contains(err.Error(), "missing=RequestInbound") {
		t.Fatalf("expected missing client field, got %v", err)
	}
	if err := validateTargetCodec(TargetCodec{Protocol: core.TargetOpenAIChat}); !strings.Contains(err.Error(), "missing=RequestOutbound") {
		t.Fatalf("expected missing target field, got %v", err)
	}
}
