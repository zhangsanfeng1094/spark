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

	reasoningfeature "spark/internal/compat/gateway/features/reasoning"
)

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
	handler := NewAnthropicMessagesToOpenAIChatHandler(AnthropicMessagesOptions{
		PreferredModel: "mimo-v2.5-pro",
		Logf:           func(string, ...any) {},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return chatStreamResponse(upstream), nil
		},
	})
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

func TestAnthropicMessagesHandlerDropsUnsupportedReasoningEffort(t *testing.T) {
	var captured map[string]any
	handler := NewAnthropicMessagesToOpenAIChatHandler(AnthropicMessagesOptions{
		PreferredModel: "glm-5.1",
		UpstreamBase:   "https://gateway.example/v1",
		Logf:           func(string, ...any) {},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"glm-5.1","choices":[{"message":{"content":"ok"}}]}`)),
			}, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"glm-5.1",
		"max_tokens":128,
		"output_config":{"effort":"high"},
		"messages":[{"role":"user","content":"hello"}]
	}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := captured["reasoning_effort"]; ok {
		t.Fatalf("did not expect reasoning_effort for unsupported upstream: %#v", captured)
	}
}

func TestAnthropicMessagesHandlerStripsReasoningContentForGenericTarget(t *testing.T) {
	var captured map[string]any
	handler := NewAnthropicMessagesToOpenAIChatHandler(AnthropicMessagesOptions{
		PreferredModel: "gpt-4.1",
		UpstreamBase:   "https://api.openai.com/v1",
		Logf:           func(string, ...any) {},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"gpt-4.1","choices":[{"message":{"content":"ok"}}]}`)),
			}, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"gpt-4.1",
		"max_tokens":128,
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"think first"},
				{"type":"tool_use","id":"call_1","name":"sum","input":{"a":1}}
			]}
		]
	}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	msgs := captured["messages"].([]map[string]any)
	if _, ok := msgs[0]["reasoning_content"]; ok {
		t.Fatalf("did not expect reasoning_content for generic target: %#v", msgs[0])
	}
}

func TestAnthropicMessagesHandlerKeepsReasoningContentForMimoTarget(t *testing.T) {
	var captured map[string]any
	handler := NewAnthropicMessagesToOpenAIChatHandler(AnthropicMessagesOptions{
		PreferredModel: "mimo-v2.5-pro",
		UpstreamBase:   "https://gateway.example/v1",
		Logf:           func(string, ...any) {},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"message":{"content":"ok"}}]}`)),
			}, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"mimo-v2.5-pro",
		"max_tokens":128,
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"think first"},
				{"type":"tool_use","id":"call_1","name":"sum","input":{"a":1}}
			]}
		]
	}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	msgs := captured["messages"].([]map[string]any)
	if msgs[0]["reasoning_content"] != "think first" {
		t.Fatalf("expected reasoning_content for MiMo target: %#v", msgs[0])
	}
}

func TestAnthropicMessagesHandlerRestoresCachedReasoningContent(t *testing.T) {
	var cache reasoningfeature.ReasoningCache
	cache.RememberForToolCallIDs([]string{"call_1"}, "cached think")
	var captured map[string]any
	handler := NewAnthropicMessagesToOpenAIChatHandler(AnthropicMessagesOptions{
		PreferredModel: "mimo-v2.5-pro",
		UpstreamBase:   "https://gateway.example/v1",
		ReasoningCache: &cache,
		Logf:           func(string, ...any) {},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"message":{"content":"ok"}}]}`)),
			}, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"mimo-v2.5-pro",
		"max_tokens":128,
		"messages":[
			{"role":"assistant","content":[
				{"type":"tool_use","id":"call_1","name":"sum","input":{"a":1}}
			]}
		]
	}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	msgs := captured["messages"].([]map[string]any)
	if msgs[0]["reasoning_content"] != "cached think" {
		t.Fatalf("expected cached reasoning_content: %#v", msgs[0])
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
	handler := NewAnthropicMessagesToOpenAIChatHandler(AnthropicMessagesOptions{
		PreferredModel: "mimo-v2.5-pro",
		Logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return chatStreamResponse(upstream), nil
		},
	})
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

