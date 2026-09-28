package integrations

import "spark/internal/config"

type Runner interface {
	String() string
	Run(profile *config.Profile, model string, args []string) error
}

// ConfiguredRunner is an optional capability for integrations that consume
// per-integration launch configuration (e.g. Codex model catalog).
type ConfiguredRunner interface {
	RunWithIntegration(profile *config.Profile, integration *config.IntegrationConfig, model string, args []string) error
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
