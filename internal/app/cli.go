package app

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"spark/internal/config"
	"spark/internal/httpserver"
	"spark/internal/integrations"
	"spark/internal/skills"
	"spark/internal/tui"
	"spark/internal/usage"
	"spark/internal/version"
)

const (
	ansiReset                 = "\x1b[0m"
	launchIntegrationValueSGR = "\x1b[1;38;2;200;174;252m"
	launchModelValueSGR       = "\x1b[1;38;2;244;201;93m"
	launchProfileValueSGR     = "\x1b[1;38;2;95;211;141m"
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
	root.AddCommand(newUsageCmd())
	root.AddCommand(newProfileCmd())
	root.AddCommand(newHTTPServerCmd())
	root.AddCommand(newDebugCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newUpdateCmd())
	return root
}

func newHTTPServerCmd() *cobra.Command {
	var addr string
	var devUI string
	var openBrowser bool

	cmd := &cobra.Command{
		Use:   "httpserver",
		Short: "Start the local Spark web configuration server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if openBrowser {
				fmt.Fprintln(cmd.ErrOrStderr(), "--open is reserved and is not implemented yet")
			}
			server, err := httpserver.New(httpserver.Options{Addr: addr, DevUI: devUI})
			if err != nil {
				return err
			}
			if httpserver.IsWideListenAddress(server.Addr()) {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: listening on %s exposes the unauthenticated configuration server beyond localhost\n", server.Addr())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Spark HTTP server listening on http://%s\n", server.Addr())
			return server.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "HTTP listen address")
	cmd.Flags().StringVar(&devUI, "dev-ui", "", "Proxy non-API requests to a Vite dev server URL")
	cmd.Flags().BoolVar(&openBrowser, "open", false, "Open a browser after startup (reserved)")
	return cmd
}

func newUsageCmd() *cobra.Command {
	var modelFlag string

	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show recorded compat proxy token usage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			model := strings.TrimSpace(modelFlag)
			snapshot, err := loadTokenUsageSnapshotWithFilter(usage.QueryFilter{Model: model})
			if err != nil {
				return err
			}
			printUsageReport(cmd, snapshot, model)
			return nil
		},
	}
	cmd.Flags().StringVar(&modelFlag, "model", "", "Only show usage for this model")
	return cmd
}

func newLaunchCmd() *cobra.Command {
	var modelFlag string
	var profileFlag string
	var selectProfileFlag bool
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
			if err := validateProfileSelectionFlags(profileFlag, selectProfileFlag); err != nil {
				return err
			}
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
			return launchIntegration(name, LaunchOptions{
				Model:         modelFlag,
				Profile:       profileFlag,
				SelectProfile: selectProfileFlag,
				ConfigOnly:    configOnly,
				PassArgs:      passArgs,
			})
		},
	}
	cmd.Flags().StringVar(&modelFlag, "model", "", "Model name")
	cmd.Flags().StringVar(&profileFlag, "profile", "", "Profile name")
	cmd.Flags().BoolVar(&selectProfileFlag, "select-profile", false, "Select profile before launching")
	cmd.Flags().BoolVar(&configOnly, "config", false, "Configure without launching")
	return cmd
}
func newConfigCmd() *cobra.Command {
	var profileFlag string
	var selectProfileFlag bool
	var modelFlag string
	cmd := &cobra.Command{
		Use:   "config [integration]",
		Short: "Configure integration only",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateProfileSelectionFlags(profileFlag, selectProfileFlag); err != nil {
				return err
			}
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
			return launchIntegration(name, LaunchOptions{
				Model:         modelFlag,
				Profile:       profileFlag,
				SelectProfile: selectProfileFlag,
				ConfigOnly:    true,
			})
		},
	}
	cmd.Flags().StringVar(&modelFlag, "model", "", "Model name")
	cmd.Flags().StringVar(&profileFlag, "profile", "", "Profile name")
	cmd.Flags().BoolVar(&selectProfileFlag, "select-profile", false, "Select profile before configuring")
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

