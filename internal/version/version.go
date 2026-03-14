package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

// GitHubRelease represents a GitHub release
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

// CheckLatestVersion checks GitHub releases for the latest version
// Returns the latest version and an error if the check fails
func CheckLatestVersion(ctx context.Context, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "spark-agent-launcher")
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to check version: status %d", resp.StatusCode)
	}
	
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	
	// Strip the 'v' prefix if present
	version := release.TagName
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	
	return version, nil
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

// CompareVersions compares two semantic versions
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func CompareVersions(a, b string) int {
	// Simple comparison - assumes valid semver
	// For production, consider using a proper semver library
	var aMajor, aMinor, aPatch int
	var bMajor, bMinor, bPatch int
	
	fmt.Sscanf(a, "%d.%d.%d", &aMajor, &aMinor, &aPatch)
	fmt.Sscanf(b, "%d.%d.%d", &bMajor, &bMinor, &bPatch)
	
	if aMajor != bMajor {
		if aMajor < bMajor {
			return -1
		}
		return 1
	}
	if aMinor != bMinor {
		if aMinor < bMinor {
			return -1
		}
		return 1
	}
	if aPatch != bPatch {
		if aPatch < bPatch {
			return -1
		}
		return 1
	}
	return 0
}

// IsUpdateAvailable checks if an update is available
func IsUpdateAvailable(current, latest string) bool {
	return CompareVersions(current, latest) < 0
}
