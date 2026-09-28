package mcp

import (
	"strings"
	"testing"
	"time"

	"spark/internal/config"
)

func TestSummarizeDisabled(t *testing.T) {
	if matrixTestGlyph(Status{Kind: StatusDisabled}) != "○" {
		t.Fatal("disabled glyph mismatch")
	}
	if matrixTestGlyph(Status{Kind: StatusNotChecked}) != "?" {
		t.Fatal("not-checked glyph mismatch")
	}
	if matrixTestGlyph(Status{Kind: StatusError}) != "!" {
		t.Fatal("error glyph mismatch")
	}
	if matrixTestGlyph(Status{Kind: StatusOK}) != "●" {
		t.Fatal("ok glyph mismatch")
	}
}

func matrixTestGlyph(status Status) string {
	switch status.Kind {
	case StatusOK:
		return "●"
	case StatusError:
		return "!"
	case StatusNotChecked:
		return "?"
	default:
		return "○"
	}
}

func TestSummarizeNotChecked(t *testing.T) {
	status := Summarize(&config.McpServerConfig{Command: "x", Enabled: true}, nil)
	if status.Kind != StatusNotChecked || status.Headline != "Not checked" {
		t.Fatalf("expected Not checked, got %+v", status)
	}
}

func TestSummarizeInvalidConfig(t *testing.T) {
	status := Summarize(&config.McpServerConfig{}, nil)
	if status.Kind != StatusError || status.Headline != "Invalid config" {
		t.Fatalf("expected Invalid config, got %+v", status)
	}
}

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		err  string
		want FailureReason
	}{
		{"HTTP 401: unauthorized", ReasonAuthRequired},
		{"HTTP 403: forbidden", ReasonAuthRequired},
		{`exec: "npx": executable file not found in $PATH`, ReasonCommandNotFound},
		{"context deadline exceeded", ReasonTimeout},
		{"dial tcp: connection refused", ReasonUnreachable},
		{"no such host", ReasonUnreachable},
		{"missing transport: command or url is required", ReasonInvalidConfig},
		{"something else", ReasonOther},
	}
	for _, tc := range cases {
		if got := ClassifyFailure(tc.err); got != tc.want {
			t.Fatalf("ClassifyFailure(%q) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestFailureHeadlines(t *testing.T) {
	for reason, want := range map[FailureReason]string{
		ReasonAuthRequired:    "Auth required",
		ReasonCommandNotFound: "Command not found",
		ReasonTimeout:         "Timeout",
		ReasonUnreachable:     "Unreachable",
		ReasonInvalidConfig:   "Invalid config",
		ReasonOther:           "Error",
	} {
		if got := FailureHeadline(reason); got != want {
			t.Fatalf("FailureHeadline(%v) = %q, want %q", reason, got, want)
		}
	}
}

func TestSummarizeTestedOKIncludesTools(t *testing.T) {
	status := Summarize(&config.McpServerConfig{Command: "x", Enabled: true}, &Result{
		Stage:      StageToolsList,
		ToolsCount: 5,
		Latency:    120 * time.Millisecond,
	})
	if status.Kind != StatusOK {
		t.Fatalf("expected OK, got %+v", status)
	}
	if !strings.Contains(status.Headline, "5 tools") {
		t.Fatalf("headline should include tool count: %q", status.Headline)
	}
}

func TestSummarizeErrorIsActionable(t *testing.T) {
	status := Summarize(&config.McpServerConfig{Command: "gone", Enabled: true}, &Result{
		Stage: StageSpawn,
		Err:   `exec: "gone": executable file not found in $PATH`,
	})
	if status.Kind != StatusError || status.Headline != "Command not found" {
		t.Fatalf("expected Command not found, got %+v", status)
	}
	if len(status.Suggestions) == 0 || !strings.Contains(status.Suggestions[0], "PATH") {
		t.Fatalf("expected actionable suggestion, got %v", status.Suggestions)
	}
}

func TestTestInvalidServer(t *testing.T) {
	result := Test(&config.McpServerConfig{})
	if result.Err == "" || result.Stage != StageSpawn {
		t.Fatalf("expected spawn error for missing transport, got %+v", result)
	}
}

func TestAgentLabel(t *testing.T) {
	for agent, want := range map[string]string{
		"codex": "Codex", "claude": "Claude", "one": "One", "grok": "Grok", "agy": "Agy",
	} {
		if got := AgentLabel(agent); got != want {
			t.Fatalf("AgentLabel(%q) = %q, want %q", agent, got, want)
		}
	}
}
