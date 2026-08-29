package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"spark/internal/version"
)

func TestRunUpdateCheckOnlyUpToDate(t *testing.T) {
	prevVersion := version.Version
	version.Version = "9.9.9"
	t.Cleanup(func() { version.Version = prevVersion })

	server, restore := startReleaseServer(t, "v9.9.9")
	defer server.Close()
	defer restore()

	var out bytes.Buffer
	if err := runUpdate(context.Background(), &out, &out, updateOptions{CheckOnly: true}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if !strings.Contains(out.String(), "You're up to date") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunUpdateCheckOnlyAvailable(t *testing.T) {
	prevVersion := version.Version
	version.Version = "0.1.0"
	t.Cleanup(func() { version.Version = prevVersion })

	server, restore := startReleaseServer(t, "v0.2.0")
	defer server.Close()
	defer restore()

	var out bytes.Buffer
	if err := runUpdate(context.Background(), &out, &out, updateOptions{CheckOnly: true}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Update available") {
		t.Fatalf("expected update available, got %s", text)
	}
	if !strings.Contains(text, "spark update") {
		t.Fatalf("expected install hint, got %s", text)
	}
}

func TestRunUpdatePackageManaged(t *testing.T) {
	prevVersion := version.Version
	version.Version = "0.1.0"
	t.Cleanup(func() { version.Version = prevVersion })

	t.Setenv(version.EnvManagedByNPM, "1")
	t.Setenv(version.EnvManagedByBun, "")
	t.Setenv(version.EnvManagedByPNPM, "")
	t.Setenv(version.EnvManagedPackageRoot, t.TempDir())

	server, restore := startReleaseServer(t, "v0.2.0")
	defer server.Close()
	defer restore()

	var installed version.InstallMethod
	prev := runPackageInstall
	runPackageInstall = func(ctx context.Context, method version.InstallMethod, stdout, stderr io.Writer) error {
		installed = method
		fmt.Fprintln(stdout, "fake npm ok")
		return nil
	}
	t.Cleanup(func() { runPackageInstall = prev })

	var out bytes.Buffer
	if err := runUpdate(context.Background(), &out, &out, updateOptions{}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if installed != version.InstallNPM {
		t.Fatalf("installed via %q want npm", installed)
	}
	if !strings.Contains(out.String(), "Update ran successfully") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunUpdateStandalone(t *testing.T) {
	prevVersion := version.Version
	version.Version = "0.1.0"
	t.Cleanup(func() { version.Version = prevVersion })

	t.Setenv(version.EnvManagedByNPM, "")
	t.Setenv(version.EnvManagedByBun, "")
	t.Setenv(version.EnvManagedByPNPM, "")
	t.Setenv(version.EnvManagedPackageRoot, "")

	server, restore := startReleaseServer(t, "v0.2.0")
	defer server.Close()
	defer restore()

	called := false
	prev := replaceExecutable
	replaceExecutable = func(ctx context.Context, targetPath string, release *version.Release) error {
		called = true
		if release == nil || release.Version() != "0.2.0" {
			return fmt.Errorf("unexpected release")
		}
		if strings.TrimSpace(targetPath) == "" {
			return fmt.Errorf("empty target")
		}
		return nil
	}
	t.Cleanup(func() { replaceExecutable = prev })

	var out bytes.Buffer
	if err := runUpdate(context.Background(), &out, &out, updateOptions{}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if !called {
		t.Fatal("expected standalone replace to be called")
	}
	if !strings.Contains(out.String(), "Update ran successfully") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestNewUpdateCmdRegistered(t *testing.T) {
	root := NewRootCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "update" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("update command not registered")
	}
}

func startReleaseServer(t *testing.T, tag string) (*httptest.Server, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(version.Release{
			TagName: tag,
			HTMLURL: "https://example.test/release",
			Assets:  nil,
		})
	}))

	client := server.Client()
	restore := version.SetHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Host, "api.github.com") {
			u := *req.URL
			u.Scheme = "http"
			u.Host = server.Listener.Addr().String()
			u.Path = "/"
			cloned := req.Clone(req.Context())
			cloned.URL = &u
			cloned.Host = u.Host
			return client.Do(cloned)
		}
		return client.Do(req)
	}))
	return server, restore
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }
