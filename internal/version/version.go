package version

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Build-time variables injected via ldflags
var (
	// Version is the semantic version (e.g., "0.1.6")
	Version = "dev"
	// GitCommit is the git commit hash
	GitCommit = "unknown"
	// BuildDate is the build timestamp
	BuildDate = "unknown"
)

// GitHubRelease represents a GitHub release (legacy shape kept for compatibility).
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// VersionInfo contains version information
type VersionInfo struct {
	Version   string
	GitCommit string
	BuildDate string
}

// Get returns the current version info
func Get() VersionInfo {
	return VersionInfo{
		Version:   Version,
		GitCommit: GitCommit,
		BuildDate: BuildDate,
	}
}

// String returns a formatted version string
func (v VersionInfo) String() string {
	return fmt.Sprintf("spark %s (commit: %s, built: %s)", v.Version, v.GitCommit, v.BuildDate)
}

// Short returns just the version number
func (v VersionInfo) Short() string {
	return v.Version
}

// CheckLatestVersion checks GitHub releases for the latest version.
// Returns the latest version and an error if the check fails.
func CheckLatestVersion(ctx context.Context, repo string) (string, error) {
	release, err := FetchLatestRelease(ctx, repo)
	if err != nil {
		return "", err
	}
	return release.Version(), nil
}

// Cache represents a version check cache
type Cache struct {
	LastChecked    time.Time `json:"last_checked"`
	LatestVersion  string    `json:"latest_version"`
	DismissedUntil string    `json:"dismissed_until,omitempty"` // Version to skip notifications for
}

// cachePath returns the path to the version cache file
func cachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".spark", "version_cache.json"), nil
}

// LoadCache loads the version cache from disk
func LoadCache() (*Cache, error) {
	path, err := cachePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Cache{}, nil
		}
		return nil, err
	}

	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// SaveCache saves the version cache to disk
func SaveCache(cache *Cache) error {
	path, err := cachePath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// ShouldCheckVersion determines if we should check for a new version
// based on the cache age (default 24 hours)
func ShouldCheckVersion(cache *Cache, maxAge time.Duration) bool {
	if cache == nil {
		return true
	}
	return time.Since(cache.LastChecked) > maxAge
}

// IsDismissed checks if the given version is dismissed
func IsDismissed(cache *Cache, version string) bool {
	if cache == nil {
		return false
	}
	return cache.DismissedUntil == version
}

// CompareVersions compares two semantic versions.
// Returns: -1 if a < b, 0 if a == b, 1 if a > b.
// Non-semver values like "dev" compare as older than any numeric release.
func CompareVersions(a, b string) int {
	a = stripVPrefix(a)
	b = stripVPrefix(b)

	aParts, aOK := parseSemverCore(a)
	bParts, bOK := parseSemverCore(b)
	if !aOK && !bOK {
		return strings.Compare(a, b)
	}
	if !aOK {
		return -1
	}
	if !bOK {
		return 1
	}
	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

func parseSemverCore(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "dev") || strings.EqualFold(v, "unknown") {
		return out, false
	}
	// Drop pre-release / build metadata: 1.2.3-rc.1+build -> 1.2.3
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	n, err := fmt.Sscanf(v, "%d.%d.%d", &out[0], &out[1], &out[2])
	if err != nil || n < 2 {
		// Allow major.minor
		n, err = fmt.Sscanf(v, "%d.%d", &out[0], &out[1])
		if err != nil || n < 2 {
			n, err = fmt.Sscanf(v, "%d", &out[0])
			if err != nil || n < 1 {
				return out, false
			}
		}
	}
	return out, true
}

// IsUpdateAvailable checks if an update is available
func IsUpdateAvailable(current, latest string) bool {
	return CompareVersions(current, latest) < 0
}
