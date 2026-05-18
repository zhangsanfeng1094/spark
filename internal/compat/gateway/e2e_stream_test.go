package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestCodexResponsesStreamE2ETranslatesRequestAndWritesResponsesSSE(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"chatcmpl_e2e","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think"}}]}`,
		`data: {"id":"chatcmpl_e2e","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hi"}}]}`,
		`data: {"id":"chatcmpl_e2e","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":"}}]}}]}`,
		`data: {"id":"chatcmpl_e2e","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	executor := &streamE2EExecutor{body: upstream}
	handler := CodexResponsesHandler{
		Mode:         ResponsesModeChatCompletionsOnly,
		Route:        Route{Client: ClientCodexResponses, Target: TargetOpenAIChat},
		UpstreamBase: "https://token-plan-sgp.xiaomimimo.com/v1",
		Logf:         func(string, ...any) {},
		Warnf:        func(string) {},
		Executor:     executor,
		PrepareChat:  ChatReasoningAdapter{UpstreamBase: "https://token-plan-sgp.xiaomimimo.com/v1"}.ApplyToChatRequest,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"mimo-v2.5-pro",
		"stream":true,
		"instructions":"be terse",
		"input":[
			{"role":"user","content":[{"type":"input_text","text":"add one"}]},
			{"type":"reasoning","summary":[{"type":"summary_text","text":"used lookup"}]},
			{"type":"function_call","call_id":"call_prev","name":"lookup","arguments":"{\"x\":1}"},
			{"type":"function_call_output","call_id":"call_prev","output":"{\"ok\":true}"}
		],
		"tools":[{"type":"function","name":"sum","parameters":{"type":"object","properties":{"a":{"type":"number"}}}}],
		"tool_choice":"auto"
	}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	if executor.chatReq == nil {
		t.Fatal("executor did not receive translated chat request")
	}
	if executor.chatReq["stream"] != true {
		t.Fatalf("expected stream chat request, got %#v", executor.chatReq["stream"])
	}
	if tools, ok := executor.chatReq["tools"].([]map[string]any); !ok || len(tools) != 1 {
		t.Fatalf("expected one mapped tool, got %#v", executor.chatReq["tools"])
	}
	msgs, ok := executor.chatReq["messages"].([]map[string]any)
	if !ok || len(msgs) != 4 {
		t.Fatalf("expected system/user/assistant/tool messages, got %#v", executor.chatReq["messages"])
	}
	if msgs[0]["role"] != "system" || msgs[1]["role"] != "user" || msgs[2]["role"] != "assistant" || msgs[3]["role"] != "tool" {
		t.Fatalf("unexpected mapped message roles: %#v", msgs)
	}
	body := rec.Body.String()
	assertOrderedSubstrings(t, body, []string{
		`"type":"response.created"`,
		`"type":"response.reasoning_summary_text.delta"`,
		`"type":"response.output_text.delta"`,
		`"type":"response.function_call_arguments.delta"`,
		`"type":"response.function_call_arguments.done"`,
		`"type":"response.completed"`,
		`data: [DONE]`,
	})
	for _, want := range []string{
		`"delta":"Hi"`,
		`"call_id":"call_1"`,
		`"name":"sum"`,
		`"arguments":"{\"a\":1}"`,
		`"input_tokens":2`,
		`"output_tokens":3`,
		`"total_tokens":5`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in response stream: %s", want, body)
		}
	}
}

func TestAnthropicMessagesStreamE2ETranslatesRequestAndWritesMessagesSSE(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"chatcmpl_e2e","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think "}}]}`,
		`data: {"id":"chatcmpl_e2e","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"id":"chatcmpl_e2e","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"lo"}}]}`,
		`data: {"id":"chatcmpl_e2e","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":1}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")
	var captured map[string]any
	handler := AnthropicMessagesHandler{
		PreferredModel: "mimo-v2.5-pro",
		Logf:           func(string, ...any) {},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return chatStreamResponse(upstream), nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"mimo-v2.5-pro",
		"max_tokens":128,
		"stream":true,
		"system":"be terse",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"add one"}]},
			{"role":"assistant","content":[{"type":"thinking","thinking":"used lookup"},{"type":"tool_use","id":"call_prev","name":"lookup","input":{"x":1}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_prev","content":"{\"ok\":true}"}]}
		],
		"tools":[{"name":"sum","input_schema":{"type":"object","properties":{"a":{"type":"number"}}}}],
		"tool_choice":{"type":"tool","name":"sum"}
	}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	if captured == nil {
		t.Fatal("upstream post did not receive translated chat request")
	}
	if captured["stream"] != true || captured["model"] != "mimo-v2.5-pro" {
		t.Fatalf("unexpected chat request stream/model: %#v", captured)
	}
	msgs, ok := captured["messages"].([]map[string]any)
	if !ok || len(msgs) != 4 {
		t.Fatalf("expected system/user/assistant/tool messages, got %#v", captured["messages"])
	}
	if msgs[0]["role"] != "system" || msgs[1]["role"] != "user" || msgs[2]["role"] != "assistant" || msgs[3]["role"] != "tool" {
		t.Fatalf("unexpected mapped message roles: %#v", msgs)
	}
	body := rec.Body.String()
	assertOrderedSubstrings(t, body, []string{
		"event: message_start",
		`"type":"content_block_start"`,
		`"type":"thinking_delta"`,
		`"type":"text_delta"`,
		`"type":"tool_use"`,
		`"type":"input_json_delta"`,
		"event: message_delta",
		"event: message_stop",
	})
	for _, want := range []string{
		`"text":"Hel"`,
		`"text":"lo"`,
		`"id":"call_1"`,
		`"name":"sum"`,
		`"stop_reason":"tool_use"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in anthropic stream: %s", want, body)
		}
	}
}
