package app

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"spark/internal/config"
	"spark/internal/integrations"
	"spark/internal/tui"
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "spark",
		Short:         "Launch coding agents with configurable OpenAI-compatible gateways",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractive()
		},
	}

	root.AddCommand(newLaunchCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newProfileCmd())
	return root
}

func newLaunchCmd() *cobra.Command {
	var modelFlag string
	var profileFlag string
	var configOnly bool

	cmd := &cobra.Command{
		Use:   "launch [integration] [-- [extra args...]]",
		Short: "Configure and launch an integration",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			var passArgs []string
			dash := cmd.ArgsLenAtDash()
			if dash == -1 {
				if len(args) > 0 {
					name = args[0]
				}
			} else {
				if dash > 0 {
					name = args[0]
				}
				passArgs = args[dash:]
			}

			if name == "" {
				selected, err := tui.SelectOne("Select integration:", integrations.Names())
				if err != nil {
					return err
				}
				name = selected
			}
			return launchIntegration(name, modelFlag, profileFlag, configOnly, passArgs)
		},
	}
	cmd.Flags().StringVar(&modelFlag, "model", "", "Model name")
	cmd.Flags().StringVar(&profileFlag, "profile", "", "Profile name")
	cmd.Flags().BoolVar(&configOnly, "config", false, "Configure without launching")
	return cmd
}

func newConfigCmd() *cobra.Command {
	var profileFlag string
	var modelFlag string
	cmd := &cobra.Command{
		Use:   "config [integration]",
		Short: "Configure integration only",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				selected, err := tui.SelectOne("Select integration:", integrations.Names())
				if err != nil {
					return err
				}
				name = selected
			}
			return launchIntegration(name, modelFlag, profileFlag, true, nil)
		},
	}
	cmd.Flags().StringVar(&modelFlag, "model", "", "Model name")
	cmd.Flags().StringVar(&profileFlag, "profile", "", "Profile name")
	return cmd
}

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage gateway profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return manageProfiles()
		},
	}
	return cmd
}

func runInteractive() error {
	for {
		options := []string{"Launch integration", "Manage profiles", "Show config file", "Quit"}
		choice, err := tui.SelectOne("spark", options)
		if err != nil {
			return err
		}
		switch choice {
		case "Launch integration":
			name, err := tui.SelectOne("Select integration:", integrations.Names())
			if err != nil {
				return err
			}
			if err := launchIntegration(name, "", "", false, nil); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		case "Manage profiles":
			if err := manageProfiles(); err != nil {
				return err
			}
		case "Show config file":
			path, _ := config.ConfigPath()
			fmt.Println(path)
		case "Quit":
			return nil
		}
	}
}

func launchIntegration(name, modelFlag, profileFlag string, configOnly bool, passArgs []string) error {
	r, ok := integrations.Get(name)
	if !ok {
		return fmt.Errorf("unknown integration: %s", name)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	profileName := cfg.DefaultProfile
	if strings.TrimSpace(profileFlag) != "" {
		profileName = strings.TrimSpace(profileFlag)
	}
	profile, err := cfg.ProfileByName(profileName)
	if err != nil {
		names := profileNames(cfg)
		chosen, pickErr := tui.SelectOne("Select profile:", names)
		if pickErr != nil {
			return pickErr
		}
		profileName = chosen
		profile, err = cfg.ProfileByName(chosen)
		if err != nil {
			return err
		}
	}

	models := resolveModels(modelFlag, profile)

	if ed, isEditor := r.(integrations.Editor); isEditor {
		if len(models) == 0 {
			models, err = tui.InputCSV("Models for "+name, cfg.History.ModelInputs)
			if err != nil {
				return err
			}
		}
		if len(models) == 0 {
			return fmt.Errorf("at least one model required")
		}
		fmt.Printf("This will modify %s:\n", r.String())
		for _, p := range ed.Paths() {
			fmt.Printf("  %s\n", p)
		}
		fmt.Printf("Backups directory: %s\n", config.BackupDir())
		ok, err := tui.Confirm("Proceed", true)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := ed.Edit(profile, models); err != nil {
			return err
		}
	} else {
		model := ""
		if len(models) > 0 {
			model = models[0]
		}
		if model == "" {
			model, err = tui.InputWithDefault("Model", cfg.History.LastModelInput)
			if err != nil {
				return err
			}
			model = strings.TrimSpace(model)
			if model == "" {
				return fmt.Errorf("model cannot be empty")
			}
			models = []string{model}
		}
	}

	if len(models) == 0 || strings.TrimSpace(models[0]) == "" {
		return fmt.Errorf("model cannot be empty")
	}

	cfg.UpsertModelHistory(models[0])
	cfg.History.LastSelection = strings.ToLower(name)
	if err := config.Save(cfg); err != nil {
		return err
	}

	if configOnly {
		launchNow, err := tui.Confirm("Launch now", false)
		if err != nil {
			return err
		}
		if !launchNow {
			return nil
		}
	}

	fmt.Printf("Launching %s with %s using profile %s\n", r.String(), models[0], profileName)
	return r.Run(profile, models[0], passArgs)
}

func manageProfiles() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return tui.ManageProfilesDashboard(cfg)
}

func resolveModels(modelFlag string, profile *config.Profile) []string {
	if strings.TrimSpace(modelFlag) != "" {
		return []string{strings.TrimSpace(modelFlag)}
	}
	if profile != nil && len(profile.Models) > 0 {
		return normalizeModels(profile.Models)
	}
	if profile != nil && strings.TrimSpace(profile.DefaultModel) != "" {
		return []string{strings.TrimSpace(profile.DefaultModel)}
	}
	return nil
}

func normalizeModels(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

func profileNames(cfg *config.RootConfig) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
