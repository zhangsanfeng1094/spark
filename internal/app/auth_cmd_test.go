package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spark/internal/auth"
	"spark/internal/config"
)

func TestAuthCommands_StatusAndLogout(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	store, err := auth.DefaultStore()
	if err != nil {
		t.Fatalf("DefaultStore: %v", err)
	}

	// 1. Initial status with no records
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"auth", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth status: %v", err)
	}
	if !strings.Contains(buf.String(), "No active OAuth/Device login credentials found") {
		t.Fatalf("unexpected output: %s", buf.String())
	}

	// 2. Add an auth record to store and check status
	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderClaude,
		Kind:        auth.KindOAuth,
		AccessToken: "test-token",
		Account:     "dev@anthropic.com",
		ExpiresAt:   time.Now().Add(2 * time.Hour),
	})

	buf.Reset()
	root = NewRootCmd()
	root.SetOut(&buf)
	root.SetArgs([]string{"auth", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth status: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "claude") || !strings.Contains(out, "dev@anthropic.com") || !strings.Contains(out, "Active") {
		t.Fatalf("unexpected status output: %s", out)
	}

	// 3. Logout specific provider
	buf.Reset()
	root = NewRootCmd()
	root.SetOut(&buf)
	root.SetArgs([]string{"logout", "--provider", "claude"})
	if err := root.Execute(); err != nil {
		t.Fatalf("logout claude: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully logged out from claude") {
		t.Fatalf("unexpected logout output: %s", buf.String())
	}

	// 4. Verify store is empty
	rec, _ := store.Get(auth.ProviderClaude)
	if rec != nil {
		t.Fatalf("expected nil record after logout, got %+v", rec)
	}
}

func TestAuthCommands_LogoutAll(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	store := auth.NewStore(filepath.Join(tempDir, ".spark", "auth"))
	_ = store.Save(&auth.Auth{Provider: auth.ProviderClaude, AccessToken: "t1"})
	_ = store.Save(&auth.Auth{Provider: auth.ProviderCodex, AccessToken: "t2"})

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"logout", "--all"})
	if err := root.Execute(); err != nil {
		t.Fatalf("logout all: %v", err)
	}

	list, _ := store.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 records after logout --all, got %d", len(list))
	}
}

func TestAuthCommands_CommandCode(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	store := auth.NewStore(filepath.Join(tempDir, ".spark", "auth"))
	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderCommandCode,
		Kind:        auth.KindOAuth,
		AccessToken: "user_test_token_cc",
		Account:     "dev_cc",
	})

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"auth", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth status: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "commandcode") || !strings.Contains(out, "dev_cc") {
		t.Fatalf("unexpected status output: %s", out)
	}

	buf.Reset()
	root = NewRootCmd()
	root.SetOut(&buf)
	root.SetArgs([]string{"logout", "--provider", "commandcode-proxy"})
	if err := root.Execute(); err != nil {
		t.Fatalf("logout commandcode: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully logged out from commandcode") {
		t.Fatalf("unexpected logout output: %s", buf.String())
	}

	rec, _ := store.Get(auth.ProviderCommandCode)
	if rec != nil {
		t.Fatalf("expected nil record after logout, got %+v", rec)
	}

	// Test direct key login
	buf.Reset()
	root = NewRootCmd()
	root.SetOut(&buf)
	root.SetArgs([]string{"auth", "login", "-p", "commandcode", "-k", "user_4gt_test_key_123"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth login with key: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully saved credentials") {
		t.Fatalf("unexpected login with key output: %s", buf.String())
	}

	saved, err := store.Get(auth.ProviderCommandCode)
	if err != nil || saved == nil {
		t.Fatalf("expected saved record, got %+v (err: %v)", saved, err)
	}
	if saved.AccessToken != "user_4gt_test_key_123" {
		t.Fatalf("expected accessToken 'user_4gt_test_key_123', got %q", saved.AccessToken)
	}

	// Verify profile was auto-created
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Profiles["command-code"] == nil {
		t.Fatalf("expected auto-created profile 'command-code', got %#v", cfg.Profiles)
	}
	if cfg.Profiles["command-code"].AuthProvider != "commandcode" {
		t.Fatalf("expected auth_provider 'commandcode', got %q", cfg.Profiles["command-code"].AuthProvider)
	}
}

func TestAuthCommands_DeviceAndBrowserFlags(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// Testing mutually exclusive flags
	root.SetArgs([]string{"auth", "login", "-p", "codex", "--device", "--browser"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error when passing both --device and --browser")
	}
	if !strings.Contains(err.Error(), "cannot specify both --device and --browser") {
		t.Fatalf("expected conflict error message, got: %v", err)
	}
}
