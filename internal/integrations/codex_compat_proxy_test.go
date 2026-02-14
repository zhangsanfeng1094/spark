package integrations

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestResponsesToChatCompletions_StringInput(t *testing.T) {
	req := map[string]any{
		"model":             "glm-5:cloud",
		"input":             "hello",
		"stream":            true,
		"max_output_tokens": float64(32),
	}
	out := responsesToChatCompletions(req)

	if out["model"] != "glm-5:cloud" {
		t.Fatalf("model mismatch: %v", out["model"])
	}
	if out["stream"] != true {
		t.Fatalf("stream mismatch: %v", out["stream"])
	}
	if out["max_tokens"] != 32 {
		t.Fatalf("max_tokens mismatch: %v", out["max_tokens"])
	}
	msgs, ok := out["messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages mismatch: %#v", out["messages"])
	}
	if msgs[0]["content"] != "hello" {
		t.Fatalf("message content mismatch: %#v", msgs[0])
	}
}

func TestResponsesToChatCompletions_ToolsMappedAndFiltered(t *testing.T) {
	req := map[string]any{
		"model": "GLM-4.7",
		"input": "hello",
		"tools": []any{
			map[string]any{
				"type":        "function",
				"name":        "sum",
				"description": "add numbers",
				"parameters": map[string]any{
					"type": "object",
				},
			},
			map[string]any{
				"type": "web_search_preview",
			},
		},
		"tool_choice": map[string]any{
			"type": "function",
			"name": "sum",
		},
	}
	out := responsesToChatCompletions(req)

	tools, ok := out["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools mismatch: %#v", out["tools"])
	}
	if tools[0]["type"] != "function" {
		t.Fatalf("tool type mismatch: %#v", tools[0])
	}
	fn, ok := tools[0]["function"].(map[string]any)
	if !ok || fn["name"] != "sum" {
		t.Fatalf("tool function mismatch: %#v", tools[0])
	}

	tc, ok := out["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice mismatch: %#v", out["tool_choice"])
	}
	tcFn, ok := tc["function"].(map[string]any)
	if !ok || tcFn["name"] != "sum" {
		t.Fatalf("tool_choice function mismatch: %#v", tc)
	}
}

func TestResponsesInputToMessages_ArrayInput(t *testing.T) {
	input := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "first"},
				map[string]any{"type": "input_text", "text": "second"},
			},
		},
	}

	msgs := responsesInputToMessages(input)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0]["role"] != "user" {
		t.Fatalf("role mismatch: %#v", msgs[0])
	}
	if msgs[0]["content"] != "first\nsecond" {
		t.Fatalf("content mismatch: %#v", msgs[0])
	}
}

func TestResponsesInputToMessages_DeveloperRoleMappedToSystem(t *testing.T) {
	input := []any{
		map[string]any{
			"role":    "developer",
			"content": "be concise",
		},
	}
	msgs := responsesInputToMessages(input)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" {
		t.Fatalf("expected system role, got %#v", msgs[0]["role"])
	}
}

func TestResponsesInputToMessages_FunctionCallOutputMapped(t *testing.T) {
	input := []any{
		map[string]any{
			"type":      "function_call",
			"call_id":   "call_123",
			"name":      "sum",
			"arguments": `{"a":1,"b":2}`,
		},
		map[string]any{
			"type":    "function_call_output",
			"call_id": "call_123",
			"output":  `{"result":3}`,
		},
	}
	msgs := responsesInputToMessages(input)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d (%#v)", len(msgs), msgs)
	}
	if msgs[0]["role"] != "assistant" {
		t.Fatalf("expected synthetic assistant tool_call message, got %#v", msgs[0])
	}
	if msgs[1]["role"] != "tool" || msgs[1]["tool_call_id"] != "call_123" {
		t.Fatalf("expected tool message with call_id, got %#v", msgs[1])
	}
}

