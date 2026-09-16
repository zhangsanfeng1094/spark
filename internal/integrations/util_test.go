package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spark/internal/config"
)

func TestMergeEnv_OverrideExistingKey(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-old",
		"ANTHROPIC_AUTH_TOKEN=old",
	}
	override := []string{
		"ANTHROPIC_API_KEY=",
		"ANTHROPIC_AUTH_TOKEN=ollama",
	}
	out := mergeEnv(base, override)
	got := map[string]string{}
	for _, kv := range out {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				got[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	if got["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("expected empty ANTHROPIC_API_KEY, got %q", got["ANTHROPIC_API_KEY"])
	}
	if got["ANTHROPIC_AUTH_TOKEN"] != "ollama" {
		t.Fatalf("expected ANTHROPIC_AUTH_TOKEN=ollama, got %q", got["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestMergeEnv_DropsEntriesContainingNUL(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"BAD=before\x00after",
	}
	override := []string{
		"ANTHROPIC_AUTH_TOKEN=ok",
		"OPENAI_API_KEY=bad\x00key",
	}

	out := mergeEnv(base, override)
	for _, kv := range out {
		if containsNUL(kv) {
			t.Fatalf("mergeEnv returned invalid entry %q", kv)
		}
	}

	got := map[string]string{}
	for _, kv := range out {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				got[kv[:i]] = kv[i+1:]
				break
			}
		}
	}

	if _, ok := got["BAD"]; ok {
		t.Fatalf("expected BAD entry to be removed")
	}
	if _, ok := got["OPENAI_API_KEY"]; ok {
		t.Fatalf("expected OPENAI_API_KEY entry with NUL to be removed")
	}
	if got["ANTHROPIC_AUTH_TOKEN"] != "ok" {
		t.Fatalf("expected ANTHROPIC_AUTH_TOKEN=ok, got %q", got["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestMergeEnv_DropsEntriesWithoutUsableKey(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"=C:=C:\\Windows",
		"NO_EQUALS",
	}
	override := []string{
		"OPENAI_API_KEY=test",
	}

	out := mergeEnv(base, override)
	got := map[string]string{}
	for _, kv := range out {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				got[kv[:i]] = kv[i+1:]
				break
			}
		}
	}

	if _, ok := got[""]; ok {
		t.Fatalf("expected entries with empty key to be removed")
	}
	if _, ok := got["NO_EQUALS"]; ok {
		t.Fatalf("expected malformed env entry without '=' to be removed")
	}
	if got["OPENAI_API_KEY"] != "test" {
		t.Fatalf("expected OPENAI_API_KEY=test, got %q", got["OPENAI_API_KEY"])
	}
}

func TestDescribeEnvEntriesForLog_RedactsValues(t *testing.T) {
	in := []string{
		"ANTHROPIC_BASE_URL=http://127.0.0.1:1234",
		"ANTHROPIC_AUTH_TOKEN=ollama",
		"OPENAI_API_KEY=secret",
		"NO_EQUALS",
	}

	got := describeEnvEntriesForLog(in)
	want := []string{
		"ANTHROPIC_BASE_URL=http://127.0.0.1:1234",
		"ANTHROPIC_AUTH_TOKEN=<redacted:6>",
		"OPENAI_API_KEY=<redacted:6>",
		"NO_EQUALS",
	}

	if len(got) != len(want) {
		t.Fatalf("describeEnvEntriesForLog len=%d want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("describeEnvEntriesForLog[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestProfileOpenAIAPIType(t *testing.T) {
	tests := []struct {
		name string
		in   *config.Profile
		want string
	}{
		{name: "nil profile defaults to responses and chat", in: nil, want: config.DefaultOpenAIAPIType},
		{name: "empty defaults to responses and chat", in: &config.Profile{}, want: config.DefaultOpenAIAPIType},
		{name: "responses stays responses", in: &config.Profile{OpenAIAPIType: "responses"}, want: config.OpenAIAPITypeResponses},
		{name: "legacy auto maps to responses and chat", in: &config.Profile{OpenAIAPIType: "auto"}, want: config.DefaultOpenAIAPIType},
		{name: "response alias maps responses", in: &config.Profile{OpenAIAPIType: "response"}, want: config.OpenAIAPITypeResponses},
		{name: "multiple canonicalized", in: &config.Profile{OpenAIAPIType: "chat_completions,responses"}, want: "responses,chat_completions"},
		{name: "unknown falls back to responses and chat", in: &config.Profile{OpenAIAPIType: "foo"}, want: config.DefaultOpenAIAPIType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileOpenAIAPIType(tt.in); got != tt.want {
				t.Fatalf("profileOpenAIAPIType(%+v)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCreateLaunchTempDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir, err := createLaunchTempDir("spark-test-*")
	if err != nil {
		t.Fatalf("createLaunchTempDir failed: %v", err)
	}
	defer os.RemoveAll(dir)

	// Ensure directory was created inside ~/.spark/launch
	wantPrefix := filepath.Join(tmpDir, ".spark", "launch")
	if !strings.HasPrefix(dir, wantPrefix) {
		t.Fatalf("expected path to start with %q, got %q", wantPrefix, dir)
	}

	// Verify directory exists on disk
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if !fi.IsDir() {
		t.Fatalf("expected directory at %s", dir)
	}
}

func TestSymlinkEntries_BasicAndFallback(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "file1.txt"), []byte("hello file1"), 0o644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(src, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("hello file2"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := symlinkEntries(src, dst, func(name string) bool { return name == "file1.txt" }); err != nil {
		t.Fatalf("symlinkEntries failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "file1.txt")); !os.IsNotExist(err) {
		t.Fatalf("file1.txt should have been skipped")
	}
	if data, err := os.ReadFile(filepath.Join(dst, "subdir", "file2.txt")); err != nil || string(data) != "hello file2" {
		t.Fatalf("failed reading mirrored file: data=%s, err=%v", string(data), err)
	}
}

func TestMirrorEntryFallback_CopyAndLink(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	fileSrc := filepath.Join(src, "test.txt")
	fileDst := filepath.Join(dst, "test.txt")
	if err := os.WriteFile(fileSrc, []byte("fallback data"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mirrorEntryFallback(fileSrc, fileDst); err != nil {
		t.Fatalf("mirrorEntryFallback for file failed: %v", err)
	}
	if data, err := os.ReadFile(fileDst); err != nil || string(data) != "fallback data" {
		t.Fatalf("unexpected content in fallback file: %s", string(data))
	}

	dirSrc := filepath.Join(src, "nested")
	dirDst := filepath.Join(dst, "nested")
	if err := os.MkdirAll(dirSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirSrc, "inner.txt"), []byte("inner data"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mirrorEntryFallback(dirSrc, dirDst); err != nil {
		t.Fatalf("mirrorEntryFallback for dir failed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dirDst, "inner.txt")); err != nil || string(data) != "inner data" {
		t.Fatalf("unexpected content in fallback dir: %s", string(data))
	}
}

func TestDaemonProfileTokenCarriesThinkingOverride(t *testing.T) {
	budget := 8192
	got := daemonProfileToken(&config.Profile{
		RuntimeName:     "work",
		SessionThinking: &config.ThinkingConfig{Mode: "force", BudgetTokens: &budget},
	})
	if got != "spark-profile:work?think=8192" {
		t.Fatalf("token=%q", got)
	}
}
