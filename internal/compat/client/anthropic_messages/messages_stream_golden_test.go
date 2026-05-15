package anthropic_messages

import (
	"os"
	"strings"
	"testing"
)

func TestWriteMessagesStream_GoldenSequence(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think "}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":"}}]}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`,
		`data: [DONE]`,
	}, "\n")

	var out strings.Builder
	WriteMessagesStream(&out, strings.NewReader(stream), "", nil)

	got := strings.TrimSpace(out.String())
	wantBytes, err := os.ReadFile("testdata/messages_stream_golden.txt")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if diff := compareGoldenText(got, string(wantBytes)); diff != "" {
		t.Fatalf("messages stream golden mismatch:\n%s", diff)
	}
}

func compareGoldenText(got, want string) string {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == want {
		return ""
	}
	return "got:\n" + got + "\n\nwant:\n" + want
}
