package gateway

import (
	"strings"
	"testing"

	"spark/internal/compat/client/codex"
)

func TestWriteCodexResponsesStreamFromOpenAIChatEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		check  func(t *testing.T, output string, result codex.ResponsesStreamResult)
	}{
		{
			name:   "empty stream completes without upstream chunks",
			stream: "",
			check: func(t *testing.T, output string, result codex.ResponsesStreamResult) {
				if !strings.Contains(output, `"type":"response.created"`) {
					t.Fatalf("missing response.created event: %s", output)
				}
				if !strings.Contains(output, `"type":"response.completed"`) {
					t.Fatalf("missing response.completed event: %s", output)
				}
			},
		},
		{
			name:   "done without content marks upstream done",
			stream: "data: [DONE]\n",
			check: func(t *testing.T, output string, result codex.ResponsesStreamResult) {
				if !result.SawDone {
					t.Fatalf("expected SawDone, got %#v", result)
				}
				if result.ChunkCount != 0 {
					t.Fatalf("expected no valid chunks, got %#v", result)
				}
				if !strings.Contains(output, `"type":"response.completed"`) {
					t.Fatalf("missing response.completed event: %s", output)
				}
			},
		},
		{
			name:   "usage only chunk preserves usage",
			stream: `data: {"id":"chatcmpl_usage","model":"mimo-v2.5-pro","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}` + "\n",
			check: func(t *testing.T, output string, result codex.ResponsesStreamResult) {
				if result.ChunkCount != 1 {
					t.Fatalf("expected one valid chunk, got %#v", result)
				}
				if got := result.Usage["input_tokens"]; got != 3 {
					t.Fatalf("input token usage mismatch: %#v", result.Usage)
				}
				if !strings.Contains(output, `"input_tokens":3`) ||
					!strings.Contains(output, `"output_tokens":2`) ||
					!strings.Contains(output, `"total_tokens":5`) {
					t.Fatalf("missing completed usage: %s", output)
				}
			},
		},
		{
			name:   "malformed data before first chunk is explicit error",
			stream: "data: {not-json}\n",
			check: func(t *testing.T, output string, result codex.ResponsesStreamResult) {
				if !result.HandledError || result.ScanErr == nil {
					t.Fatalf("expected handled parse error, got %#v", result)
				}
				if !strings.Contains(output, `"type":"upstream_stream_error"`) {
					t.Fatalf("missing upstream stream error: %s", output)
				}
			},
		},
		{
			name: "malformed data after first chunk is ignored",
			stream: strings.Join([]string{
				`data: {"id":"chatcmpl_text","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hi"}}]}`,
				`data: {not-json}`,
				`data: [DONE]`,
			}, "\n"),
			check: func(t *testing.T, output string, result codex.ResponsesStreamResult) {
				if result.HandledError {
					t.Fatalf("did not expect handled error after a valid chunk: %#v", result)
				}
				if result.ExtractedTextLen != 2 || !result.SawContentDelta {
					t.Fatalf("text delta was not preserved: %#v", result)
				}
				if !strings.Contains(output, `"delta":"Hi"`) {
					t.Fatalf("missing text delta: %s", output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			result := WriteCodexResponsesStreamFromOpenAIChat(&output, strings.NewReader(tt.stream), nil)
			tt.check(t, output.String(), result)
		})
	}
}

func TestWriteCodexResponsesStreamFromOpenAIChatOrdersCoreEvents(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_order","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think"}}]}`,
		`data: {"id":"chatcmpl_order","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hi"}}]}`,
		`data: {"id":"chatcmpl_order","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":"}}]}}]}`,
		`data: {"id":"chatcmpl_order","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}`,
		`data: [DONE]`,
	}, "\n")

	var output strings.Builder
	result := WriteCodexResponsesStreamFromOpenAIChat(&output, strings.NewReader(stream), nil)

	if result.ChunkCount != 4 || !result.SawDone || !result.SawContentDelta || result.ReasoningLen != 5 {
		t.Fatalf("unexpected stream result: %#v", result)
	}
	assertOrderedSubstrings(t, output.String(), []string{
		`"type":"response.created"`,
		`"type":"response.reasoning_summary_text.delta"`,
		`"type":"response.output_text.delta"`,
		`"type":"response.function_call_arguments.delta"`,
		`"type":"response.function_call_arguments.delta"`,
		`"type":"response.function_call_arguments.done"`,
		`"type":"response.completed"`,
		`data: [DONE]`,
	})
	if !strings.Contains(output.String(), `"arguments":"{\"a\":1}"`) {
		t.Fatalf("split tool call arguments were not joined: %s", output.String())
	}
}

func assertOrderedSubstrings(t *testing.T, s string, parts []string) {
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
