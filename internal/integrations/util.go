package integrations

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"spark/internal/config"
	"spark/internal/thinking"
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

// daemonProfileToken is intentionally a routing token, never an upstream key.
// The daemon resolves credentials from the named profile and consumes think
// before forwarding, so launchers do not leak it to an upstream endpoint.
func daemonProfileToken(profile *config.Profile) string {
	if profile == nil || strings.TrimSpace(profile.RuntimeName) == "" {
		return "spark-compat"
	}
	token := "spark-profile:" + profile.RuntimeName
	if policy := profile.SessionThinking; policy != nil {
		value := policy.Effort
		if policy.BudgetTokens != nil {
			value = strconv.Itoa(*policy.BudgetTokens)
		}
		if strings.EqualFold(policy.Mode, thinking.ModeOff) {
			value = "off"
		}
		if value != "" {
			token += "?think=" + url.QueryEscape(value)
		}
	}
	return token
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

// createLaunchTempDir creates a temporary directory under ~/.spark/launch/
// so agents (like Codex) don't trigger temporary dir /tmp security sandbox warnings.
func createLaunchTempDir(pattern string) (string, error) {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		launchRoot := filepath.Join(home, ".spark", "launch")
		if err := os.MkdirAll(launchRoot, 0o755); err == nil {
			return os.MkdirTemp(launchRoot, pattern)
		}
	}
	return os.MkdirTemp("", pattern)
}

// symlinkEntries mirrors every entry from src into dst as symlinks (with fallback
// to hardlinks/junctions/copies on Windows or environments where symlink creation lacks privileges)
// so the launch home stays relocatable and writes reach the real assets.
func symlinkEntries(src, dst string, skip func(name string) bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", src, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if skip != nil && skip(name) {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		if err := mirrorEntry(srcPath, dstPath); err != nil {
			return fmt.Errorf("link %s: %w", name, err)
		}
	}
	return nil
}

// mirrorEntry mirrors a file or directory from src to dst. It prefers a symbolic link;
// if symlinking is unsupported or prohibited by policy (e.g. non-developer mode on Windows),
// it falls back gracefully:
//   - Directory on Windows: NTFS Directory Junction (`mklink /J`), then directory copy
//   - Directory on non-Windows: directory copy
//   - File: Hard link (os.Link), then file copy
func mirrorEntry(srcPath, dstPath string) error {
	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		absSrc = srcPath
	}
	if err := os.Symlink(absSrc, dstPath); err == nil {
		return nil
	}
	return mirrorEntryFallback(absSrc, dstPath)
}

func mirrorEntryFallback(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		if runtime.GOOS == "windows" {
			if err := createWindowsJunction(src, dst); err == nil {
				return nil
			}
		}
		return copyDir(src, dst)
	}
	// For files, attempt hard link first
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func createWindowsJunction(target, link string) error {
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	return cmd.Run()
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm())
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func profileBase(profile *config.Profile) string {
	if profile == nil {
		return "https://api.openai.com/v1"
	}
	if ep := strings.TrimSpace(profile.EffectiveEndpoint()); ep != "" {
		return ep
	}
	return "https://api.openai.com/v1"
}

func profileKey(profile *config.Profile) string {
	return profile.EffectiveAPIKey()
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

// writePromptTempFile writes content to a temp file and returns the path.
// The caller should defer os.Remove(path).
func writePromptTempFile(content string) (string, error) {
	f, err := os.CreateTemp("", "spark-prompt-*.md")
	if err != nil {
		return "", fmt.Errorf("create temp prompt file: %w", err)
	}
	path := f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("write temp prompt file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close temp prompt file: %w", err)
	}
	return path, nil
}
