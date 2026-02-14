package integrations

import "spark/internal/config"

type Runner interface {
	String() string
	Run(profile *config.Profile, model string, args []string) error
}

type Editor interface {
	Paths() []string
	Edit(profile *config.Profile, models []string) error
	Models() []string
}
