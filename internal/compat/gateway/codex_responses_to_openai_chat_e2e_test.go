package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	handler := NewCodexResponsesHandler(CodexResponsesOptions{
		Mode:         ResponsesModeChatCompletionsOnly,
		UpstreamBase: "https://token-plan-sgp.xiaomimimo.com/v1",
		Logf:         func(string, ...any) {},
		Warnf:        func(string) {},
		Executor:     executor,
	})
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
