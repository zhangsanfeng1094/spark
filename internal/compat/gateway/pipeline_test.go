package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type staticTranslator struct {
	chatReq map[string]any
}

func (t staticTranslator) ToChat(map[string]any) (map[string]any, error) {
	return t.chatReq, nil
}

type captureExecutor struct {
	chatReq map[string]any
}

func (e *captureExecutor) Do(_ context.Context, chatReq map[string]any) (*http.Response, error) {
	e.chatReq = chatReq
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

func TestExecuteTranslatedChatPreparesRequestBeforeExecution(t *testing.T) {
	chatReq := map[string]any{"model": "mimo-v2.5-pro"}
	executor := &captureExecutor{}

	_, _, err := ExecuteTranslatedChat(
		context.Background(),
		map[string]any{},
		staticTranslator{chatReq: chatReq},
		executor,
		func(chatReq map[string]any) {
			chatReq["prepared"] = true
		},
	)
	if err != nil {
		t.Fatalf("ExecuteTranslatedChat returned error: %v", err)
	}
	if executor.chatReq["prepared"] != true {
		t.Fatalf("expected prepared chat request before executor, got %#v", executor.chatReq)
	}
}
