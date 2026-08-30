package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// DefaultGitHubRepo is the public repository used for release checks and binary updates.
const DefaultGitHubRepo = "zhangsanfeng1094/spark"

// NPMPackageName is the published npm package name for global installs.
const NPMPackageName = "@ngominhbinh708/spark"

// ReleaseAsset is one downloadable file on a GitHub release.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Release is a GitHub release with downloadable assets.
type Release struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []ReleaseAsset `json:"assets"`
}

// Version returns the release tag without a leading "v".
func (r Release) Version() string {
	return stripVPrefix(r.TagName)
}

// AssetByName finds an asset by exact name.
func (r Release) AssetByName(name string) (ReleaseAsset, bool) {
	name = strings.TrimSpace(name)
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return ReleaseAsset{}, false
}

// HTTPDoer is the subset of http.Client used for release lookups and downloads.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

var defaultHTTPClient HTTPDoer = &http.Client{Timeout: 30 * time.Second}

// SetHTTPClient overrides the HTTP client used by release helpers. Intended for tests.
func SetHTTPClient(client HTTPDoer) (restore func()) {
	previous := defaultHTTPClient
	if client == nil {
		defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}
	} else {
		defaultHTTPClient = client
	}
	return func() { defaultHTTPClient = previous }
}

// FetchLatestRelease loads the latest GitHub release for repo.
func FetchLatestRelease(ctx context.Context, repo string) (*Release, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = DefaultGitHubRepo
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "spark-agent-launcher")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			return nil, fmt.Errorf("failed to check version: status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("failed to check version: status %d: %s", resp.StatusCode, msg)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return nil, fmt.Errorf("latest release is missing tag_name")
	}
	return &release, nil
}

// PlatformBinaryName returns the GoReleaser asset name for the current or given platform.
// Examples: spark-linux-amd64, spark-darwin-arm64, spark-windows-amd64.exe
func PlatformBinaryName(goos, goarch string) (string, error) {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	osName := goos
	switch goos {
	case "darwin", "linux", "windows":
		// ok
	default:
		return "", fmt.Errorf("unsupported OS for binary update: %s", goos)
	}

	archName := goarch
	switch goarch {
	case "amd64", "arm64":
		// ok
	case "x86_64":
		archName = "amd64"
	case "aarch64":
		archName = "arm64"
	default:
		return "", fmt.Errorf("unsupported architecture for binary update: %s", goarch)
	}

	name := fmt.Sprintf("spark-%s-%s", osName, archName)
	if goos == "windows" {
		name += ".exe"
	}
	return name, nil
}

// DownloadURL builds the direct GitHub release download URL for a version and asset.
func DownloadURL(repo, version, assetName string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = DefaultGitHubRepo
	}
	version = stripVPrefix(version)
	return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", repo, version, assetName)
}

func stripVPrefix(version string) string {
	version = strings.TrimSpace(version)
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		return version[1:]
	}
	return version
}
