package bridge

import (
	"strings"

	"spark/internal/compat/gateway/core"
	"testing"
)

func TestRouteStreamOpenAIChatEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		check  func(t *testing.T, output string, result StreamResult)
	}{
		{
			name:   "empty stream completes without upstream chunks",
			stream: "",
			check: func(t *testing.T, output string, result StreamResult) {
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
			check: func(t *testing.T, output string, result StreamResult) {
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
			check: func(t *testing.T, output string, result StreamResult) {
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
			check: func(t *testing.T, output string, result StreamResult) {
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
			check: func(t *testing.T, output string, result StreamResult) {
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
			selection, err := SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
			if err != nil {
				t.Fatalf("select route: %v", err)
			}
			result := selection.Stream(&output, strings.NewReader(tt.stream), nil)
			tt.check(t, output.String(), result)
		})
	}
}

func TestRouteStreamOpenAIChatOrdersCoreEvents(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_order","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think"}}]}`,
		`data: {"id":"chatcmpl_order","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hi"}}]}`,
		`data: {"id":"chatcmpl_order","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":"}}]}}]}`,
		`data: {"id":"chatcmpl_order","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}`,
		`data: [DONE]`,
	}, "\n")

	var output strings.Builder
	selection, err := SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
	if err != nil {
		t.Fatalf("select route: %v", err)
	}
	result := selection.Stream(&output, strings.NewReader(stream), nil)

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

func TestRouteStreamOpenAIChatDoesNotFallbackForHandledNonTextEvents(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_reasoning","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think"}}]}`,
		`data: {"id":"chatcmpl_reasoning","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{}"}}]}}]}`,
	}, "\n")

	var output strings.Builder
	selection, err := SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
	if err != nil {
		t.Fatalf("select route: %v", err)
	}
	result := selection.Stream(&output, strings.NewReader(stream), nil)
	got := output.String()

	if result.ReasoningLen != 5 {
		t.Fatalf("expected reasoning event to be handled, got %#v", result)
	}
	if strings.Count(got, `"type":"response.output_item.added"`) != 2 {
		t.Fatalf("expected one reasoning item and one function call item, got: %s", got)
	}
	if strings.Contains(got, `"type":"message"`) {
		t.Fatalf("unexpected response fallback message for non-text events: %s", got)
	}
}

func TestRouteStreamOpenAIChatCapturesReasoningSamples(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_reasoning","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think first"}}]}`,
		`data: {"id":"chatcmpl_reasoning","model":"mimo-v2.5-pro","choices":[{"delta":{"thinking":"provider thought"}}]}`,
	}, "\n")

	var output strings.Builder
	selection, err := SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
	if err != nil {
		t.Fatalf("select route: %v", err)
	}
	result := selection.Stream(&output, strings.NewReader(stream), nil)

	if len(result.ReasoningSamples) != 2 {
		t.Fatalf("expected reasoning samples, got %#v", result.ReasoningSamples)
	}
	if result.ReasoningSamples[0] != "reasoning_content:[think first]" {
		t.Fatalf("unexpected reasoning_content sample: %#v", result.ReasoningSamples)
	}
	if result.ReasoningSamples[1] != "thinking:[provider thought]" {
		t.Fatalf("unexpected thinking sample: %#v", result.ReasoningSamples)
	}
}

func TestRouteStreamOpenAIChatRawJSONFallbackWritesOnce(t *testing.T) {
	stream := `{"id":"chatcmpl_full","model":"mimo-v2.5-pro","choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

	var output strings.Builder
	selection, err := SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
	if err != nil {
		t.Fatalf("select route: %v", err)
	}
	selection.Stream(&output, strings.NewReader(stream), nil)
	got := output.String()

	if strings.Count(got, `"delta":"Hello"`) != 1 {
		t.Fatalf("expected raw JSON fallback text once, got: %s", got)
	}
}

func TestRouteStreamAnthropicMessagesToolInputDeltas(t *testing.T) {
	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude","usage":{"input_tokens":4,"output_tokens":1}}}`,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"exec_command","input":{}}}`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":"}}`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"pwd\"}"}}`,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
	}, "\n")

	var output strings.Builder
	selection, err := SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetAnthropicMessages})
	if err != nil {
		t.Fatalf("select route: %v", err)
	}
	result := selection.Stream(&output, strings.NewReader(stream), nil)
	got := output.String()

	if result.ChunkCount != 6 {
		t.Fatalf("unexpected stream result: %#v", result)
	}
	if strings.Count(got, `"type":"response.output_item.added"`) != 1 {
		t.Fatalf("expected one function_call added event, got: %s", got)
	}
	if strings.Contains(got, `"arguments":"{}{\"cmd\":\"pwd\"}"`) {
		t.Fatalf("empty Anthropic tool input was incorrectly prefixed to arguments: %s", got)
	}
	if !strings.Contains(got, `"arguments":"{\"cmd\":\"pwd\"}"`) {
		t.Fatalf("tool call arguments were not joined: %s", got)
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
