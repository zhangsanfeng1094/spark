package integrations

import "testing"

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
