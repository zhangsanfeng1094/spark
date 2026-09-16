package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

// Compile-time check that opencodeProvider satisfies the full Provider interface.
var _ schemas.Provider = (*opencodeProvider)(nil)

func TestOpencodeProviderConstructors(t *testing.T) {
	t.Parallel()

	t.Run("Zen constructor defaults", func(t *testing.T) {
		zenConfig := &schemas.ProviderConfig{}
		zenConfig.CheckAndSetDefaults()
		provider, err := NewOpencodeZenProvider(zenConfig, nil)
		if err != nil {
			t.Fatalf("NewOpencodeZenProvider failed: %v", err)
		}
		if provider.GetProviderKey() != schemas.OpencodeZen {
			t.Errorf("expected provider key %s, got %s", schemas.OpencodeZen, provider.GetProviderKey())
		}
		if provider.networkConfig.BaseURL != "https://opencode.ai/zen" {
			t.Errorf("expected base URL https://opencode.ai/zen, got %s", provider.networkConfig.BaseURL)
		}
	})

	t.Run("Go constructor defaults", func(t *testing.T) {
		goConfig := &schemas.ProviderConfig{}
		goConfig.CheckAndSetDefaults()
		provider, err := NewOpencodeGoProvider(goConfig, nil)
		if err != nil {
			t.Fatalf("NewOpencodeGoProvider failed: %v", err)
		}
		if provider.GetProviderKey() != schemas.OpencodeGo {
			t.Errorf("expected provider key %s, got %s", schemas.OpencodeGo, provider.GetProviderKey())
		}
		if provider.networkConfig.BaseURL != "https://opencode.ai/zen/go" {
			t.Errorf("expected base URL https://opencode.ai/zen/go, got %s", provider.networkConfig.BaseURL)
		}
	})
}

// TestOpencodeResponsesRouting verifies that Zen and Go providers forward both
// regular and streaming Responses calls to their native OpenAI-compatible endpoint.
func TestOpencodeResponsesRouting(t *testing.T) {
	const (
		model     = "opencode-test-model"
		apiKey    = "opencode-test-key"
		inputText = "exercise native responses routing"
	)

	for _, tc := range []struct {
		name        string
		providerKey schemas.ModelProvider
		newProvider func(*schemas.ProviderConfig) (*opencodeProvider, error)
	}{
		{name: "Zen", providerKey: schemas.OpencodeZen, newProvider: func(config *schemas.ProviderConfig) (*opencodeProvider, error) {
			return NewOpencodeZenProvider(config, nil)
		}},
		{name: "Go", providerKey: schemas.OpencodeGo, newProvider: func(config *schemas.ProviderConfig) (*opencodeProvider, error) {
			return NewOpencodeGoProvider(config, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type capturedRequest struct {
				method        string
				path          string
				authorization string
				body          map[string]any
			}

			var (
				mu       sync.Mutex
				captures []capturedRequest
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}

				mu.Lock()
				captures = append(captures, capturedRequest{
					method:        r.Method,
					path:          r.URL.Path,
					authorization: r.Header.Get("Authorization"),
					body:          payload,
				})
				mu.Unlock()

				if streaming, _ := payload["stream"].(bool); streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"id\":\"resp_stream\",\"object\":\"response\",\"model\":\"opencode-test-model\",\"output\":[]}}\n\n")
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"id":"resp_regular","object":"response","model":"opencode-test-model","output":[]}`)
			}))
			defer server.Close()

			provider, err := tc.newProvider(&schemas.ProviderConfig{
				NetworkConfig: schemas.NetworkConfig{
					BaseURL:                        server.URL,
					DefaultRequestTimeoutInSeconds: 10,
				},
			})
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}

			newRequest := func() *schemas.BifrostResponsesRequest {
				return &schemas.BifrostResponsesRequest{
					Provider: tc.providerKey,
					Model:    model,
					Input: []schemas.ResponsesMessage{{
						Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
						Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr(inputText)},
					}},
				}
			}
			key := schemas.Key{Value: *schemas.NewSecretVar(apiKey)}

			ctx, cancel := schemas.NewBifrostContextWithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			response, bifrostErr := provider.Responses(ctx, key, newRequest())
			if bifrostErr != nil {
				t.Fatalf("Responses: %v", bifrostErr)
			}
			if response == nil || response.ID == nil || *response.ID != "resp_regular" {
				t.Fatalf("Responses returned %#v, want response id resp_regular", response)
			}

			streamCtx, streamCancel := schemas.NewBifrostContextWithTimeout(context.Background(), 10*time.Second)
			defer streamCancel()
			postHookRunner := func(_ *schemas.BifrostContext, result *schemas.BifrostResponse, _ *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError) {
				return result, nil
			}
			stream, bifrostErr := provider.ResponsesStream(streamCtx, postHookRunner, nil, key, newRequest())
			if bifrostErr != nil {
				t.Fatalf("ResponsesStream: %v", bifrostErr)
			}
			streamed := false
			for chunk := range stream {
				if chunk != nil {
					streamed = true
				}
			}
			if !streamed {
				t.Fatal("ResponsesStream completed without emitting a response chunk")
			}

			mu.Lock()
			defer mu.Unlock()
			if len(captures) != 2 {
				t.Fatalf("upstream request count = %d, want 2", len(captures))
			}
			seenRegular, seenStreaming := false, false
			for _, capture := range captures {
				if capture.method != http.MethodPost {
					t.Errorf("request method = %q, want %q", capture.method, http.MethodPost)
				}
				if capture.path != "/v1/responses" {
					t.Errorf("request path = %q, want /v1/responses", capture.path)
				}
				if capture.authorization != "Bearer "+apiKey {
					t.Errorf("Authorization = %q, want %q", capture.authorization, "Bearer "+apiKey)
				}
				if gotModel, _ := capture.body["model"].(string); gotModel != model {
					t.Errorf("request model = %q, want %q", gotModel, model)
				}
				input, ok := capture.body["input"].([]any)
				if !ok || len(input) != 1 {
					t.Errorf("request input = %#v, want one message", capture.body["input"])
				}

				if streaming, _ := capture.body["stream"].(bool); streaming {
					seenStreaming = true
				} else {
					seenRegular = true
				}
			}
			if !seenRegular || !seenStreaming {
				t.Errorf("saw regular=%t streaming=%t requests, want both", seenRegular, seenStreaming)
			}
		})
	}
}
