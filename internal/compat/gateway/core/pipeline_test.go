package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type staticTranslator struct {
	upstreamReq map[string]any
}

func (t staticTranslator) Translate(map[string]any) (map[string]any, error) {
	return t.upstreamReq, nil
}

type captureExecutor struct {
	upstreamReq map[string]any
}

func (e *captureExecutor) Do(_ context.Context, upstreamReq map[string]any) (*http.Response, error) {
	e.upstreamReq = upstreamReq
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

func TestExecuteTranslatedPreparesRequestBeforeExecution(t *testing.T) {
	upstreamReq := map[string]any{"model": "mimo-v2.5-pro"}
	executor := &captureExecutor{}

	_, _, err := ExecuteTranslated(
		context.Background(),
		map[string]any{},
		staticTranslator{upstreamReq: upstreamReq},
		executor,
		func(upstreamReq map[string]any) {
			upstreamReq["prepared"] = true
		},
	)
	if err != nil {
		t.Fatalf("ExecuteTranslated returned error: %v", err)
	}
	if executor.upstreamReq["prepared"] != true {
		t.Fatalf("expected prepared request before executor, got %#v", executor.upstreamReq)
	}
}
