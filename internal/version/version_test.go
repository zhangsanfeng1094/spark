package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.2.0", "1.1.9", 1},
		{"v0.14.0", "0.13.1", 1},
		{"dev", "0.1.0", -1},
		{"0.1.0-rc.1", "0.1.0", 0},
		{"2.0", "1.9.9", 1},
	}
	for _, tc := range cases {
		got := CompareVersions(tc.a, tc.b)
		if got != tc.want {
			t.Fatalf("CompareVersions(%q, %q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestIsUpdateAvailable(t *testing.T) {
	if !IsUpdateAvailable("0.1.0", "0.2.0") {
		t.Fatal("expected update available")
	}
	if IsUpdateAvailable("0.2.0", "0.2.0") {
		t.Fatal("expected no update")
	}
	if !IsUpdateAvailable("dev", "0.1.0") {
		t.Fatal("dev should be older than a release")
	}
}

func TestPlatformBinaryName(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "spark-linux-amd64"},
		{"linux", "arm64", "spark-linux-arm64"},
		{"darwin", "arm64", "spark-darwin-arm64"},
		{"windows", "amd64", "spark-windows-amd64.exe"},
	}
	for _, tc := range cases {
		got, err := PlatformBinaryName(tc.goos, tc.goarch)
		if err != nil {
			t.Fatalf("PlatformBinaryName(%s,%s): %v", tc.goos, tc.goarch, err)
		}
		if got != tc.want {
			t.Fatalf("PlatformBinaryName(%s,%s)=%q want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
	if _, err := PlatformBinaryName("plan9", "amd64"); err == nil {
		t.Fatal("expected error for unsupported OS")
	}
}

func TestParseChecksum(t *testing.T) {
	linuxSum := strings.Repeat("a", 64)
	darwinSum := strings.Repeat("b", 64)
	content := fmt.Sprintf(`
# comment
%s spark-linux-amd64
%s  ./spark-darwin-arm64
`, linuxSum, darwinSum)
	sum, ok := parseChecksum(content, "spark-linux-amd64")
	if !ok || sum != linuxSum {
		t.Fatalf("got sum=%q ok=%v", sum, ok)
	}
	sum, ok = parseChecksum(content, "spark-darwin-arm64")
	if !ok || sum != darwinSum {
		t.Fatalf("got sum=%q ok=%v", sum, ok)
	}
	if _, ok := parseChecksum(content, "missing"); ok {
		t.Fatal("expected missing asset")
	}
}

func TestFetchLatestReleaseAndReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spark")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	newPayload := []byte("new-binary-contents-v9")
	sum := sha256.Sum256(newPayload)
	sumHex := hex.EncodeToString(sum[:])
	assetName, err := PlatformBinaryName("", "")
	if err != nil {
		t.Fatal(err)
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/zhangsanfeng1094/spark/releases/latest":
			_ = json.NewEncoder(w).Encode(Release{
				TagName: "v9.9.9",
				HTMLURL: "https://example.test/release",
				Assets: []ReleaseAsset{
					{Name: assetName, BrowserDownloadURL: serverURL + "/bin", Size: int64(len(newPayload))},
					{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/sum"},
				},
			})
		case "/bin":
			_, _ = w.Write(newPayload)
		case "/sum":
			fmt.Fprintf(w, "%s  %s\n", sumHex, assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := server.Client()
	restore := SetHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		u := *req.URL
		if strings.Contains(req.URL.Host, "api.github.com") {
			u.Scheme = "http"
			u.Host = server.Listener.Addr().String()
			u.Path = "/repos/zhangsanfeng1094/spark/releases/latest"
		} else if strings.HasPrefix(req.URL.String(), "http://example") || req.URL.Host == "example.invalid" {
			// not used
		} else if strings.Contains(req.URL.Path, "/bin") || strings.HasSuffix(req.URL.Path, "/bin") {
			u.Scheme = "http"
			u.Host = server.Listener.Addr().String()
			u.Path = "/bin"
		} else if strings.Contains(req.URL.Path, "/sum") || strings.HasSuffix(req.URL.Path, "/sum") {
			u.Scheme = "http"
			u.Host = server.Listener.Addr().String()
			u.Path = "/sum"
		} else if req.URL.Host == strings.TrimPrefix(server.URL, "http://") || req.URL.Host == server.Listener.Addr().String() {
			return client.Do(req)
		} else {
			// Absolute URLs pointing at serverURL
			if strings.HasPrefix(req.URL.String(), server.URL) {
				return client.Do(req)
			}
		}
		cloned := req.Clone(req.Context())
		cloned.URL = &u
		cloned.Host = u.Host
		return client.Do(cloned)
	}))
	defer restore()

	release, err := FetchLatestRelease(context.Background(), DefaultGitHubRepo)
	if err != nil {
		t.Fatalf("FetchLatestRelease: %v", err)
	}
	if release.Version() != "9.9.9" {
		t.Fatalf("version=%q", release.Version())
	}

	// Ensure asset URLs point at the test server (handler used serverURL at encode time).
	if asset, ok := release.AssetByName(assetName); !ok || !strings.Contains(asset.BrowserDownloadURL, server.Listener.Addr().String()) && !strings.HasPrefix(asset.BrowserDownloadURL, server.URL) {
		// Rebind in case serverURL was empty during first encode — re-fetch after serverURL set.
		release, err = FetchLatestRelease(context.Background(), DefaultGitHubRepo)
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := ReplaceExecutable(context.Background(), target, release); err != nil {
		t.Fatalf("ReplaceExecutable: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newPayload) {
		t.Fatalf("target contents=%q want %q", got, newPayload)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestDetectInstallInfoFromEnv(t *testing.T) {
	t.Setenv(EnvManagedByNPM, "1")
	t.Setenv(EnvManagedByBun, "")
	t.Setenv(EnvManagedByPNPM, "")
	t.Setenv(EnvManagedPackageRoot, "/tmp/fake-spark-root")

	info, err := DetectInstallInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Method != InstallNPM {
		t.Fatalf("method=%q want npm", info.Method)
	}
	if info.PackageRoot != "/tmp/fake-spark-root" {
		t.Fatalf("package root=%q", info.PackageRoot)
	}
	if !info.PackageManaged() {
		t.Fatal("expected package managed")
	}
	if !strings.Contains(info.UpdateCommand(), "npm install -g") {
		t.Fatalf("update command=%q", info.UpdateCommand())
	}
}

func TestDetectMethodFromPath(t *testing.T) {
	if got := detectMethodFromPath("/home/u/.bun/install/global/node_modules/@ngominhbinh708/spark/vendor/spark", ""); got != InstallBun {
		t.Fatalf("got %q want bun", got)
	}
	if got := detectMethodFromPath("/home/u/.nvm/versions/node/v20/lib/node_modules/@ngominhbinh708/spark/node_modules/@ngominhbinh708/spark-linux-x64/vendor/spark", ""); got != InstallNPM {
		t.Fatalf("got %q want npm", got)
	}
	if got := detectMethodFromPath("/usr/local/bin/spark", ""); got != InstallStandalone {
		t.Fatalf("got %q want standalone", got)
	}
}

func TestInstallInfoUpdateCommand(t *testing.T) {
	cases := []struct {
		method InstallMethod
		want   string
	}{
		{InstallNPM, "npm install -g @ngominhbinh708/spark@latest"},
		{InstallBun, "bun install -g @ngominhbinh708/spark@latest"},
		{InstallPNPM, "pnpm add -g @ngominhbinh708/spark@latest"},
	}
	for _, tc := range cases {
		got := (InstallInfo{Method: tc.method}).UpdateCommand()
		if got != tc.want {
			t.Fatalf("method %s: got %q want %q", tc.method, got, tc.want)
		}
	}
}

func TestDownloadURL(t *testing.T) {
	got := DownloadURL(DefaultGitHubRepo, "v1.2.3", "spark-linux-amd64")
	want := "https://github.com/zhangsanfeng1094/spark/releases/download/v1.2.3/spark-linux-amd64"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
