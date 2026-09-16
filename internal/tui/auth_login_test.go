package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"spark/internal/auth"
	"spark/internal/config"
)

func TestAuthManagerModel_RenderAndView(t *testing.T) {
	tempDir := t.TempDir()
	store := auth.NewStore(filepath.Join(tempDir, "auth"))

	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderClaude,
		Kind:        auth.KindOAuth,
		AccessToken: "test-token",
		Account:     "user@anthropic.com",
		ExpiresAt:   time.Now().Add(2 * time.Hour),
	})

	m := newAuthManagerModel(store)

	view := m.View()
	if !strings.Contains(view, "Claude Code") {
		t.Fatalf("expected Claude Code in view: %s", view)
	}
	if !strings.Contains(view, "Command Code") {
		t.Fatalf("expected Command Code in view: %s", view)
	}
	if !strings.Contains(view, "[Active]") {
		t.Fatalf("expected Active status in view: %s", view)
	}
	if !strings.Contains(view, "user@anthropic.com") {
		t.Fatalf("expected account in view: %s", view)
	}

	// Move down selection
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := newM.(*authManagerModel)
	if updated.selected != 1 {
		t.Fatalf("expected selected 1, got %d", updated.selected)
	}
}

func TestAuthManagerModel_LogoutFlow(t *testing.T) {
	tempDir := t.TempDir()
	store := auth.NewStore(filepath.Join(tempDir, "auth"))

	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderClaude,
		AccessToken: "test-token",
	})

	m := newAuthManagerModel(store)

	// Trigger logout key 'x'
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	updated := newM.(*authManagerModel)
	if !updated.confirmLogout {
		t.Fatalf("expected confirmLogout=true")
	}

	// Confirm with 'y'
	newM, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	updated = newM.(*authManagerModel)
	if updated.confirmLogout {
		t.Fatalf("expected confirmLogout=false")
	}

	rec, _ := store.Get(auth.ProviderClaude)
	if rec != nil {
		t.Fatalf("expected record deleted, got %+v", rec)
	}
}

func TestAuthManagerModel_ManualInputWhileLoggingIn(t *testing.T) {
	tempDir := t.TempDir()
	store := auth.NewStore(filepath.Join(tempDir, "auth"))

	m := newAuthManagerModel(store)
	m.loggingIn = true
	m.loginMode = "browser"
	m.manualCh = make(chan string, 1)

	// Verify initial view shows paste prompt
	v := m.View()
	if !strings.Contains(v, "Paste API Key") {
		t.Fatalf("expected 'Paste API Key' in view: %s", v)
	}

	// Type 'user_test'
	for _, r := range "user_test" {
		newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = newM.(*authManagerModel)
	}
	if m.manualInput != "user_test" {
		t.Fatalf("expected manualInput 'user_test', got %q", m.manualInput)
	}

	// Test backspace
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = newM.(*authManagerModel)
	if m.manualInput != "user_tes" {
		t.Fatalf("expected manualInput 'user_tes', got %q", m.manualInput)
	}

	// Submit with Enter
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(*authManagerModel)

	select {
	case received := <-m.manualCh:
		if received != "user_tes" {
			t.Fatalf("expected received 'user_tes', got %q", received)
		}
	default:
		t.Fatalf("expected manualCh to receive input")
	}
}

func TestAuthManagerModel_LoginFinishedCreatesProfile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	store := auth.NewStore(filepath.Join(tempDir, "auth"))
	m := newAuthManagerModel(store)

	newM, _ := m.Update(authLoginFinishedMsg{
		Provider: auth.ProviderCommandCode,
		Auth: &auth.Auth{
			Provider:    auth.ProviderCommandCode,
			Kind:        auth.KindOAuth,
			AccessToken: "user_test_token_finish",
		},
	})
	updated := newM.(*authManagerModel)
	if !strings.Contains(updated.status, "Created profile 'command-code'") && !strings.Contains(updated.status, "Configured profile") {
		t.Fatalf("expected status mentioning profile creation, got %q", updated.status)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.Profiles["command-code"] == nil {
		t.Fatalf("expected auto-created profile 'command-code', got %#v", cfg.Profiles)
	}
}

func TestAuthManagerModel_DualLoginModes(t *testing.T) {
	tempDir := t.TempDir()
	store := auth.NewStore(filepath.Join(tempDir, "auth"))

	m := newAuthManagerModel(store)

	// Find index for Codex
	codexIdx := -1
	for i, spec := range m.specs {
		if spec.Provider == auth.ProviderCodex {
			codexIdx = i
			break
		}
	}
	if codexIdx == -1 {
		t.Fatalf("codex provider not found in specs")
	}

	m.selected = codexIdx
	view := m.View()

	// Verify both auth methods are presented
	if !strings.Contains(view, "[B] Browser OAuth") || !strings.Contains(view, "[D] Device Code Flow") {
		t.Fatalf("expected view to show both Browser and Device options, got:\n%s", view)
	}

	// 1. Press 'b' to initiate browser login for Codex
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	updated := newM.(*authManagerModel)
	if !updated.loggingIn || updated.loginMode != "browser" {
		t.Fatalf("expected browser login initiated for codex, got loggingIn=%v mode=%s", updated.loggingIn, updated.loginMode)
	}

	// Cancel login
	newM, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated = newM.(*authManagerModel)
	if updated.loggingIn {
		t.Fatalf("expected login canceled")
	}

	// 2. Press 'd' to initiate device login for Codex
	newM, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated = newM.(*authManagerModel)
	if !updated.loggingIn || updated.loginMode != "device" {
		t.Fatalf("expected device login initiated for codex, got loggingIn=%v mode=%s", updated.loggingIn, updated.loginMode)
	}
}
