package app

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"spark/internal/version"
)

const (
	githubRepo = "zhangsanfeng1094/spark"
	cacheAge   = 24 * time.Hour
)

func newVersionCmd() *cobra.Command {
	var checkFlag bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			v := version.Get()
			fmt.Println(v.String())

			if checkFlag {
				return checkForUpdate()
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkFlag, "check", false, "Check for updates")

	return cmd
}

func checkForUpdate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	latest, err := version.CheckLatestVersion(ctx, githubRepo)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	current := version.Get().Version
	fmt.Printf("Current: %s\n", current)
	fmt.Printf("Latest:  %s\n", latest)

	if version.IsUpdateAvailable(current, latest) {
		fmt.Printf("\n✨ Update available! Run: npm i -g spark-agent-launcher\n")
	} else {
		fmt.Println("\n✓ You're up to date!")
	}

	return nil
}

// CheckVersionStartup checks for version updates on startup (non-blocking)
// Returns update message if available, otherwise empty string
func CheckVersionStartup() string {
	cache, err := version.LoadCache()
	if err != nil {
		return ""
	}

	current := version.Get().Version

	// Check if current version is dismissed
	if version.IsDismissed(cache, current) {
		return ""
	}

	// Check if we should fetch new version info
	if version.ShouldCheckVersion(cache, cacheAge) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			latest, err := version.CheckLatestVersion(ctx, githubRepo)
			if err != nil {
				return
			}

			cache.LatestVersion = latest
			cache.LastChecked = time.Now()
			_ = version.SaveCache(cache)
		}()
	}

	// Check if update is available
	if cache.LatestVersion != "" && version.IsUpdateAvailable(current, cache.LatestVersion) {
		return fmt.Sprintf("✨ Update available: %s → %s (run: spark version --check)", current, cache.LatestVersion)
	}

	return ""
}
