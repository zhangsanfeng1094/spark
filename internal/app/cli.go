package app

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"spark/internal/config"
	"spark/internal/integrations"
	"spark/internal/skills"
	"spark/internal/tui"
	"spark/internal/usage"
	"spark/internal/version"
)

func NewRootCmd() *cobra.Command {
	var showVersion bool

	root := &cobra.Command{
		Use:           "spark",
		Short:         "Launch coding agents with configurable OpenAI-compatible gateways",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				fmt.Println(version.Get().String())
				return nil
			}
			return runInteractive()
		},
	}

	root.Flags().BoolVarP(&showVersion, "version", "V", false, "Print version information")

	root.AddCommand(newLaunchCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newMcpCmd())
	root.AddCommand(newSkillCmd())
	root.AddCommand(newProfileCmd())
	root.AddCommand(newDebugCmd())
	root.AddCommand(newVersionCmd())
	return root
}

func newLaunchCmd() *cobra.Command {
	var modelFlag string
	var profileFlag string
	var configOnly bool

	cmd := &cobra.Command{
		Use:   "launch [integration] [-- [extra args...]]",
		Short: "Configure and launch an integration",
		Long: `Configure and launch an AI coding agent integration.

Arguments after -- are passed directly to the integration.

Examples:
  spark launch codex -- resume abc123
  spark launch codex --model gpt-4o -- --no-auto-approve

Rule: Arguments before -- are for spark, arguments after -- are passed to the integration.`,
		Args: cobra.ArbitraryArgs,
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

func interactiveMenuOptions() []string {
	return []string{"Launch integration", "Token usage", "Manage profiles", "Manage MCP servers", "Manage skills", "Show config file", "Quit"}
}

func interactiveMenuDescriptions() map[string]string {
	return map[string]string{
		"Launch integration": "Start Spark with the selected coding agent integration using the active profile and model settings.",
		"Token usage":        "Review recorded compat proxy token usage with fixed time filters.",
		"Manage profiles":    "Edit provider profiles, base URLs, API keys, API type behavior, and model defaults.",
		"Manage MCP servers": "Manage MCP server entries, health probes, and transport settings.",
		"Manage skills":      "Browse installed skills, add new ones, and toggle them on or off.",
		"Show config file":   "Print the active Spark config path after leaving the dashboard.",
		"Quit":               "Exit Spark without making additional changes.",
	}
}

func interactiveDashboardActions() []tui.DashboardAction {
	descriptions := interactiveMenuDescriptions()
	options := interactiveMenuOptions()
	actions := make([]tui.DashboardAction, 0, len(options))
	for _, option := range options {
		actions = append(actions, tui.DashboardAction{
			Title:       option,
			Description: descriptions[option],
		})
	}
	return actions
}

func newDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "debug",
		Short:  "Inspect Spark UI and runtime state",
		Hidden: true,
	}
	cmd.AddCommand(newDebugSnapshotCmd())
	return cmd
}

func newDebugSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Render non-interactive UI snapshots",
	}
	cmd.AddCommand(newDebugDashboardSnapshotCmd())
	cmd.AddCommand(newDebugTokenUsageSnapshotCmd())
	cmd.AddCommand(newDebugProfileSnapshotCmd())
	cmd.AddCommand(newDebugMCPSnapshotCmd())
	cmd.AddCommand(newDebugSkillSnapshotCmd())
	return cmd
}

func newDebugDashboardSnapshotCmd() *cobra.Command {
	var width int
	var height int
	var cursor int
	var color bool

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Render the main dashboard without starting the TUI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			summary := tui.DashboardSummary{}
			if path, err := config.ConfigPath(); err == nil {
				summary.ConfigPath = path
			}
			if cfg, err := config.Load(); err == nil {
				summary.CurrentProfile = cfg.DefaultProfile
			}

			view, err := tui.RenderDashboardSnapshot("Spark", interactiveDashboardActions(), summary, width, height, cursor)
			if err != nil {
				return err
			}
			writeSnapshot(cmd, view, color)
			return nil
		},
	}
	cmd.Flags().IntVar(&width, "width", 90, "Snapshot terminal width")
	cmd.Flags().IntVar(&height, "height", 24, "Snapshot terminal height")
	cmd.Flags().IntVar(&cursor, "cursor", 0, "Selected dashboard row")
	cmd.Flags().BoolVar(&color, "color", false, "Keep ANSI color escape sequences")
	return cmd
}

