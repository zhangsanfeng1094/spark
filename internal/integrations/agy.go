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

	if err := syncAgyConfig(); err != nil {
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

// syncAgyConfig updates ~/.gemini/antigravity-cli/settings.json to select the
// gemini provider and merges Spark-managed MCP servers into ~/.gemini/config/mcp_config.json.
func syncAgyConfig() error {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	realGemini := filepath.Join(userHome, ".gemini")
	if err := writeAgySettings(realGemini); err != nil {
		return err
	}
	return writeAgyMCP(realGemini)
}

func writeAgySettings(geminiDir string) error {
	settingsPath := filepath.Join(geminiDir, "antigravity-cli", "settings.json")
	if err := ensureDir(settingsPath); err != nil {
		return err
	}
	settings := readMap(settingsPath)
	settings["modelProvider"] = agyModelProvider
	return writeJSON(settingsPath, settings)
}

func writeAgyMCP(geminiDir string) error {
	mcpPath := filepath.Join(geminiDir, "config", "mcp_config.json")
	if err := ensureDir(mcpPath); err != nil {
		return err
	}
	root := readMap(mcpPath)
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if sparkCfg, _ := config.Load(); sparkCfg != nil {
		for name, srv := range sparkCfg.McpServersForAgent("agy") {
			servers[name] = encodeAgyMCPServer(srv)
		}
	}
	root["mcpServers"] = servers
	return writeJSON(mcpPath, root)
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
