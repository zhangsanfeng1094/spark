package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
)

type streamE2EExecutor struct {
	chatReq map[string]any
	body    string
}

func (e *streamE2EExecutor) Do(_ context.Context, chatReq map[string]any) (*http.Response, error) {
	e.chatReq = chatReq
	return chatStreamResponse(e.body), nil
}

func chatStreamResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func assertOrderedSubstrings(t testingT, s string, parts []string) {
	t.Helper()
	offset := 0
	for _, part := range parts {
		idx := strings.Index(s[offset:], part)
		if idx < 0 {
			t.Fatalf("missing %q after offset %d in: %s", part, offset, s)
		}
		offset += idx + len(part)
	}
}

type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

type captureChatExecutor struct {
	chatReq map[string]any
	body    string
}

func (e *captureChatExecutor) Do(_ context.Context, chatReq map[string]any) (*http.Response, error) {
	e.chatReq = chatReq
	body := e.body
	if body == "" {
		body = `{"id":"chatcmpl_1","choices":[{"message":{"content":"ok"}}]}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}
