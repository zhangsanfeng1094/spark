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