const (
	interactiveActionQuickLaunch    = "Quick launch"
	interactiveActionLaunchOptions  = "Launch options"
	interactiveActionManageSettings = "Manage settings"
)

func interactiveMenuOptions() []string {
	return []string{interactiveActionQuickLaunch, interactiveActionLaunchOptions, interactiveActionManageSettings, "Token usage", "Manage profiles", "Manage MCP servers", "Manage skills", "Show config file", "Quit"}
}

func interactiveMenuDescriptions() map[string]string {
	return map[string]string{
		interactiveActionQuickLaunch:    "Start Spark immediately with the quick launch integration, default profile, and default model.",
		interactiveActionLaunchOptions:  "Choose the integration, profile, and configured model for this launch.",
		interactiveActionManageSettings: "Manage global defaults, prompt injection, Codex model catalog, and launch history.",
		"Token usage":                   "Review recorded compat proxy token usage with fixed time filters.",
		"Manage profiles":               "Edit provider profiles, base URLs, API keys, API type behavior, and model defaults.",
		"Manage MCP servers":            "Manage MCP server entries, health probes, and transport settings.",
		"Manage skills":                 "Browse installed skills, add new ones, and toggle them on or off.",
		"Show config file":              "Print the active Spark config path after leaving the dashboard.",
		"Quit":                          "Exit Spark without making additional changes.",
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
			summary := loadDashboardSummary()
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

func loadDashboardSummary() tui.DashboardSummary {
	summary := tui.DashboardSummary{}
	if path, err := config.ConfigPath(); err == nil {
		summary.ConfigPath = path
	}
	if cfg, err := config.Load(); err == nil {
		applyDashboardConfigSummary(&summary, cfg, integrations.Names())
	}
	return summary
}

func applyDashboardConfigSummary(summary *tui.DashboardSummary, cfg *config.RootConfig, integrationNames []string) {
	if summary == nil || cfg == nil {
		return
	}
	summary.QuickLaunchIntegration = defaultQuickLaunchIntegration(cfg, integrationNames)
	summary.DefaultProfile = cfg.DefaultProfile
	summary.CurrentProfile = cfg.DefaultProfile
	summary.DefaultModel = defaultProfileModel(cfg)
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
		summary := loadDashboardSummary()
		choice, err := tui.SelectDashboard("Spark", interactiveDashboardActions(), summary)
		if err != nil {
			return err
		}
		switch choice {
		case interactiveActionQuickLaunch:
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			selection, err := resolveQuickLaunchDefaults(cfg, integrations.Names())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}
			if err := launchIntegration(selection.Integration, LaunchOptions{Profile: selection.Profile, Model: selection.Model}); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		case interactiveActionLaunchOptions:
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			integrationNames := integrations.Names()
			selection, err := tui.SelectLaunchOptions(integrationNames, cfg, defaultQuickLaunchIntegration(cfg, integrationNames))
			if err != nil {
				if errors.Is(err, tui.ErrCanceled) {
					continue
				}
				return err
			}
			if selection.Model == "" {
				fmt.Fprintf(os.Stderr, "Error: %v\n", missingProfileModelError(selection.Profile))
				continue
			}
			if err := launchIntegration(selection.Integration, LaunchOptions{Profile: selection.Profile, Model: selection.Model}); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		case interactiveActionManageSettings:
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := tui.ManageSettingsDashboard(cfg, integrations.Names()); err != nil {
				return err
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
	return loadTokenUsageSnapshotWithFilter(usage.QueryFilter{})
}

func loadTokenUsageSnapshotWithFilter(filter usage.QueryFilter) (tui.TokenUsageSnapshot, error) {
	path, err := usage.DefaultPath()
	if err != nil {
		return tui.TokenUsageSnapshot{}, err
	}
	now := time.Now()
	windows, count, err := usage.QueryWindowsWithFilter(path, now, filter)
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

func printUsageReport(cmd *cobra.Command, snapshot tui.TokenUsageSnapshot, model string) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Token usage")
	if model != "" {
		fmt.Fprintf(out, "Model: %s\n", model)
	}
	fmt.Fprintf(out, "Records: %s\n", formatUsageInt(snapshot.RecordCount))
	fmt.Fprintf(out, "Source: %s\n\n", snapshot.SourcePath)

	fmt.Fprintf(out, "%-8s %10s %12s %12s %12s %12s\n", "Window", "Requests", "Tokens", "Input", "Output", "Cached")
	for _, window := range snapshot.Windows {
		summary := window.Summary
		fmt.Fprintf(out, "%-8s %10s %12s %12s %12s %12s\n",
			window.Label,
			formatUsageInt(summary.Requests),
			formatUsageInt(summary.TotalTokens),
			formatUsageInt(summary.InputTokens),
			formatUsageInt(summary.OutputTokens),
			formatUsageInt(summary.CachedInputTokens),
		)
	}

	window := usageReportWindow(snapshot)
	if window.Summary.Requests == 0 {
		fmt.Fprintln(out, "\nNo recorded compat proxy usage for this filter.")
		return
	}

	fmt.Fprintln(out, "\nBreakdown by source and model")
	fmt.Fprintf(out, "%-12s %-28s %10s %12s %12s %12s %12s\n", "Source", "Model", "Requests", "Tokens", "Input", "Output", "Cached")
	for _, row := range window.Breakdowns {
		fmt.Fprintf(out, "%-12s %-28s %10s %12s %12s %12s %12s\n",
			fitUsageCell(row.Client, 12),
			fitUsageCell(row.Model, 28),
			formatUsageInt(row.Requests),
			formatUsageInt(row.TotalTokens),
			formatUsageInt(row.InputTokens),
			formatUsageInt(row.OutputTokens),
			formatUsageInt(row.CachedInputTokens),
		)
	}

	if len(window.HeavyRequests) == 0 {
		return
	}
	fmt.Fprintln(out, "\nLargest requests")
	fmt.Fprintf(out, "%-19s %-12s %-28s %12s %12s %12s\n", "Time", "Source", "Model", "Tokens", "In/Out", "Cached")
	for _, row := range window.HeavyRequests {
		fmt.Fprintf(out, "%-19s %-12s %-28s %12s %12s %12s\n",
			row.Timestamp.Format("2006-01-02 15:04"),
			fitUsageCell(row.Client, 12),
			fitUsageCell(row.Model, 28),
			formatUsageInt(row.TotalTokens),
			formatUsageInt(row.InputTokens)+"/"+formatUsageInt(row.OutputTokens),
			formatUsageInt(row.CachedInputTokens),
		)
	}
}

func usageReportWindow(snapshot tui.TokenUsageSnapshot) tui.TokenUsageWindow {
	if len(snapshot.Windows) == 0 {
		return tui.TokenUsageWindow{}
	}
	for _, window := range snapshot.Windows {
		if window.Window == string(usage.WindowAll) {
			return window
		}
	}
	return snapshot.Windows[len(snapshot.Windows)-1]
}

func formatUsageInt(v int) string {
	s := fmt.Sprintf("%d", v)
	parts := []string{}
	for len(s) > 3 {
		parts = append(parts, s[len(s)-3:])
		s = s[:len(s)-3]
	}
	parts = append(parts, s)
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ",")
}

func fitUsageCell(value string, width int) string {
	value = strings.TrimSpace(value)
	if len(value) <= width {
		return value
	}
	if width <= 1 {
		return value[:width]
	}
	return value[:width-1] + "~"
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

type LaunchOptions struct {
	Model         string
	Profile       string
	SelectProfile bool
	ConfigOnly    bool
	PassArgs      []string
}

type profilePicker func(title string, options []string) (string, error)

func resolveQuickLaunchDefaults(cfg *config.RootConfig, integrationNames []string) (tui.LaunchSelection, error) {
	selection := tui.LaunchSelection{}
	if cfg == nil {
		return selection, fmt.Errorf("config is required")
	}
	selection.Integration = defaultQuickLaunchIntegration(cfg, integrationNames)
	if selection.Integration == "" {
		return selection, fmt.Errorf("no integrations available")
	}
	selection.Profile = cfg.DefaultProfile
	profile, err := cfg.ProfileByName(selection.Profile)
	if err != nil {
		return selection, fmt.Errorf("default profile %q is not configured; open Manage profiles to choose a default profile", emptyFallback(selection.Profile, "not set"))
	}
	models := config.EffectiveProfileModels(profile)
	if len(models) > 0 {
		selection.Model = models[0]
	}
	if selection.Model == "" {
		return selection, missingDefaultProfileModelError(selection.Profile)
	}
	return selection, nil
}

func defaultQuickLaunchIntegration(cfg *config.RootConfig, integrationNames []string) string {
	if len(integrationNames) == 0 {
		return ""
	}
	defaultIntegration := ""
	lastSelection := ""
	if cfg != nil {
		defaultIntegration = strings.TrimSpace(cfg.DefaultIntegration)
		lastSelection = strings.TrimSpace(cfg.History.LastSelection)
	}
	if matched := firstMatchingIntegration(defaultIntegration, integrationNames); matched != "" {
		return matched
	}
	if matched := firstMatchingIntegration(lastSelection, integrationNames); matched != "" {
		return matched
	}
	return integrationNames[0]
}

func firstMatchingIntegration(want string, integrationNames []string) string {
	want = strings.TrimSpace(want)
	if want == "" {
		return ""
	}
	for _, name := range integrationNames {
		if strings.EqualFold(want, name) {
			return name
		}
	}
	return ""
}

func defaultProfileModel(cfg *config.RootConfig) string {
	if cfg == nil {
		return ""
	}
	profile, err := cfg.ProfileByName(cfg.DefaultProfile)
	if err != nil {
		return ""
	}
	models := config.EffectiveProfileModels(profile)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func missingProfileModelError(profileName string) error {
	return fmt.Errorf("no model configured for profile %q; open Manage profiles to set default_model or models", emptyFallback(profileName, "not set"))
}

func missingDefaultProfileModelError(profileName string) error {
	return fmt.Errorf("no model configured for default profile %q; open Manage profiles to set default_model or models", emptyFallback(profileName, "not set"))
}

func emptyFallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func launchIntegration(name string, opts LaunchOptions) error {
	if err := validateProfileSelectionFlags(opts.Profile, opts.SelectProfile); err != nil {
		return err
	}
	r, ok := integrations.Get(name)
	if !ok {
		return fmt.Errorf("unknown integration: %s", name)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	profileName, profile, err := resolveLaunchProfile(cfg, opts.Profile, opts.SelectProfile, tui.SelectOne)
	if err != nil {
		return err
	}

	models := resolveModels(opts.Model, profile)

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
		ok, err := confirmEditorLaunch(r.String(), profileName, models, ed.Paths())
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

	cfg.RecordLaunch(name, profileName, models[0])
	if err := config.Save(cfg); err != nil {
		return err
	}

	if opts.ConfigOnly {
		launchNow, err := tui.ConfirmDetails(tui.ConfirmRequest{
			Title:   "Config written — launch " + r.String() + "?",
			Summary: "Spark already updated this integration's config for the selected model.",
			Details: []string{
				"Integration: " + r.String(),
				"Profile:     " + profileName,
				"Model:       " + models[0],
			},
			Footnote:       "Choose Launch to start the agent now, or Not now to stop after writing config.",
			ConfirmLabel:   "Launch now",
			CancelLabel:    "Not now",
			DefaultConfirm: false,
		})
		if err != nil {
			return err
		}
		if !launchNow {
			return nil
		}
	}

	fmt.Println(formatLaunchLine(r.String(), models[0], profileName))
	prompt, err := cfg.ResolvePromptInjection(strings.ToLower(name), models[0])
	if err != nil {
		return err
	}
	if prompt != nil {
		fmt.Printf("✓ Prompt injection: %s (mode: %s)\n", prompt.Path, prompt.Mode)
	} else if cfg.Prompts.IsEnabled() {
		fmt.Printf("⚠ Prompt injection: not configured for %s/%s\n", strings.ToLower(name), models[0])
	}
	integration := cfg.Integration(name)
	if cr, ok := r.(integrations.ConfiguredPromptRunner); ok {
		return cr.RunWithConfigAndPrompt(profile, integration, models[0], opts.PassArgs, prompt)
	}
	if prompt != nil {
		if pr, ok := r.(integrations.PromptRunner); ok {
			return pr.RunWithPrompt(profile, models[0], opts.PassArgs, prompt)
		}
		return fmt.Errorf("prompt binding exists for %s/%s, but integration does not support prompt injection", strings.ToLower(name), models[0])
	}
	return r.Run(profile, models[0], opts.PassArgs)
}

func formatLaunchLine(integration, model, profileName string) string {
	return fmt.Sprintf("Launching %s with %s using profile %s",
		colorLaunchValue(launchIntegrationValueSGR, integration),
		colorLaunchValue(launchModelValueSGR, model),
		colorLaunchValue(launchProfileValueSGR, profileName),
	)
}

// confirmEditorLaunch asks before Spark rewrites an integration's on-disk config.
func confirmEditorLaunch(integration, profileName string, models, paths []string) (bool, error) {
	return tui.ConfirmDetails(editorLaunchConfirmRequest(integration, profileName, models, paths, config.BackupDir()))
}

func editorLaunchConfirmRequest(integration, profileName string, models, paths []string, backupDir string) tui.ConfirmRequest {
	details := make([]string, 0, len(paths)+6)
	details = append(details,
		"Integration: "+integration,
		"Profile:     "+profileName,
	)
	if len(models) == 1 {
		details = append(details, "Model:       "+models[0])
	} else if len(models) > 1 {
		details = append(details, "Models:      "+strings.Join(models, ", "))
	}
	if len(paths) > 0 {
		details = append(details, "Files Spark will update:")
		for _, p := range paths {
			details = append(details, "  "+p)
		}
	}
	details = append(details,
		"Spark only touches its own managed entries (e.g. spark-* models).",
		"Your other settings, logins, and defaults stay as they are.",
	)
	return tui.ConfirmRequest{
		Title:          "Apply Spark config for " + integration + "?",
		Summary:        "Before launching, Spark writes provider/model settings so this agent uses your selected profile.",
		Details:        details,
		Footnote:       "Backups (if any): " + backupDir + "  ·  Esc cancels without writing.",
		ConfirmLabel:   "Write config & launch",
		CancelLabel:    "Cancel",
		DefaultConfirm: true,
	}
}

func colorLaunchValue(sgr, value string) string {
	return sgr + value + ansiReset
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
	return config.EffectiveProfileModels(profile)
}

func validateProfileSelectionFlags(profileFlag string, selectProfile bool) error {
	if strings.TrimSpace(profileFlag) != "" && selectProfile {
		return fmt.Errorf("--profile and --select-profile cannot be used together")
	}
	return nil
}

func resolveLaunchProfile(cfg *config.RootConfig, profileFlag string, selectProfile bool, picker profilePicker) (string, *config.Profile, error) {
	if err := validateProfileSelectionFlags(profileFlag, selectProfile); err != nil {
		return "", nil, err
	}

	if profileName := strings.TrimSpace(profileFlag); profileName != "" {
		profile, err := cfg.ProfileByName(profileName)
		return profileName, profile, err
	}

	if selectProfile {
		return pickLaunchProfile(cfg, picker)
	}

	profileName := cfg.DefaultProfile
	profile, err := cfg.ProfileByName(profileName)
	if err == nil {
		return profileName, profile, nil
	}
	return pickLaunchProfile(cfg, picker)
}

func pickLaunchProfile(cfg *config.RootConfig, picker profilePicker) (string, *config.Profile, error) {
	chosen, err := picker("Select profile:", profileNames(cfg))
	if err != nil {
		return "", nil, err
	}
	profile, err := cfg.ProfileByName(chosen)
	if err != nil {
		return "", nil, err
	}
	return chosen, profile, nil
}

func profileNames(cfg *config.RootConfig) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
