package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"spark/internal/config"
)

func runCmd(name string, args []string, env []string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	var mergedEnv []string
	var droppedEnv []string
	if env != nil {
		mergedEnv, droppedEnv = mergeEnvWithDropped(os.Environ(), env)
		cmd.Env = mergedEnv
	}
	appendLaunchRouteLog(fmt.Sprintf(
		"[exec] path=%q args=%s spark_env=%s env_count=%d dropped_env=%s",
		name,
		mustJSONForLog(args),
		mustJSONForLog(describeEnvEntriesForLog(env)),
		len(mergedEnv),
		mustJSONForLog(droppedEnv),
	))
	err := cmd.Run()
	if err != nil {
		appendLaunchRouteLog(fmt.Sprintf(
			"[exec-error] path=%q args=%s err=%q spark_env=%s dropped_env=%s",
			name,
			mustJSONForLog(args),
			err.Error(),
			mustJSONForLog(describeEnvEntriesForLog(env)),
			mustJSONForLog(droppedEnv),
		))
	}
	return err
}

func mergeEnv(base []string, override []string) []string {
	out, _ := mergeEnvWithDropped(base, override)
	return out
}

func mergeEnvWithDropped(base []string, override []string) ([]string, []string) {
	keys := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(override))
	dropped := make([]string, 0)
	for _, kv := range override {
		if !isUsableEnvEntry(kv) {
			dropped = append(dropped, "override:"+describeEnvEntryForLog(kv))
			continue
		}
		if i := strings.IndexByte(kv, '='); i > 0 {
			keys[strings.ToUpper(kv[:i])] = struct{}{}
		}
	}
	for _, kv := range base {
		if !isUsableEnvEntry(kv) {
			dropped = append(dropped, "base:"+describeEnvEntryForLog(kv))
			continue
		}
		i := strings.IndexByte(kv, '=')
		if _, ok := keys[strings.ToUpper(kv[:i])]; ok {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, override...)
	kept, sanitizedDropped := sanitizeEnv(out)
	dropped = append(dropped, sanitizedDropped...)
	return kept, dropped
}

func sanitizeEnv(env []string) ([]string, []string) {
	out := env[:0]
	dropped := make([]string, 0)
	for _, kv := range env {
		if !isUsableEnvEntry(kv) {
			dropped = append(dropped, "merged:"+describeEnvEntryForLog(kv))
			continue
		}
		out = append(out, kv)
	}
	return out, dropped
}

func isUsableEnvEntry(s string) bool {
	if containsNUL(s) {
		return false
	}
	return strings.IndexByte(s, '=') > 0
}

func containsNUL(s string) bool {
	return strings.IndexByte(s, 0) >= 0
}

func describeEnvEntriesForLog(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		out = append(out, describeEnvEntryForLog(kv))
	}
	return out
}

func describeEnvEntryForLog(kv string) string {
	i := strings.IndexByte(kv, '=')
	if i <= 0 {
		return kv
	}
	key := kv[:i]
	value := kv[i+1:]
	if shouldRedactEnvValue(key) {
		return fmt.Sprintf("%s=<redacted:%d>", key, len(value))
	}
	return kv
}

func shouldRedactEnvValue(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
	return strings.Contains(key, "KEY") || strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET")
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readMap(path string) map[string]any {
	m := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

func ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func profileBase(profile *config.Profile) string {
	if profile == nil || profile.OpenAIBaseURL == "" {
		return "https://api.openai.com/v1"
	}
	return profile.OpenAIBaseURL
}

func profileKey(profile *config.Profile) string {
	if profile == nil {
		return ""
	}
	return profile.OpenAIAPIKey
}

func profileOpenAIAPIType(profile *config.Profile) string {
	if profile == nil {
		return config.DefaultOpenAIAPIType
	}
	if canonical := config.CanonicalizeOpenAIAPITypes(profile.OpenAIAPIType); canonical != "" {
		return canonical
	}
	return config.DefaultOpenAIAPIType
}

func firstModel(models []string) (string, error) {
	if len(models) == 0 || models[0] == "" {
		return "", fmt.Errorf("no models selected")
	}
	return models[0], nil
}

func isInteractiveTerminal() bool {
	return isTerminalFile(os.Stdin) && isTerminalFile(os.Stdout) && isTerminalFile(os.Stderr)
}

// shouldQuietCompatStderr controls whether compatibility adapter warnings should
// print to stderr. AGENT_LAUNCH_COMPAT_STDERR overrides auto behavior:
// 1/true/on => always print, 0/false/off => always quiet.
func shouldQuietCompatStderr() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("AGENT_LAUNCH_COMPAT_STDERR")))
	switch v {
	case "1", "true", "on", "yes":
		return false
	case "0", "false", "off", "no":
		return true
	default:
		return isInteractiveTerminal()
	}
}

func isTerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
