package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatIntegrationsDoNotKeepLegacyTranslatorNames(t *testing.T) {
	legacyNames := []string{
		"responses" + "ToChatCompletions",
		"responses" + "ToolsToChatTools",
		"responses" + "ToolChoiceToChatToolChoice",
		"responses" + "InputToMessages",
		"anthropic" + "ToChatCompletions",
		"anthropic" + "MessagesToChatMessages",
		"anthropic" + "ContentToChatParts",
		"anthropic" + "ToolsToChatTools",
		"anthropic" + "ToolChoiceToChatToolChoice",
		"chat" + "ToAnthropicMessage",
		"chat" + "StopReason",
	}
	for _, name := range legacyNames {
		name := name
		t.Run(name, func(t *testing.T) {
			filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				if strings.Contains(string(data), name) {
					t.Fatalf("legacy translator name %q remains in %s", name, path)
				}
				return nil
			})
		})
	}
}
