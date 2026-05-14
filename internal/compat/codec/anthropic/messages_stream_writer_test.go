package anthropic

import (
	"strings"
	"testing"
)

func TestWriteMessagesStreamWritesTextAndToolUseDeltas(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think "}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":"}}]}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`,
		`data: [DONE]`,
	}, "\n")

	var out strings.Builder
	result := WriteMessagesStream(&out, strings.NewReader(stream), "", nil)
	got := out.String()

	if !strings.Contains(got, "event: message_start") {
		t.Fatalf("missing message_start: %q", got)
	}
	if !strings.Contains(got, `"type":"thinking"`) || !strings.Contains(got, `"type":"thinking_delta"`) || !strings.Contains(got, `"thinking":"think "`) {
		t.Fatalf("missing thinking delta: %q", got)
	}
	if !strings.Contains(got, `"text":"Hel"`) {
		t.Fatalf("missing text delta: %q", got)
	}
	if !strings.Contains(got, `"type":"tool_use"`) || !strings.Contains(got, `"type":"input_json_delta"`) || !strings.Contains(got, `"id":"call_1"`) {
		t.Fatalf("missing tool_use delta: %q", got)
	}
	if !strings.Contains(got, `"stop_reason":"tool_use"`) || !strings.Contains(got, "event: message_stop") {
		t.Fatalf("missing stop events: %q", got)
	}
	if result.ReasoningText != "think " {
		t.Fatalf("reasoning mismatch: %#v", result)
	}
	if len(result.ToolCallIDs) != 1 || result.ToolCallIDs[0] != "call_1" {
		t.Fatalf("tool call ids mismatch: %#v", result.ToolCallIDs)
	}
}

func TestWriteMessagesStreamFallsBackToFullResponse(t *testing.T) {
	stream := `{"id":"chatcmpl_1","model":"gpt-4.1","choices":[{"message":{"content":"hello","tool_calls":[{"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`

	var out strings.Builder
	result := WriteMessagesStream(&out, strings.NewReader(stream), "", nil)
	got := out.String()

	if !strings.Contains(got, "event: message_start") || !strings.Contains(got, `"text":"hello"`) {
		t.Fatalf("missing fallback text stream: %q", got)
	}
	if !strings.Contains(got, `"name":"sum"`) || !strings.Contains(got, `"partial_json":"{\"a\":1}"`) {
		t.Fatalf("missing fallback tool stream: %q", got)
	}
	if result.EmptyStream {
		t.Fatalf("expected fallback stream result, got %#v", result)
	}
	if len(result.ToolCallIDs) != 1 || result.ToolCallIDs[0] != "call_1" {
		t.Fatalf("tool call ids mismatch: %#v", result.ToolCallIDs)
	}
}
