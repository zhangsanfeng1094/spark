package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/config"
)

func TestToBifrostRequest_OpenAI(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		OpenAIAPIType: config.OpenAIAPITypeChatCompletions,
		APIKey:        "sk-test-key",
		DefaultModel:  "gpt-4o",
	}

	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello!"}
		],
		"temperature": 0.7,
		"max_tokens": 1000,
		"stream": true
	}`)

	var rawReq map[string]any
	if err := json.Unmarshal(body, &rawReq); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	bReq := ToBifrostRequest(rawReq, body, profile)
	if bReq.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", bReq.Model, "gpt-4o")
	}
	if bReq.Provider != schemas.OpenAI {
		t.Errorf("Provider = %v, want %v", bReq.Provider, schemas.OpenAI)
	}
	if len(bReq.Input) != 2 {
		t.Fatalf("Input length = %d, want 2", len(bReq.Input))
	}
	if bReq.Input[0].Role != schemas.ChatMessageRoleSystem {
		t.Errorf("First msg role = %v, want system", bReq.Input[0].Role)
	}
	if bReq.Params == nil || bReq.Params.MaxCompletionTokens == nil || *bReq.Params.MaxCompletionTokens != 1000 {
		t.Errorf("MaxCompletionTokens mismatch: %+v", bReq.Params)
	}
}

func TestForwardStream_OpenAI(t *testing.T) {
	rec := httptest.NewRecorder()
	streamChan := make(chan *schemas.BifrostStreamChunk, 2)

	content := "Hello world"
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:     "chatcmpl-test",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Choices: []schemas.BifrostResponseChoice{
				{
					Index: 0,
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{
							Content: &content,
						},
					},
				},
			},
		},
	}
	close(streamChan)

	calledUsage := false
	ForwardStream(rec, streamChan, "gpt-4o", func(u *schemas.BifrostLLMUsage, model string) {
		calledUsage = true
	})
	_ = calledUsage

	body := rec.Body.String()
	if !strings.Contains(body, "chatcmpl-test") {
		t.Errorf("Stream body does not contain id: %s", body)
	}
	if !strings.Contains(body, "Hello world") {
		t.Errorf("Stream body does not contain delta content: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("Stream body does not contain [DONE]: %s", body)
	}
}

func TestListModels(t *testing.T) {
	h := NewHandler(nil, &config.Profile{
		DefaultModel: "my-test-model",
		Models:       []string{"my-test-model-2"},
	}, "", nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal list models response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("Expected non-empty data list, got: %+v", resp)
	}

	found := false
	for _, item := range data {
		if m, ok := item.(map[string]any); ok {
			if m["id"] == "my-test-model" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("my-test-model not found in models list")
	}
}