func TestResponsesInputToMessages_ToolRolePreservesToolCallID(t *testing.T) {
	input := []any{
		map[string]any{
			"role":         "tool",
			"tool_call_id": "call_456",
			"content":      "ok",
		},
	}
	msgs := responsesInputToMessages(input)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d (%#v)", len(msgs), msgs)
	}
	if msgs[0]["role"] != "assistant" {
		t.Fatalf("expected synthetic assistant before tool, got %#v", msgs[0])
	}
	if msgs[1]["role"] != "tool" || msgs[1]["tool_call_id"] != "call_456" {
		t.Fatalf("expected tool_call_id passthrough, got %#v", msgs[1])
	}
}

func TestShouldRetryWithMinimalChatReq(t *testing.T) {
	if !shouldRetryWithMinimalChatReq(400, []byte("invalid json")) {
		t.Fatal("expected retry for plain invalid json")
	}
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": "invalid json",
		},
	})
	if !shouldRetryWithMinimalChatReq(400, body) {
		t.Fatal("expected retry for json invalid json")
	}
	if shouldRetryWithMinimalChatReq(401, body) {
		t.Fatal("unexpected retry on non-400 status")
	}
}

func TestUltraMinimalChatCompletionsRequest(t *testing.T) {
	chatReq := map[string]any{
		"model": "GLM-4.7",
		"messages": []map[string]any{
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "你好"},
		},
		"stream": true,
	}
	out := ultraMinimalChatCompletionsRequest(chatReq)
	if out["model"] != "GLM-4.7" {
		t.Fatalf("model mismatch: %v", out["model"])
	}
	msgs, ok := out["messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages mismatch: %#v", out["messages"])
	}
	if msgs[0]["role"] != "user" || msgs[0]["content"] != "你好" {
		t.Fatalf("unexpected minimal message: %#v", msgs[0])
	}
}

func TestDecodeResponsesRequest_PlainJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"GLM-4.7","input":"hi"}`))
	req, raw, err := decodeResponsesRequest(r)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !strings.Contains(raw, `"model":"GLM-4.7"`) {
		t.Fatalf("raw mismatch: %q", raw)
	}
	if req["model"] != "GLM-4.7" {
		t.Fatalf("model mismatch: %#v", req)
	}
}

func TestForwardNonStream_MapsToolCallsToFunctionCallOutputItems(t *testing.T) {
	upResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_1","model":"GLM-4.7","choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":1}"}}]}}]}`,
		)),
	}
	rec := &responseRecorder{header: make(http.Header)}
	p := &responsesCompatProxy{}
	p.forwardNonStream(rec, upResp)

	if rec.status != 0 && rec.status != 200 {
		t.Fatalf("unexpected status: %d", rec.status)
	}
	body := rec.body.String()
	if !strings.Contains(body, `"type":"function_call"`) {
		t.Fatalf("expected function_call output item, got %q", body)
	}
	if !strings.Contains(body, `"name":"sum"`) {
		t.Fatalf("expected tool name in output, got %q", body)
	}
}

func TestForwardNonStream_MapsUsageDetails(t *testing.T) {
	upResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_1","model":"GLM-4.7","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":2}}}`,
		)),
	}
	rec := &responseRecorder{header: make(http.Header)}
	p := &responsesCompatProxy{}
	p.forwardNonStream(rec, upResp)

	body := rec.body.String()
	if !strings.Contains(body, `"usage"`) {
		t.Fatalf("expected usage in non-stream response, got %q", body)
	}
	if !strings.Contains(body, `"input_tokens":10`) || !strings.Contains(body, `"output_tokens":4`) || !strings.Contains(body, `"total_tokens":14`) {
		t.Fatalf("expected mapped usage tokens, got %q", body)
	}
	if !strings.Contains(body, `"input_tokens_details":{"cached_tokens":3}`) {
		t.Fatalf("expected cached usage details, got %q", body)
	}
	if !strings.Contains(body, `"output_tokens_details":{"reasoning_tokens":2}`) {
		t.Fatalf("expected reasoning usage details, got %q", body)
	}
}

func TestDecodeResponsesRequest_GzipJSON(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(`{"model":"GLM-4.7","input":"hi"}`))
	_ = zw.Close()

	r := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(buf.Bytes()))
	r.Header.Set("Content-Encoding", "gzip")
	req, raw, err := decodeResponsesRequest(r)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !strings.Contains(raw, `"model":"GLM-4.7"`) {
		t.Fatalf("raw mismatch: %q", raw)
	}
	if req["model"] != "GLM-4.7" {
		t.Fatalf("model mismatch: %#v", req)
	}
}

