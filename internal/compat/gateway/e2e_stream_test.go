package gateway

import (
	"context"
	"encoding/json"
	"fmt"
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
	streamOptions, ok := captured["stream_options"].(map[string]any)
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream_options.include_usage missing: %#v", captured["stream_options"])
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

func TestAnthropicMessagesStreamE2EPreservesContextAndLogsRedactedUsage(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"chatcmpl_audit","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"plan "}}]}`,
		`data: {"id":"chatcmpl_audit","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"id":"chatcmpl_audit","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"lo"}}]}`,
		`data: {"id":"chatcmpl_audit","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_sum","type":"function","function":{"name":"sum","arguments":"{\"a\":"}}]}}]}`,
		`data: {"id":"chatcmpl_audit","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1,\"b\":2}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":42,"completion_tokens":7,"total_tokens":49,"prompt_tokens_details":{"cached_tokens":32}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	var captured map[string]any
	var logs []string
	handler := AnthropicMessagesHandler{
		PreferredModel: "mimo-v2.5-pro",
		Logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return chatStreamResponse(upstream), nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"mimo-v2.5-pro",
		"max_tokens":512,
		"stream":true,
		"system":"private system prompt",
		"thinking":{"type":"enabled","budget_tokens":128},
		"messages":[
			{"role":"user","content":[{"type":"text","text":"add private numbers"}]},
			{"role":"assistant","content":[{"type":"thinking","thinking":"private prior reasoning"},{"type":"tool_use","id":"call_prev","name":"lookup","input":{"query":"private lookup input"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_prev","content":"{\"secret\":\"private tool result\"}"}]}
		],
		"tools":[{
			"name":"sum",
			"description":"private sum tool description",
			"input_schema":{"type":"object","properties":{"a":{"type":"number","description":"private a description"},"b":{"type":"number"}}}
		}],
		"tool_choice":{"type":"tool","name":"sum"}
	}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	assertTranslatedChatRequestPreservesAnthropicContext(t, captured)
	assertAnthropicStreamOutputPreservesOpenAIChatDeltas(t, rec.Body.String())
	assertAnthropicCompatLogsAreRedactedAndIncludeUsage(t, strings.Join(logs, "\n"))
}

func assertTranslatedChatRequestPreservesAnthropicContext(t *testing.T, captured map[string]any) {
	t.Helper()
	if captured == nil {
		t.Fatal("upstream post did not receive translated chat request")
	}
	if captured["model"] != "mimo-v2.5-pro" || captured["stream"] != true || captured["max_tokens"] != 512 {
		t.Fatalf("unexpected translated request controls: %#v", captured)
	}
	streamOptions, ok := captured["stream_options"].(map[string]any)
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream_options.include_usage missing: %#v", captured["stream_options"])
	}
	thinking, ok := captured["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" || thinking["budget_tokens"] != 128 {
		t.Fatalf("thinking config not preserved: %#v", captured["thinking"])
	}
	msgs, ok := captured["messages"].([]map[string]any)
	if !ok || len(msgs) != 4 {
		t.Fatalf("expected system/user/assistant/tool messages, got %#v", captured["messages"])
	}
	if roles := []any{msgs[0]["role"], msgs[1]["role"], msgs[2]["role"], msgs[3]["role"]}; fmt.Sprint(roles) != "[system user assistant tool]" {
		t.Fatalf("unexpected mapped message roles: %#v", msgs)
	}
	if msgs[0]["content"] != "private system prompt" || msgs[1]["content"] != "add private numbers" {
		t.Fatalf("text context was not preserved: %#v", msgs)
	}
	if msgs[2]["reasoning_content"] != "private prior reasoning" {
		t.Fatalf("assistant reasoning not preserved: %#v", msgs[2])
	}
	toolCalls, ok := msgs[2]["tool_calls"].([]map[string]any)
	if !ok || len(toolCalls) != 1 || toolCalls[0]["id"] != "call_prev" {
		t.Fatalf("assistant tool_use not mapped to tool_calls: %#v", msgs[2]["tool_calls"])
	}
	fn := toolCalls[0]["function"].(map[string]any)
	if fn["name"] != "lookup" || !strings.Contains(fmt.Sprint(fn["arguments"]), "private lookup input") {
		t.Fatalf("tool call function not preserved: %#v", fn)
	}
	if msgs[3]["role"] != "tool" || msgs[3]["tool_call_id"] != "call_prev" || !strings.Contains(fmt.Sprint(msgs[3]["content"]), "private tool result") {
		t.Fatalf("tool_result not mapped to tool message: %#v", msgs[3])
	}
	tools, ok := captured["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected one mapped tool, got %#v", captured["tools"])
	}
	toolFn := tools[0]["function"].(map[string]any)
	if toolFn["name"] != "sum" || toolFn["description"] != "private sum tool description" {
		t.Fatalf("tool definition not preserved: %#v", toolFn)
	}
	params := toolFn["parameters"].(map[string]any)
	if params["type"] != "object" || !strings.Contains(fmt.Sprint(params), "private a description") {
		t.Fatalf("tool schema not preserved: %#v", params)
	}
	choice := captured["tool_choice"].(map[string]any)
	if choice["type"] != "function" || choice["function"].(map[string]any)["name"] != "sum" {
		t.Fatalf("tool_choice not mapped: %#v", choice)
	}
}

func assertAnthropicStreamOutputPreservesOpenAIChatDeltas(t *testing.T, body string) {
	t.Helper()
	assertOrderedSubstrings(t, body, []string{
		"event: message_start",
		`"type":"thinking_delta"`,
		`"type":"text_delta"`,
		`"type":"tool_use"`,
		`"stop_reason":"tool_use"`,
		"event: message_stop",
	})
	events := anthropicSSEEvents(t, body)
	if len(events) == 0 {
		t.Fatal("no anthropic SSE events decoded")
	}
	var gotUsage map[string]any
	for _, event := range events {
		if event["type"] == "message_delta" {
			gotUsage, _ = event["usage"].(map[string]any)
		}
	}
	if gotUsage == nil {
		t.Fatalf("message_delta usage missing in SSE body: %s", body)
	}
	for key, want := range map[string]int{
		"input_tokens":            42,
		"output_tokens":           7,
		"cache_read_input_tokens": 32,
	} {
		if got := intFromAny(gotUsage[key]); got != want {
			t.Fatalf("SSE usage %s=%d want %d in %#v", key, got, want, gotUsage)
		}
	}
	if !strings.Contains(body, `"thinking":"plan "`) {
		t.Fatalf("thinking delta text missing in SSE body: %s", body)
	}
	for _, want := range []string{
		`"text":"Hel"`,
		`"text":"lo"`,
		`"id":"call_sum"`,
		`"name":"sum"`,
		`"partial_json":"{\"a\":"`,
		`"partial_json":"1,\"b\":2}"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE fragment %s missing in body: %s", want, body)
		}
	}
}

func assertAnthropicCompatLogsAreRedactedAndIncludeUsage(t *testing.T, logs string) {
	t.Helper()
	for _, want := range []string{
		"middleware stage=anthropic.request",
		"middleware stage=ir.request",
		"middleware stage=openai_chat.request",
		"stream parse flags",
		"middleware stage=anthropic.stream usage input=42 output=7 total=49 cached=32 cache_creation=0 reasoning=0",
		`raw_usage={"cache_read_input_tokens":32,"input_tokens":42,"output_tokens":7}`,
		`"<text len=21>"`,
		`"<description len=28>"`,
		`"<input items=1>"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("missing log fragment %s in logs:\n%s", want, logs)
		}
	}
	for _, leaked := range []string{
		"private system prompt",
		"add private numbers",
		"private prior reasoning",
		"private lookup input",
		"private tool result",
		"private sum tool description",
		"private a description",
	} {
		if strings.Contains(logs, leaked) {
			t.Fatalf("compat log leaked %q in logs:\n%s", leaked, logs)
		}
	}
}

func anthropicSSEEvents(t *testing.T, body string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode SSE data %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}