func newDebugTokenUsageSnapshotCmd() *cobra.Command {
	var width int
	var height int
	var cursor int
	var color bool

	cmd := &cobra.Command{
		Use:   "token-usage",
		Short: "Render the token usage view without starting the TUI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshot, err := loadTokenUsageSnapshot()
			if err != nil {
				return err
			}
			view, err := tui.RenderTokenUsageSnapshot(snapshot, width, height, cursor)
			if err != nil {
				return err
			}
			writeSnapshot(cmd, view, color)
			return nil
		},
	}
	cmd.Flags().IntVar(&width, "width", 90, "Snapshot terminal width")
	cmd.Flags().IntVar(&height, "height", 24, "Snapshot terminal height")
	cmd.Flags().IntVar(&cursor, "cursor", 0, "Selected time filter")
	cmd.Flags().BoolVar(&color, "color", false, "Keep ANSI color escape sequences")
	return cmd
}

func newDebugProfileSnapshotCmd() *cobra.Command {
	var width int
	var height int
	var color bool

	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Render the profile manager without starting the TUI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			view, err := tui.RenderProfileManagerSnapshot(cfg, width, height)
			if err != nil {
				return err
			}
			writeSnapshot(cmd, view, color)
			return nil
		},
	}
	cmd.Flags().IntVar(&width, "width", 120, "Snapshot terminal width")
	cmd.Flags().IntVar(&height, "height", 32, "Snapshot terminal height")
	cmd.Flags().BoolVar(&color, "color", false, "Keep ANSI color escape sequences")
	return cmd
}

func newDebugMCPSnapshotCmd() *cobra.Command {
	var width int
	var height int
	var state string
	var color bool

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Render the MCP manager without starting the TUI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			view, err := tui.RenderMCPManagerSnapshot(cfg, width, height, state)
			if err != nil {
				return err
			}
			writeSnapshot(cmd, view, color)
			return nil
		},
	}
	cmd.Flags().IntVar(&width, "width", 120, "Snapshot terminal width")
	cmd.Flags().IntVar(&height, "height", 32, "Snapshot terminal height")
	cmd.Flags().StringVar(&state, "state", "overview", "State: overview, add-stdio, add-http, add-sse, edit-current, transfer")
	cmd.Flags().BoolVar(&color, "color", false, "Keep ANSI color escape sequences")
	return cmd
}

func newDebugSkillSnapshotCmd() *cobra.Command {
	var width int
	var height int
	var state string
	var color bool

	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Render the skill manager without starting the TUI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := skills.LoadRegistry()
			if err != nil {
				return err
			}
			view, err := tui.RenderSkillManagerSnapshot(registry, width, height, state)
			if err != nil {
				return err
			}
			writeSnapshot(cmd, view, color)
			return nil
		},
	}
	cmd.Flags().IntVar(&width, "width", 120, "Snapshot terminal width")
	cmd.Flags().IntVar(&height, "height", 32, "Snapshot terminal height")
	cmd.Flags().StringVar(&state, "state", "overview", "State: overview, install, catalog, transfer")
	cmd.Flags().BoolVar(&color, "color", false, "Keep ANSI color escape sequences")
	return cmd
}

func writeSnapshot(cmd *cobra.Command, view string, color bool) {
	if !color {
		view = tui.StripANSI(view)
	}
	fmt.Fprintln(cmd.OutOrStdout(), view)
}

