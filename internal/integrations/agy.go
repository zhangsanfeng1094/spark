package integrations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"spark/internal/compat/daemon"
	"spark/internal/config"
)

const agyModelProvider = "gemini"

type Agy struct{}

func (a *Agy) String() string { return "Antigravity" }

func agyShouldUseCompatProxy(profile *config.Profile) bool {
	if profile == nil {
		return true
	}
	return !config.SupportsOpenAIAPIType(profileOpenAIAPIType(profile), config.OpenAIAPITypeGeminiGenerateContent)
}

func (a *Agy) Run(profile *config.Profile, model string, args []string) error {
	modelID := strings.TrimSpace(model)
	if modelID == "" {
		return fmt.Errorf("model cannot be empty")
	}
	bin, err := findAgyBinary()
	if err != nil {
		return err
	}

	launchHome, err := createLaunchTempDir("spark-agy-home-*")
	if err != nil {
		return fmt.Errorf("create agy launch home: %w", err)
	}
	defer func() { _ = os.RemoveAll(launchHome) }()

	if err := writeAgyLaunchHome(launchHome); err != nil {
		return err
	}

	cmdArgs := append([]string{}, args...)
	if !cliArgsHasModelFlag(cmdArgs) {
		cmdArgs = append([]string{"--model", modelID}, cmdArgs...)
	}

	geminiBase := strings.TrimRight(profileBase(profile), "/")
	geminiKey := profileKey(profile)
	if os.Getenv("SPARK_DIRECT") != "1" {
		dInfo, err := daemon.EnsureDaemon(context.Background(), nil)
		if err != nil {
			return fmt.Errorf("start shared Spark daemon: %w", err)
		}
		if dInfo == nil {
			return fmt.Errorf("start shared Spark daemon: no daemon information returned")
		}
		geminiBase = dInfo.BaseURL
		geminiKey = daemonProfileToken(profile)
		routeLine := fmt.Sprintf("[route] mode=shared-daemon integration=agy upstream=%s proxy=%s daemon_pid=%d", profileBase(profile), geminiBase, dInfo.PID)
		if routeLogPath := appendLaunchRouteLog(routeLine); routeLogPath != "" {
			routeLine += " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
	} else {
		key, source := resolveOpenAIAPIKey(geminiKey)
		if key != "" {
			geminiKey = key
		}
		routeLine := fmt.Sprintf("[route] mode=direct integration=agy upstream=%s api_key_source=%s api_key_set=%t", geminiBase, source, strings.TrimSpace(key) != "")
		if routeLogPath := appendLaunchRouteLog(routeLine); routeLogPath != "" {
			routeLine += " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
	}

	env := []string{
		"HOME=" + launchHome,
		"GEMINI_API_KEY=" + geminiKey,
		"GOOGLE_GEMINI_BASE_URL=" + geminiBase,
	}
	return runCmd(bin, cmdArgs, env)
}

func findAgyBinary() (string, error) {
	if p, err := exec.LookPath("agy"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("agy is not installed, install it from the Antigravity CLI release")
	}
	candidate := filepath.Join(home, ".local", "bin", "agy")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, nil
	}
	return "", fmt.Errorf("agy is not installed, install it from the Antigravity CLI release")
}

// writeAgyLaunchHome creates an isolated HOME that keeps the user's AGY assets
// while selecting Gemini API-key auth and merging Spark-managed MCP servers.
func writeAgyLaunchHome(launchHome string) error {
	if err := os.MkdirAll(launchHome, 0o755); err != nil {
		return err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err := symlinkEntries(userHome, launchHome, func(name string) bool {
		return name == ".gemini"
	}); err != nil {
		return err
	}

	realGemini := filepath.Join(userHome, ".gemini")
	launchGemini := filepath.Join(launchHome, ".gemini")
	if err := os.MkdirAll(launchGemini, 0o755); err != nil {
		return err
	}
	if err := symlinkEntries(realGemini, launchGemini, func(name string) bool {
		return name == "antigravity-cli" || name == "config"
	}); err != nil {
		return err
	}

	if err := writeAgyLaunchSettings(launchGemini, realGemini); err != nil {
		return err
	}
	return writeAgyLaunchMCP(launchGemini, realGemini)
}

func writeAgyLaunchSettings(launchGemini, realGemini string) error {
	realCLI := filepath.Join(realGemini, "antigravity-cli")
	launchCLI := filepath.Join(launchGemini, "antigravity-cli")
	if err := os.MkdirAll(launchCLI, 0o755); err != nil {
		return err
	}
	if err := symlinkEntries(realCLI, launchCLI, shouldSkipAgyCLIMirror); err != nil {
		return err
	}

	settings := readMap(filepath.Join(realCLI, "settings.json"))
	settings["modelProvider"] = agyModelProvider
	return writeJSON(filepath.Join(launchCLI, "settings.json"), settings)
}

func shouldSkipAgyCLIMirror(name string) bool {
	if name == "settings.json" || strings.Contains(strings.ToLower(name), "oauth-token") {
		return true
	}
	return strings.HasSuffix(name, ".lock")
}

func writeAgyLaunchMCP(launchGemini, realGemini string) error {
	realConfig := filepath.Join(realGemini, "config")
	launchConfig := filepath.Join(launchGemini, "config")
	if err := os.MkdirAll(launchConfig, 0o755); err != nil {
		return err
	}
	if err := symlinkEntries(realConfig, launchConfig, func(name string) bool {
		return name == "mcp_config.json"
	}); err != nil {
		return err
	}

	root := readMap(filepath.Join(realConfig, "mcp_config.json"))
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if sparkCfg, _ := config.Load(); sparkCfg != nil {
		for name, srv := range sparkCfg.McpServers {
			if srv != nil && srv.Enabled {
				servers[name] = encodeAgyMCPServer(srv)
			}
		}
	}
	root["mcpServers"] = servers
	return writeJSON(filepath.Join(launchConfig, "mcp_config.json"), root)
}

func encodeAgyMCPServer(server *config.McpServerConfig) map[string]any {
	out := map[string]any{"disabled": false}
	if strings.TrimSpace(server.URL) != "" {
		out["serverUrl"] = strings.TrimSpace(server.URL)
	} else {
		out["command"] = server.Command
		if len(server.Args) > 0 {
			out["args"] = append([]string(nil), server.Args...)
		}
	}
	if len(server.Env) > 0 {
		env := make(map[string]string, len(server.Env))
		for key, value := range server.Env {
			env[key] = value
		}
		out["env"] = env
	}
	return out
}
