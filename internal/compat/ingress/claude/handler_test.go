package claude

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

func TestToBifrostRequest_Anthropic(t *testing.T) {
	profile := &config.Profile{
		AnthropicBaseURL: "https://api.anthropic.com/v1",
		OpenAIAPIType:    config.OpenAIAPITypeAnthropicMessages,
		APIKey:           "sk-ant-test",
		DefaultModel:     "claude-3-5-sonnet-20241022",
	}

	rawReq := map[string]any{
		"model":  "claude-3-5-sonnet-20241022",
		"system": "You are Claude.",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Hi Claude",
			},
		},
		"stream": true,
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 2048,
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
	if bReq.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("Model = %v, want claude-3-5-sonnet-20241022", bReq.Model)
	}
	if bReq.Provider != schemas.Anthropic {
		t.Errorf("Provider = %v, want anthropic", bReq.Provider)
	}
	if len(bReq.Input) != 2 {
		t.Fatalf("Input count = %d, want 2 (system + user)", len(bReq.Input))
	}
	if bReq.Input[0].Role != schemas.ChatMessageRoleSystem {
		t.Errorf("First role = %v, want system", bReq.Input[0].Role)
	}
	if bReq.Params == nil || bReq.Params.Reasoning == nil || *bReq.Params.Reasoning.MaxTokens != 2048 {
		t.Errorf("Reasoning budget mismatch: %+v", bReq.Params)
	}
}

func TestToBifrostRequest_ToolsAndToolResults(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		APIKey:        "sk-openai-test",
		DefaultModel:  "gpt-4o",
	}

	rawReq := map[string]any{
		"model": "gpt-4o",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "System instructions here.",
				"cache_control": map[string]any{
					"type": "ephemeral",
				},
			},
		},
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Calculate 2+2",
					},
				},
			},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":     "thinking",
						"thinking": "Let me calculate.",
					},
					map[string]any{
						"type": "tool_use",
						"id":   "call_1",
						"name": "calc",
						"input": map[string]any{
							"expr": "2+2",
						},
					},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "call_1",
						"content":     "4",
					},
				},
			},
		},
		"tools": []any{
			map[string]any{
				"name":        "calc",
				"description": "Calculate math expressions",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"expr": map[string]any{"type": "string"},
					},
					"required": []any{"expr"},
				},
			},
		},
		"tool_choice": map[string]any{
			"type": "auto",
		},
		"max_tokens":  4096,
		"temperature": 0.7,
	}

	bReq := ToBifrostRequest(rawReq, profile)
	if bReq.Provider != schemas.OpenAI {
		t.Errorf("Provider = %v, want openai", bReq.Provider)
	}
	if len(bReq.Input) < 4 {
		t.Fatalf("expected at least 4 input messages, got %d", len(bReq.Input))
	}
	if len(bReq.Params.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(bReq.Params.Tools))
	}
	if bReq.Params.Tools[0].Function == nil || bReq.Params.Tools[0].Function.Name != "calc" {
		t.Errorf("tool function name mismatch: %+v", bReq.Params.Tools[0].Function)
	}
	if bReq.Params.MaxCompletionTokens == nil || *bReq.Params.MaxCompletionTokens != 4096 {
		t.Errorf("MaxCompletionTokens = %v, want 4096", bReq.Params.MaxCompletionTokens)
	}
}

func TestClaudeHandler_MethodNotAllowed(t *testing.T) {
	ctx := context.Background()
	eng, err := engine.New(ctx, nil)
	if err != nil {
		t.Fatalf("Engine init error: %v", err)
	}
	h := NewHandler(eng, &config.Profile{}, "", nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestClaudeHandler_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	eng, err := engine.New(ctx, nil)
	if err != nil {
		t.Fatalf("Engine init error: %v", err)
	}
	h := NewHandler(eng, &config.Profile{}, "", nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("bad-json"))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestClaudeHandler_ProfileResolverAndSessionLogf(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl_mock",
			"model": "gpt-4o",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Hello from mock"
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

	h := NewHandler(eng, nil, "fallback-model", nil)

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
		if md, ok := req["metadata"].(map[string]any); ok {
			loggedSession = md["session_id"].(string)
		}
		return func(format string, args ...any) {}
	})

	body := `{"model":"gpt-4o","metadata":{"session_id":"sess_123"},"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if resolvedModel != "gpt-4o" {
		t.Errorf("resolvedModel = %q, want gpt-4o", resolvedModel)
	}
	if loggedSession != "sess_123" {
		t.Errorf("loggedSession = %q, want sess_123", loggedSession)
	}
	if !strings.Contains(w.Body.String(), "Hello from mock") {
		t.Errorf("response missing expected content: %s", w.Body.String())
	}
}