func TestAnthropicMessagesHandlerPreservesCacheControlBreakpoints(t *testing.T) {
	var captured map[string]any
	handler := NewAnthropicMessagesToOpenAIChatHandler(AnthropicMessagesOptions{
		PreferredModel: "mimo-v2.5-pro",
		UpstreamBase:  "https://gateway.example/v1",
		Logf:          func(string, ...any) {},
		PostChatCompletions: func(_ context.Context, chatReq map[string]any) (*http.Response, error) {
			captured = chatReq
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"message":{"content":"ok"}}]}`)),
			}, nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"mimo-v2.5-pro",
		"max_tokens":128,
		"system":[
			{"type":"text","text":"base system"},
			{"type":"text","text":"dynamic system","cache_control":{"type":"ephemeral"}}
		],
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"hello"},{"type":"text","text":"cached hint","cache_control":{"type":"ephemeral"}}
			]},
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"sum","input":{"a":1}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"{\"ok\":true}","cache_control":{"type":"ephemeral"}}]}
		],
		"tools":[{"name":"sum","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}]
	}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}

	msgs, ok := captured["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("expected messages array, got %#v", captured["messages"])
	}
	// Layout: [system, user, assistant, tool]. cache_control lives on the
	// individual content blocks, mirroring Anthropic's explicit breakpoint
	// format (see docs.claude.com prompt-caching).
	if len(msgs) != 4 {
		t.Fatalf("expected 4 chat messages, got %d: %#v", len(msgs), msgs)
	}
	if msgs[0]["role"] != "system" || msgs[1]["role"] != "user" || msgs[2]["role"] != "assistant" || msgs[3]["role"] != "tool" {
		t.Fatalf("unexpected mapped message roles: %#v", msgs)
	}

	// system message: content is an array of text blocks with per-block cache_control
	sysBlocks, ok := msgs[0]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected system content as block array (cache_control present), got %#v", msgs[0]["content"])
	}
	if len(sysBlocks) != 2 || sysBlocks[0]["text"] != "base system" || sysBlocks[1]["text"] != "dynamic system" {
		t.Fatalf("unexpected system blocks: %#v", sysBlocks)
	}
	if sysBlocks[0]["cache_control"] != nil {
		t.Fatalf("first system block had no cache_control in anthropic request, must stay absent: %#v", sysBlocks[0])
	}
	if sysBlocks[1]["cache_control"] == nil {
		t.Fatalf("expected cache_control on second system block: %#v", sysBlocks[1])
	}

	// user message: content array with per-block cache_control
	userBlocks, ok := msgs[1]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected user content as block array (cache_control present), got %#v", msgs[1]["content"])
	}
	if len(userBlocks) != 2 || userBlocks[0]["text"] != "hello" || userBlocks[1]["text"] != "cached hint" {
		t.Fatalf("unexpected user blocks: %#v", userBlocks)
	}
	if userBlocks[1]["cache_control"] == nil {
		t.Fatalf("expected cache_control on cached hint block: %#v", userBlocks[1])
	}

	// assistant message: string content (chat completions), never carries cache_control
	if _, isStr := msgs[2]["content"].(string); !isStr {
		t.Fatalf("assistant content should stay string (chat completions), got %#v", msgs[2]["content"])
	}
	if msgs[2]["cache_control"] != nil {
		t.Fatalf("assistant message must not carry cache_control: %#v", msgs[2])
	}

	// tool message: array content with cache_control on the block
	toolBlocks, ok := msgs[3]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected tool content as block array (cache_control present), got %#v", msgs[3]["content"])
	}
	if len(toolBlocks) != 1 || toolBlocks[0]["cache_control"] == nil {
		t.Fatalf("expected cache_control on tool result block: %#v", toolBlocks)
	}

	// tools carry cache_control
	tools, ok := captured["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected one tool with cache_control, got %#v", captured["tools"])
	}
	if tools[0]["cache_control"] == nil {
		t.Fatalf("expected cache_control on tool definition: %#v", tools[0])
	}
}
