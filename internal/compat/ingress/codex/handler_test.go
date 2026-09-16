package codex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/compat/engine"
	"spark/internal/config"
)

func TestToBifrostRequest(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		APIKey:        "sk-test",
		DefaultModel:  "gpt-4o",
	}

	rawReq := map[string]any{
		"model":        "gpt-4o",
		"instructions": "You are a helpful assistant",
		"input": []any{
			map[string]any{
				"role":    "user",
				"content": "Hello world",
			},
		},
		"stream": true,
		"reasoning": map[string]any{
			"effort": "high",
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
	if bReq.Model != "gpt-4o" {
		t.Errorf("Model = %v, want gpt-4o", bReq.Model)
	}
	if bReq.Provider != schemas.OpenAI {
		t.Errorf("Provider = %v, want openai", bReq.Provider)
	}
	if len(bReq.Input) != 2 {
		t.Fatalf("Input length = %d, want 2 (system + user)", len(bReq.Input))
	}
	if bReq.Input[0].Role != schemas.ChatMessageRoleSystem {
		t.Errorf("First msg role = %v, want system", bReq.Input[0].Role)
	}
	if bReq.Params == nil || bReq.Params.Reasoning == nil || *bReq.Params.Reasoning.Effort != "high" {
		t.Errorf("Reasoning effort mismatch: %+v", bReq.Params)
	}
}

func TestToBifrostRequest_ToolsAndReasoningAndToolResults(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		APIKey:        "sk-test",
		DefaultModel:  "gpt-4o",
	}

	rawReq := map[string]any{
		"model":        "gpt-4o",
		"instructions": "System instructions",
		"input": []any{
			map[string]any{
				"role":    "user",
				"content": "What is the weather?",
			},
			map[string]any{
				"type": "reasoning",
				"summary": []any{
					map[string]any{"text": "Checking tools"},
				},
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_123",
				"name":      "get_weather",
				"arguments": `{"city":"Tokyo"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_123",
				"output":  `{"temp":22}`,
			},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"name": "get_weather",
				"description": "Get weather for a city",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []any{"city"},
				},
			},
		},
		"max_output_tokens": 2048,
		"temperature":       0.5,
	}

	bReq := ToBifrostRequest(rawReq, profile)
	if len(bReq.Input) < 4 {
		t.Fatalf("Input length = %d, expected at least 4 messages", len(bReq.Input))
	}
	if len(bReq.Params.Tools) != 1 {
		t.Fatalf("Tools length = %d, expected 1", len(bReq.Params.Tools))
	}
	if bReq.Params.Tools[0].Function == nil || bReq.Params.Tools[0].Function.Name != "get_weather" {
		t.Errorf("Tool function name mismatch: %+v", bReq.Params.Tools[0].Function)
	}
	if bReq.Params.MaxCompletionTokens == nil || *bReq.Params.MaxCompletionTokens != 2048 {
		t.Errorf("MaxCompletionTokens = %v, want 2048", bReq.Params.MaxCompletionTokens)
	}
}

func TestCodexHandler_MethodNotAllowed(t *testing.T) {
	ctx := context.Background()
	eng, err := engine.New(ctx, nil)
	if err != nil {
		t.Fatalf("Engine init error: %v", err)
	}
	h := NewHandler(eng, &config.Profile{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestCodexHandler_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	eng, err := engine.New(ctx, nil)
	if err != nil {
		t.Fatalf("Engine init error: %v", err)
	}
	h := NewHandler(eng, &config.Profile{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("invalid-json"))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCodexHandler_ProfileResolverAndSessionLogf(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl_mock",
			"model": "gpt-4o",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Hello from mock codex"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 5,
				"completion_tokens": 3,
				"total_tokens": 8
			}
		}`))
	}))
	defer mockUpstream.Close()

	ctx := context.Background()
	eng, err := engine.New(ctx, nil)
	if err != nil {
		t.Fatalf("Engine init error: %v", err)
	}

	h := NewHandler(eng, nil, nil)

	var resolvedModel string
	h.SetProfileResolver(func(r *http.Request, modelName string) *config.Profile {
		resolvedModel = modelName
		return &config.Profile{
			OpenAIBaseURL: mockUpstream.URL + "/v1",
			APIKey:        "sk-mock-key",
			DefaultModel:  modelName,
		}
	})

	var loggedSession string
	h.SetSessionLogf(func(req map[string]any) func(format string, args ...any) {
		if md, ok := req["client_metadata"].(map[string]any); ok {
			loggedSession = md["x-codex-window-id"].(string)
		}
		return func(format string, args ...any) {}
	})

	body := `{"model":"gpt-4o","client_metadata":{"x-codex-window-id":"win_123"},"input":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if resolvedModel != "gpt-4o" {
		t.Errorf("resolvedModel = %q, want gpt-4o", resolvedModel)
	}
	if loggedSession != "win_123" {
		t.Errorf("loggedSession = %q, want win_123", loggedSession)
	}
	if !strings.Contains(w.Body.String(), "Hello from mock codex") {
		t.Errorf("response missing expected content: %s", w.Body.String())
	}
}