func TestDecodeResponsesRequest_InvalidBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`not-json`))
	_, raw, err := decodeResponsesRequest(r)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if raw == "" {
		t.Fatal("expected raw body in error case")
	}
}

func TestDecodeResponsesRequest_ZstdJSON(t *testing.T) {
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd writer failed: %v", err)
	}
	_, _ = zw.Write([]byte(`{"model":"GLM-4.7","input":"hi"}`))
	zw.Close()

	r := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(buf.Bytes()))
	r.Header.Set("Content-Encoding", "zstd")
	req, raw, err := decodeResponsesRequest(r)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !strings.Contains(raw, `"model":"GLM-4.7"`) {
		t.Fatalf("raw mismatch: %q", raw)
	}
	if req["model"] != "GLM-4.7" {
		t.Fatalf("model mismatch: %#v", req)
	}
}

func TestWriteUpstreamErrorAsJSON_WrapsPlainText(t *testing.T) {
	upResp := &http.Response{
		StatusCode: 400,
		Body:       io.NopCloser(strings.NewReader("invalid json")),
	}
	rec := &responseRecorder{header: make(http.Header)}
	writeUpstreamErrorAsJSON(rec, upResp)

	if rec.status != 400 {
		t.Fatalf("status mismatch: %d", rec.status)
	}
	if !strings.Contains(rec.body.String(), `"error"`) {
		t.Fatalf("expected json error body, got %q", rec.body.String())
	}
	if !strings.Contains(rec.body.String(), "invalid json") {
		t.Fatalf("expected original error message, got %q", rec.body.String())
	}
}

func TestNormalizeMessageContent_MapText(t *testing.T) {
	raw := map[string]any{
		"type": "text",
		"text": "你好",
	}
	got := normalizeMessageContent(raw)
	if got != "你好" {
		t.Fatalf("expected map text content, got %q", got)
	}
}

func TestExtractChatText_FallbackChoiceText(t *testing.T) {
	resp := map[string]any{
		"choices": []any{
			map[string]any{
				"text": "hello",
			},
		},
	}
	got := extractChatText(resp)
	if got != "hello" {
		t.Fatalf("expected fallback choice text, got %q", got)
	}
}

func TestExtractChatDelta_DeltaTextString(t *testing.T) {
	chunk := map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"text": "你好",
				},
			},
		},
	}
	got := extractChatDelta(chunk)
	if got != "你好" {
		t.Fatalf("expected delta text, got %q", got)
	}
}

func TestForwardStream_FallbackForSingleJSONLine(t *testing.T) {
	upResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_1","model":"GLM-4.7","choices":[{"message":{"content":"你好"}}]}` + "\n",
		)),
	}
	rec := &flushResponseRecorder{responseRecorder: responseRecorder{header: make(http.Header)}}
	p := &responsesCompatProxy{}
	p.forwardStream(rec, upResp)

	body := rec.body.String()
	if !strings.Contains(body, `"response.output_text.done"`) {
		t.Fatalf("expected done event, got %q", body)
	}
	if !strings.Contains(body, "你好") {
		t.Fatalf("expected output text in stream response, got %q", body)
	}
}

func TestForwardStream_EmitsFunctionCallEventsFromToolCallDeltas(t *testing.T) {
	upResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"id":"chatcmpl_1","model":"GLM-4.7","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":"}}]}}]}`,
			`data: {"id":"chatcmpl_1","model":"GLM-4.7","choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"arguments":"1}"}}]}}]}`,
			`data: [DONE]`,
			``,
		}, "\n"))),
	}
	rec := &flushResponseRecorder{responseRecorder: responseRecorder{header: make(http.Header)}}
	p := &responsesCompatProxy{}
	p.forwardStream(rec, upResp)

	body := rec.body.String()
	if !strings.Contains(body, `"response.function_call_arguments.delta"`) {
		t.Fatalf("expected function_call argument delta event, got %q", body)
	}
	if !strings.Contains(body, `"type":"function_call"`) {
		t.Fatalf("expected function_call output item in stream, got %q", body)
	}
	if !strings.Contains(body, `"call_id":"call_1"`) {
		t.Fatalf("expected call_id in stream output, got %q", body)
	}
}

