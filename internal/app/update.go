package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"spark/internal/version"
)

func newUpdateCmd() *cobra.Command {
	var checkOnly bool
	var force bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Spark to the latest version",
		Long: `Update Spark to the latest published release.

Install method is detected automatically:
  - npm / bun / pnpm global installs re-run the package manager
  - standalone binaries download the matching GitHub release asset

Examples:
  spark update
  spark update --check
  spark update --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), updateOptions{
				CheckOnly: checkOnly,
				Force:     force,
			})
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check whether an update is available")
	cmd.Flags().BoolVar(&force, "force", false, "Reinstall even when already on the latest version")
	return cmd
}

type updateOptions struct {
	CheckOnly bool
	Force     bool
}

// runPackageInstall is swapped in tests.
var runPackageInstall = func(ctx context.Context, method version.InstallMethod, stdout, stderr io.Writer) error {
	var cmdline string
	switch method {
	case version.InstallBun:
		cmdline = "bun install -g " + version.NPMPackageName + "@latest"
	case version.InstallPNPM:
		cmdline = "pnpm add -g " + version.NPMPackageName + "@latest"
	default:
		cmdline = "npm install -g " + version.NPMPackageName + "@latest"
	}

	fmt.Fprintf(stdout, "Updating Spark via `%s`...\n\n", cmdline)

	parts := strings.Fields(cmdline)
	c := exec.CommandContext(ctx, parts[0], parts[1:]...)
	c.Stdout = stdout
	c.Stderr = stderr
	c.Env = os.Environ()
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", cmdline, err)
	}
	return nil
}

// replaceExecutable is swapped in tests.
var replaceExecutable = version.ReplaceExecutable

func runUpdate(ctx context.Context, stdout, stderr io.Writer, opts updateOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
	}

	info, err := version.DetectInstallInfo()
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not resolve executable path: %v\n", err)
		info = version.InstallInfo{Method: version.InstallUnknown}
	}

	current := version.Get().Version
	fmt.Fprintf(stdout, "Current version: %s\n", current)
	fmt.Fprintf(stdout, "Install method:  %s\n", displayInstallMethod(info))
	if info.Executable != "" {
		fmt.Fprintf(stdout, "Executable:      %s\n", info.Executable)
	}

	fmt.Fprintln(stdout, "Checking GitHub releases...")
	release, err := version.FetchLatestRelease(ctx, version.DefaultGitHubRepo)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	latest := release.Version()
	fmt.Fprintf(stdout, "Latest version:  %s\n", latest)
	if release.HTMLURL != "" {
		fmt.Fprintf(stdout, "Release:         %s\n", release.HTMLURL)
	}

	updateAvailable := version.IsUpdateAvailable(current, latest)
	if !updateAvailable && !opts.Force {
		fmt.Fprintln(stdout, "\n✓ You're up to date!")
		return nil
	}
	if !updateAvailable && opts.Force {
		fmt.Fprintln(stdout, "\nAlready on latest; --force will reinstall.")
	} else {
		fmt.Fprintf(stdout, "\nUpdate available: %s → %s\n", current, latest)
	}

	if opts.CheckOnly {
		fmt.Fprintf(stdout, "Run `spark update` to install.\n")
		// Non-zero would break scripts that only want to probe; keep success
		// but print clearly. Callers can parse the message.
		return nil
	}

	switch {
	case info.PackageManaged():
		if err := runPackageInstall(ctx, info.Method, stdout, stderr); err != nil {
			return err
		}
	case info.Method == version.InstallStandalone, info.Method == version.InstallUnknown:
		target := info.Executable
		if target == "" {
			return fmt.Errorf("cannot update standalone install: executable path unknown")
		}
		assetName, err := version.PlatformBinaryName("", "")
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "\nDownloading %s...\n", assetName)
		if err := replaceExecutable(ctx, target, release); err != nil {
			return fmt.Errorf("binary update failed: %w\n\nYou can also reinstall manually:\n  %s", err, fallbackManualCommand(info))
		}
		fmt.Fprintf(stdout, "Installed to %s\n", target)
	default:
		return fmt.Errorf("unsupported install method %q; try: %s", info.Method, fallbackManualCommand(info))
	}

	// Refresh cache so startup banner stays quiet after a successful update.
	if cache, err := version.LoadCache(); err == nil {
		cache.LatestVersion = latest
		cache.LastChecked = time.Now()
		_ = version.SaveCache(cache)
	}

	fmt.Fprintln(stdout, "\n🎉 Update ran successfully! Please restart Spark.")
	if info.PackageManaged() {
		fmt.Fprintf(stdout, "Updated via %s (%s).\n", info.Method, version.NPMPackageName)
	} else {
		fmt.Fprintf(stdout, "Updated standalone binary to %s.\n", latest)
	}
	return nil
}

func displayInstallMethod(info version.InstallInfo) string {
	switch info.Method {
	case version.InstallNPM:
		return "npm global package"
	case version.InstallBun:
		return "bun global package"
	case version.InstallPNPM:
		return "pnpm global package"
	case version.InstallStandalone:
		return "standalone binary"
	default:
		if info.PackageRoot != "" {
			return "package-managed (unknown manager)"
		}
		return "unknown (will try standalone binary replace)"
	}
}

func fallbackManualCommand(info version.InstallInfo) string {
	if info.PackageManaged() {
		return info.UpdateCommand()
	}
	return fmt.Sprintf("download from https://github.com/%s/releases/latest", version.DefaultGitHubRepo)
}
