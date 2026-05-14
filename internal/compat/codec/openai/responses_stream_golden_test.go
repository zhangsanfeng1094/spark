package openai

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestWriteResponsesStream_GoldenSequence(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"reasoning_content":"think "}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"sum","arguments":"{\"a\":"}}]}}]}`,
		`data: {"id":"chatcmpl_1","model":"mimo-v2.5-pro","choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`,
		`data: [DONE]`,
	}, "\n")

	var out strings.Builder
	WriteResponsesStream(&out, strings.NewReader(stream), nil)

	got := normalizeResponsesStreamGolden(out.String())
	wantBytes, err := os.ReadFile("testdata/responses_stream_golden.txt")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if diff := compareGoldenText(got, string(wantBytes)); diff != "" {
		t.Fatalf("responses stream golden mismatch:\n%s", diff)
	}
}

func normalizeResponsesStreamGolden(s string) string {
	replacements := []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`"id":"resp_[0-9]+"`), `"id":"resp_STATIC"`},
		{regexp.MustCompile(`"id":"rs_[0-9]+"`), `"id":"rs_STATIC"`},
		{regexp.MustCompile(`"id":"msg_[0-9]+"`), `"id":"msg_STATIC"`},
		{regexp.MustCompile(`"id":"fc_[0-9]+_[0-9]+"`), `"id":"fc_STATIC_0"`},
		{regexp.MustCompile(`"response":\{"id":"resp_[0-9]+"`), `"response":{"id":"resp_STATIC"`},
		{regexp.MustCompile(`"item_id":"rs_[0-9]+"`), `"item_id":"rs_STATIC"`},
		{regexp.MustCompile(`"item_id":"msg_[0-9]+"`), `"item_id":"msg_STATIC"`},
		{regexp.MustCompile(`"item_id":"fc_[0-9]+_[0-9]+"`), `"item_id":"fc_STATIC_0"`},
	}
	for _, item := range replacements {
		s = item.re.ReplaceAllString(s, item.repl)
	}
	return strings.TrimSpace(s)
}

func compareGoldenText(got, want string) string {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == want {
		return ""
	}
	return "got:\n" + got + "\n\nwant:\n" + want
}