func TestForwardStream_ResponseCompletedIncludesUsageDetails(t *testing.T) {
	upResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"id":"chatcmpl_1","model":"GLM-4.7","choices":[{"delta":{"content":"he"}}]}`,
			`data: {"id":"chatcmpl_1","model":"GLM-4.7","choices":[{"delta":{"content":"llo"}}]}`,
			`data: {"id":"chatcmpl_1","model":"GLM-4.7","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":2}}}`,
			`data: [DONE]`,
			``,
		}, "\n"))),
	}
	rec := &flushResponseRecorder{responseRecorder: responseRecorder{header: make(http.Header)}}
	p := &responsesCompatProxy{}
	p.forwardStream(rec, upResp)

	body := rec.body.String()
	if !strings.Contains(body, `"type":"response.completed"`) {
		t.Fatalf("expected response.completed event, got %q", body)
	}
	if !strings.Contains(body, `"usage":{"input_tokens":12`) {
		t.Fatalf("expected usage tokens in completed event, got %q", body)
	}
	if !strings.Contains(body, `"input_tokens_details":{"cached_tokens":4}`) {
		t.Fatalf("expected cached usage details in completed event, got %q", body)
	}
	if !strings.Contains(body, `"output_tokens_details":{"reasoning_tokens":2}`) {
		t.Fatalf("expected reasoning usage details in completed event, got %q", body)
	}
}

func TestChatUsageToResponsesUsage_MapsDetails(t *testing.T) {
	payload := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(4),
			"total_tokens":      float64(14),
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(3),
			},
			"completion_tokens_details": map[string]any{
				"reasoning_tokens": float64(2),
			},
		},
	}
	got, ok := chatUsageToResponsesUsage(payload)
	if !ok {
		t.Fatal("expected usage mapping")
	}
	if intFromAny(got["input_tokens"]) != 10 || intFromAny(got["output_tokens"]) != 4 || intFromAny(got["total_tokens"]) != 14 {
		t.Fatalf("unexpected token mapping: %#v", got)
	}
	if intFromAny(mapValue(got["input_tokens_details"])["cached_tokens"]) != 3 {
		t.Fatalf("expected cached_tokens in input_tokens_details, got %#v", got["input_tokens_details"])
	}
	if intFromAny(mapValue(got["output_tokens_details"])["reasoning_tokens"]) != 2 {
		t.Fatalf("expected reasoning_tokens in output_tokens_details, got %#v", got["output_tokens_details"])
	}
}

func TestMergeResponsesUsage_PrefersIncomingNonZero(t *testing.T) {
	base := map[string]any{
		"input_tokens":  float64(10),
		"output_tokens": float64(0),
		"input_tokens_details": map[string]any{
			"cached_tokens": float64(2),
		},
	}
	incoming := map[string]any{
		"output_tokens": float64(5),
		"total_tokens":  float64(15),
		"output_tokens_details": map[string]any{
			"reasoning_tokens": float64(1),
		},
	}
	got := mergeResponsesUsage(base, incoming)
	if intFromAny(got["input_tokens"]) != 10 {
		t.Fatalf("expected input_tokens=10, got %#v", got)
	}
	if intFromAny(got["output_tokens"]) != 5 {
		t.Fatalf("expected output_tokens=5, got %#v", got)
	}
	if intFromAny(got["total_tokens"]) != 15 {
		t.Fatalf("expected total_tokens=15, got %#v", got)
	}
	if intFromAny(mapValue(got["input_tokens_details"])["cached_tokens"]) != 2 {
		t.Fatalf("expected cached_tokens=2, got %#v", got)
	}
	if intFromAny(mapValue(got["output_tokens_details"])["reasoning_tokens"]) != 1 {
		t.Fatalf("expected reasoning_tokens=1, got %#v", got)
	}
}

type responseRecorder struct {
	header http.Header
	body   strings.Builder
	status int
}

type flushResponseRecorder struct {
	responseRecorder
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	return r.body.Write(data)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
}

func (r *flushResponseRecorder) Flush() {}
