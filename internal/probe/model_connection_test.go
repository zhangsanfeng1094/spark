package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spark/internal/config"
)

type fakePoster struct {
	requests []Request
	status   int
	body     []byte
	err      error
}

func (f *fakePoster) PostJSON(_ context.Context, req Request) (Response, error) {
	f.requests = append(f.requests, req)
	return Response{Status: f.status, Body: f.body}, f.err
}

type sequencePoster struct {
	requests  []Request
	responses []Response
	errs      []error
}

func (s *sequencePoster) PostJSON(_ context.Context, req Request) (Response, error) {
	s.requests = append(s.requests, req)
	idx := len(s.requests) - 1
	var resp Response
	if idx < len(s.responses) {
		resp = s.responses[idx]
	}
	var err error
	if idx < len(s.errs) {
		err = s.errs[idx]
	}
	return resp, err
}

func TestModelConnectionWritesLogFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "model-test.log")
	t.Setenv("SPARK_MODEL_TEST_LOG", logPath)

	result := testModelConnection(nil, "", &fakePoster{status: 200})
	if result.LogPath != logPath {
		t.Fatalf("expected log path %q, got %q", logPath, result.LogPath)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected log file to exist, read failed: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[model-test] ===== model connection test start =====") {
		t.Fatalf("missing start log line, got: %q", content)
	}
	if !strings.Contains(content, `result=fail reason="Profile is nil"`) {
		t.Fatalf("missing fail reason log line, got: %q", content)
	}
}

func TestModelConnectionTestsAllExplicitEndpoints(t *testing.T) {
	poster := &fakePoster{err: errors.New("dial refused")}
	profile := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:1/v1",
		OpenAIAPIType: "responses,chat_completions",
		DefaultModel:  "m1",
	}

	got := testModelConnection(profile, "", poster)
	if got.Success {
		t.Fatalf("expected failure against fake poster, got success: %s", got.Message)
	}
	if !strings.Contains(got.Message, "responses: ERR") || !strings.Contains(got.Message, "chat_completions: ERR") {
		t.Fatalf("expected both endpoint results in message, got %q", got.Message)
	}
	if len(poster.requests) != 2 {
		t.Fatalf("expected two requests, got %d", len(poster.requests))
	}
}

func TestModelConnectionSucceedsWhenAnyExplicitEndpointWorks(t *testing.T) {
	poster := &sequencePoster{
		responses: []Response{
			{Status: 200, Body: []byte(`{"id":"resp_1"}`)},
			{Status: 400, Body: []byte(`{"error":{"message":"Unsupported parameter: 'max_tokens'"}}`)},
		},
	}
	profile := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:1/v1",
		OpenAIAPIType: "responses,chat_completions",
		DefaultModel:  "gpt-5",
	}

	got := testModelConnection(profile, "", poster)
	if !got.Success {
		t.Fatalf("expected success when responses endpoint works, got: %s", got.Message)
	}
	if !strings.Contains(got.Message, "responses: HTTP 200") || !strings.Contains(got.Message, "chat_completions: HTTP 400") {
		t.Fatalf("expected both endpoint results in success message, got %q", got.Message)
	}
	if len(poster.requests) != 2 {
		t.Fatalf("expected two requests, got %d", len(poster.requests))
	}
}

func TestConnectionTestPayloadUsesMaxCompletionTokensForGPT5ChatCompletions(t *testing.T) {
	_, payload, err := connectionTestPayload("gpt-5", config.OpenAIAPITypeChatCompletions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload["max_completion_tokens"] != 16 {
		t.Fatalf("expected max_completion_tokens=16, got %#v", payload)
	}
	if _, ok := payload["max_tokens"]; ok {
		t.Fatalf("did not expect max_tokens for gpt-5 payload: %#v", payload)
	}
}

func TestModelConnectionAnthropicEndpointDoesNotFallbackToResponses(t *testing.T) {
	poster := &fakePoster{err: errors.New("dial refused")}
	profile := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:1",
		OpenAIAPIType: config.OpenAIAPITypeAnthropicMessages,
		DefaultModel:  "claude-sonnet-4-20250514",
	}

	got := testModelConnection(profile, "", poster)
	if got.Success {
		t.Fatalf("expected failure against fake poster, got success: %s", got.Message)
	}
	if !strings.Contains(got.Message, "anthropic_messages: ERR") {
		t.Fatalf("expected anthropic endpoint result in message, got %q", got.Message)
	}
	if strings.Contains(got.Message, "responses: ERR") {
		t.Fatalf("did not expect responses fallback for anthropic profile, got %q", got.Message)
	}
	if len(poster.requests) != 1 || poster.requests[0].EndpointType != config.OpenAIAPITypeAnthropicMessages {
		t.Fatalf("expected only anthropic request, got %#v", poster.requests)
	}
}

func TestModelConnectionUsesCurlStylePostRequest(t *testing.T) {
	args := curlPostJSONArgs(Request{
		EndpointType: config.OpenAIAPITypeChatCompletions,
		URL:          "https://gateway.example/v1/chat/completions",
		APIKey:       "sk-test",
		Org:          "org-test",
		Project:      "proj-test",
		Payload:      map[string]any{"model": "m1"},
	}, 8*time.Second)

	joined := strings.Join(args, "\n")
	for _, want := range []string{
		"--request\nPOST",
		"https://gateway.example/v1/chat/completions",
		"--header\nContent-Type: application/json",
		"--header\nAuthorization: Bearer sk-test",
		"--header\nOpenAI-Organization: org-test",
		"--header\nOpenAI-Project: proj-test",
		"--data-binary\n@-",
		"__SPARK_HTTP_STATUS__:%{http_code}",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected curl args to contain %q, got %#v", want, args)
		}
	}
}

func TestParseCurlResponse(t *testing.T) {
	resp, err := parseCurlResponse([]byte("{\"ok\":true}\n__SPARK_HTTP_STATUS__:201"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 201 || string(resp.Body) != "{\"ok\":true}" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}