func runInteractive() error {
	// Check for version updates in background
	if updateMsg := CheckVersionStartup(); updateMsg != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n\n", updateMsg)
	}

	for {
		summary := tui.DashboardSummary{}
		if path, err := config.ConfigPath(); err == nil {
			summary.ConfigPath = path
		}
		if cfg, err := config.Load(); err == nil {
			summary.CurrentProfile = cfg.DefaultProfile
		}

		choice, err := tui.SelectDashboard("Spark", interactiveDashboardActions(), summary)
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
		case "Token usage":
			snapshot, err := loadTokenUsageSnapshot()
			if err != nil {
				return err
			}
			if err := tui.ShowTokenUsage(snapshot, loadTokenUsageSnapshot); err != nil {
				return err
			}
		case "Manage profiles":
			if err := manageProfiles(); err != nil {
				return err
			}
		case "Manage MCP servers":
			if err := manageMcpServers(); err != nil {
				return err
			}
		case "Manage skills":
			if err := manageSkills(); err != nil {
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

func loadTokenUsageSnapshot() (tui.TokenUsageSnapshot, error) {
	path, err := usage.DefaultPath()
	if err != nil {
		return tui.TokenUsageSnapshot{}, err
	}
	now := time.Now()
	windows, count, err := usage.QueryWindows(path, now)
	if err != nil {
		return tui.TokenUsageSnapshot{}, err
	}
	snapshot := tui.TokenUsageSnapshot{
		SourcePath:  path,
		UpdatedAt:   now,
		RecordCount: count,
	}
	for _, window := range usage.Windows() {
		data := windows[window]
		snapshot.Windows = append(snapshot.Windows, tui.TokenUsageWindow{
			Window:        string(window),
			Label:         usageWindowLabel(window),
			TrendLabel:    tokenUsageTrendLabel(window),
			Summary:       tokenUsageSummary(data.Summary),
			Breakdowns:    tokenUsageBreakdowns(data.Breakdowns),
			DailySeries:   tokenUsageSeries(data.Series, window),
			HeavyRequests: tokenUsageHeavyRequests(data.HeavyRequests),
		})
	}
	return snapshot, nil
}

func usageWindowLabel(window usage.Window) string {
	switch window {
	case usage.WindowToday:
		return "Today"
	case usage.Window7D:
		return "7d"
	case usage.Window30D:
		return "30d"
	case usage.WindowAll:
		return "All"
	default:
		return string(window)
	}
}

func tokenUsageTrendLabel(window usage.Window) string {
	if window == usage.WindowToday {
		return "Hourly trend"
	}
	return "Daily trend"
}

func tokenUsageSummary(summary usage.Summary) tui.TokenUsageSummary {
	return tui.TokenUsageSummary{
		Requests:          summary.Requests,
		InputTokens:       summary.InputTokens,
		OutputTokens:      summary.OutputTokens,
		TotalTokens:       summary.TotalTokens,
		CachedInputTokens: summary.CachedInputTokens,
	}
}

func tokenUsageBreakdowns(rows []usage.Breakdown) []tui.TokenUsageBreakdown {
	out := make([]tui.TokenUsageBreakdown, 0, len(rows))
	for _, row := range rows {
		out = append(out, tui.TokenUsageBreakdown{
			Client:            row.Client,
			Model:             row.Model,
			Requests:          row.Requests,
			InputTokens:       row.InputTokens,
			OutputTokens:      row.OutputTokens,
			TotalTokens:       row.TotalTokens,
			CachedInputTokens: row.CachedInputTokens,
		})
	}
	return out
}

func tokenUsageSeries(rows []usage.DailySummary, window usage.Window) []tui.TokenUsageDailyPoint {
	if window == usage.WindowToday {
		return tokenUsageDailySeries(rows, "15:00")
	}
	return tokenUsageDailySeries(rows, "01-02")
}

func tokenUsageDailySeries(rows []usage.DailySummary, labelFormat string) []tui.TokenUsageDailyPoint {
	out := make([]tui.TokenUsageDailyPoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, tui.TokenUsageDailyPoint{
			Label:             row.Day.Format(labelFormat),
			Requests:          row.Requests,
			InputTokens:       row.InputTokens,
			OutputTokens:      row.OutputTokens,
			TotalTokens:       row.TotalTokens,
			CachedInputTokens: row.CachedInputTokens,
		})
	}
	return out
}

func tokenUsageHeavyRequests(rows []usage.HeavyRequest) []tui.TokenUsageRequest {
	out := make([]tui.TokenUsageRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, tui.TokenUsageRequest{
			Timestamp:         row.Timestamp.In(time.Local),
			Client:            row.Client,
			Model:             row.Model,
			Stream:            row.Stream,
			InputTokens:       row.InputTokens,
			OutputTokens:      row.OutputTokens,
			TotalTokens:       row.TotalTokens,
			CachedInputTokens: row.CachedInputTokens,
		})
	}
	return out
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

func manageSkills() error {
	return tui.ManageSkillsDashboard()
}

func resolveModels(modelFlag string, profile *config.Profile) []string {
	if model := config.NormalizeModel(modelFlag); model != "" {
		return []string{model}
	}
	if profile == nil {
		return nil
	}
	models := config.NormalizeModels(profile.Models)
	defaultModel := config.NormalizeModel(profile.DefaultModel)
	if defaultModel != "" {
		if len(models) == 0 {
			return []string{defaultModel}
		}
		ordered := make([]string, 0, len(models)+1)
		ordered = append(ordered, defaultModel)
		for _, m := range models {
			if m == defaultModel {
				continue
			}
			ordered = append(ordered, m)
		}
		return ordered
	}
	if len(models) > 0 {
		return models
	}
	return nil
}

func profileNames(cfg *config.RootConfig) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
