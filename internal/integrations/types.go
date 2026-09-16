package integrations

import "spark/internal/config"

type Runner interface {
	String() string
	Run(profile *config.Profile, model string, args []string) error
}

type PromptRunner interface {
	RunWithPrompt(profile *config.Profile, model string, args []string, prompt *config.PromptInjection) error
}

type ConfiguredPromptRunner interface {
	RunWithConfigAndPrompt(profile *config.Profile, integration *config.IntegrationConfig, model string, args []string, prompt *config.PromptInjection) error
}

type Editor interface {
	Paths() []string
	Edit(profile *config.Profile, models []string) error
	Models() []string
}

// EditChecker is an optional Editor extension. Integrations that can tell
// whether Spark-managed config is already in the desired state should
// implement it so launch can skip confirm-and-write when nothing would change.
// Editors without this interface keep the previous always-confirm behavior.
type EditChecker interface {
	NeedsEdit(profile *config.Profile, models []string) (bool, error)
}
