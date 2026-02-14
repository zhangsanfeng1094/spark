package integrations

import (
	"fmt"
	"os"
	"os/exec"

	"spark/internal/config"
)

type Codex struct{}

func (c *Codex) String() string { return "Codex" }

const codexProviderName = "spark"

func (c *Codex) args(model, baseURL string, extra []string) []string {
	cmdArgs := []string{
		"-c", fmt.Sprintf(`model_providers.%s.name="Spark"`, codexProviderName),
		"-c", fmt.Sprintf(`model_providers.%s.base_url="%s"`, codexProviderName, baseURL),
		"-c", fmt.Sprintf(`model_provider="%s"`, codexProviderName),
	}
	if model != "" {
		cmdArgs = append(cmdArgs, "-m", model)
	}
	cmdArgs = append(cmdArgs, extra...)
	return cmdArgs
}

func (c *Codex) Run(profile *config.Profile, model string, args []string) error {
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("codex is not installed, install with: npm install -g @openai/codex")
	}

	baseURL := profileBase(profile)
	apiKey := profileKey(profile)
	quietCompatStderr := shouldQuietCompatStderr()
	proxy, err := startResponsesCompatProxy(baseURL, apiKey, quietCompatStderr)
	if err != nil {
		return err
	}
	defer proxy.Close()

	envBaseURL := proxy.BaseURL()
	envKey := "spark-compat"
	if !quietCompatStderr {
		fmt.Fprintf(os.Stderr, "Using compatibility adapter: %s -> %s\n", envBaseURL, baseURL)
		fmt.Fprintf(os.Stderr, "Compatibility adapter log file: %s\n", proxy.LogPath())
	}
	cmdArgs := c.args(model, envBaseURL, args)

	env := []string{
		"OPENAI_BASE_URL=" + envBaseURL,
		"OPENAI_API_KEY=" + envKey,
		"CODEX_API_KEY=" + envKey,
		"OPENAI_ORG_ID=" + profile.OpenAIOrg,
		"OPENAI_PROJECT_ID=" + profile.OpenAIProject,
	}
	return runCmd("codex", cmdArgs, env)
}
