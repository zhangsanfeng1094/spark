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
	if env != nil {
		cmd.Env = mergeEnv(os.Environ(), env)
	}
	return cmd.Run()
}

func mergeEnv(base []string, override []string) []string {
	keys := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(override))
	for _, kv := range override {
		if i := strings.IndexByte(kv, '='); i > 0 {
			keys[strings.ToUpper(kv[:i])] = struct{}{}
		}
	}
	for _, kv := range base {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			out = append(out, kv)
			continue
		}
		if _, ok := keys[strings.ToUpper(kv[:i])]; ok {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, override...)
	return out
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
