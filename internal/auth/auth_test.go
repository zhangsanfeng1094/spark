package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStore_SaveGetDeleteList(t *testing.T) {
	tempDir := t.TempDir()
	store := NewStore(filepath.Join(tempDir, "auth"))

	// List on empty store
	list, err := store.List()
	if err != nil {
		t.Fatalf("List empty store: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 auth records, got %d", len(list))
	}

	// Get non-existent
	record, err := store.Get(ProviderClaude)
	if err != nil {
		t.Fatalf("Get non-existent: %v", err)
	}
	if record != nil {
		t.Fatalf("expected nil record, got %+v", record)
	}

	// Save record
	authRec := &Auth{
		Provider:     ProviderClaude,
		Kind:         KindOAuth,
		AccessToken:  "claude-access-token",
		RefreshToken: "claude-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		ClientID:     "spark-claude-client",
		Scope:        "user:read",
		Account:      "user@example.com",
	}
	if err := store.Save(authRec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Get record
	got, err := store.Get(ProviderClaude)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatalf("expected auth record, got nil")
	}
	if got.AccessToken != "claude-access-token" || got.RefreshToken != "claude-refresh-token" {
		t.Fatalf("unexpected tokens: %+v", got)
	}
	if got.Account != "user@example.com" {
		t.Fatalf("unexpected account: %s", got.Account)
	}

	// Save another record
	codexRec := &Auth{
		Provider:    ProviderCodex,
		Kind:        KindDevice,
		AccessToken: "codex-token",
	}
	if err := store.Save(codexRec); err != nil {
		t.Fatalf("Save codex: %v", err)
	}

	list, err = store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 auth records, got %d", len(list))
	}

	// Delete
	if err := store.Delete(ProviderClaude); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err = store.Get(ProviderClaude)
	if err != nil || got != nil {
		t.Fatalf("expected nil after delete, got %v, err=%v", got, err)
	}
}

func TestAuth_Expired(t *testing.T) {
	var nilAuth *Auth
	if !nilAuth.Expired(0) {
		t.Fatal("nil auth should be expired")
	}

	emptyToken := &Auth{AccessToken: ""}
	if !emptyToken.Expired(0) {
		t.Fatal("empty token should be expired")
	}

	noExpiry := &Auth{AccessToken: "abc"}
	if noExpiry.Expired(time.Hour) {
		t.Fatal("auth without expires_at should not be expired")
	}

	pastExpiry := &Auth{
		AccessToken: "abc",
		ExpiresAt:   time.Now().Add(-10 * time.Minute),
	}
	if !pastExpiry.Expired(0) {
		t.Fatal("past expiry should be expired")
	}

	nearExpiry := &Auth{
		AccessToken: "abc",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}
	if !nearExpiry.Expired(10 * time.Minute) {
		t.Fatal("within skew should be reported as expired")
	}
	if nearExpiry.Expired(1 * time.Minute) {
		t.Fatal("outside skew should not be expired")
	}
}

func TestSpecFor(t *testing.T) {
	spec, err := SpecFor(ProviderClaude)
	if err != nil {
		t.Fatalf("SpecFor claude: %v", err)
	}
	if spec.Provider != ProviderClaude || spec.AuthEndpoint == "" {
		t.Fatalf("invalid spec: %+v", spec)
	}

	for _, alias := range []string{"commandcode", "command-code", "cmd", "commandcode-proxy"} {
		ccSpec, err := SpecFor(alias)
		if err != nil {
			t.Fatalf("SpecFor %q: %v", alias, err)
		}
		if ccSpec.Provider != ProviderCommandCode {
			t.Fatalf("expected ProviderCommandCode for %q, got %q", alias, ccSpec.Provider)
		}
		if ccSpec.DefaultCallbackPort != 5959 {
			t.Fatalf("expected port 5959, got %d", ccSpec.DefaultCallbackPort)
		}
		if ccSpec.AuthEndpoint != "https://commandcode.ai/studio/auth/cli" {
			t.Fatalf("unexpected auth endpoint: %s", ccSpec.AuthEndpoint)
		}
	}

	_, err = SpecFor("unknown-provider")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestProviderSpec_BuildAuthURL(t *testing.T) {
	ccSpec, err := SpecFor(ProviderCommandCode)
	if err != nil {
		t.Fatalf("SpecFor: %v", err)
	}
	u := ccSpec.BuildAuthURL("http://localhost:5959/callback", "state123", "")
	if u != "https://commandcode.ai/studio/auth/cli?callback=http%3A%2F%2Flocalhost%3A5959%2Fcallback&state=state123" {
		t.Fatalf("unexpected Command Code auth URL: %s", u)
	}

	claudeSpec, err := SpecFor(ProviderClaude)
	if err != nil {
		t.Fatalf("SpecFor: %v", err)
	}
	uClaude := claudeSpec.BuildAuthURL("http://localhost:54545/callback", "state456", "challenge789")
	if !strings.Contains(uClaude, "client_id=") || !strings.Contains(uClaude, "code_challenge=challenge789") || !strings.Contains(uClaude, "state=state456") {
		t.Fatalf("unexpected Claude auth URL: %s", uClaude)
	}

	codexSpec, err := SpecFor(ProviderCodex)
	if err != nil {
		t.Fatalf("SpecFor Codex: %v", err)
	}
	if !codexSpec.SupportsDeviceFlow {
		t.Fatalf("Codex should support device flow")
	}
	if codexSpec.DeviceAuthorizationEndpoint != "https://auth.openai.com/api/accounts/deviceauth/usercode" {
		t.Fatalf("unexpected Codex device endpoint: %s", codexSpec.DeviceAuthorizationEndpoint)
	}
	if codexSpec.DeviceTokenEndpoint != "https://auth.openai.com/api/accounts/deviceauth/token" {
		t.Fatalf("unexpected Codex token endpoint: %s", codexSpec.DeviceTokenEndpoint)
	}
	if codexSpec.DeviceVerificationEndpoint != "https://auth.openai.com/codex/device" {
		t.Fatalf("unexpected Codex verification endpoint: %s", codexSpec.DeviceVerificationEndpoint)
	}
	uCodex := codexSpec.BuildAuthURL("http://localhost:1455/auth/callback", "stateCodex", "challengeCodex")
	if !strings.Contains(uCodex, "prompt=login") ||
		!strings.Contains(uCodex, "id_token_add_organizations=true") ||
		!strings.Contains(uCodex, "codex_cli_simplified_flow=true") ||
		!strings.Contains(uCodex, "code_challenge=challengeCodex") {
		t.Fatalf("Codex auth URL missing required CPA parameters: %s", uCodex)
	}
}

func TestExtractCodexClaims(t *testing.T) {
	// Construct a mock JWT payload:
	// {"email": "test@example.com", "https://api.openai.com/auth": {"chatgpt_account_id": "acc-98765", "chatgpt_plan_type": "team"}}
	header := "eyJhbGciOiJub25lIn0" // {"alg":"none"}
	payload := "eyJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjLTk4NzY1IiwiY2hhdGdwdF9wbGFuX3R5cGUiOiJ0ZWFtIn19"
	jwt := header + "." + payload + ".sig"

	accID, plan, email, err := ExtractCodexClaims(jwt)
	if err != nil {
		t.Fatalf("ExtractCodexClaims failed: %v", err)
	}
	if accID != "acc-98765" {
		t.Fatalf("expected acc-98765, got %s", accID)
	}
	if plan != "team" {
		t.Fatalf("expected team, got %s", plan)
	}
	if email != "test@example.com" {
		t.Fatalf("expected test@example.com, got %s", email)
	}
}

func TestGrokAuthSpec(t *testing.T) {
	grokSpec, err := SpecFor(ProviderGrok)
	if err != nil {
		t.Fatalf("SpecFor Grok: %v", err)
	}
	if !grokSpec.SupportsDeviceFlow {
		t.Fatalf("Grok should support device flow")
	}
	if grokSpec.ClientID != "b1a00492-073a-47ea-816f-4c329264a828" {
		t.Fatalf("unexpected Grok client id: %s", grokSpec.ClientID)
	}
	if grokSpec.DeviceAuthorizationEndpoint != "https://auth.x.ai/oauth2/device/code" {
		t.Fatalf("unexpected Grok device endpoint: %s", grokSpec.DeviceAuthorizationEndpoint)
	}
	cat, ok := CatalogFor(ProviderGrok)
	if !ok || cat.DefaultEndpoint != "https://cli-chat-proxy.grok.com/v1" {
		t.Fatalf("unexpected Grok catalog: %+v", cat)
	}
}

func TestGeminiAuthSpec(t *testing.T) {
	t.Setenv("GEMINI_CLIENT_ID", "test-gemini-client-id")
	t.Setenv("GEMINI_CLIENT_SECRET", "test-gemini-client-secret")
	geminiSpec, err := SpecFor(ProviderGemini)
	if err != nil {
		t.Fatalf("SpecFor Gemini: %v", err)
	}
	if geminiSpec.ClientID != "test-gemini-client-id" {
		t.Fatalf("unexpected Gemini client id: %s", geminiSpec.ClientID)
	}
	if geminiSpec.ClientSecret != "test-gemini-client-secret" {
		t.Fatalf("unexpected Gemini client secret: %s", geminiSpec.ClientSecret)
	}
}

func TestStore_CommandCodeLegacy(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	store := NewStore(filepath.Join(tempDir, ".spark", "auth"))

	// 1. Initially nil
	got, err := store.Get(ProviderCommandCode)
	if err != nil || got != nil {
		t.Fatalf("expected nil initially, got %+v, err=%v", got, err)
	}

	// 2. Fallback to ~/.commandcode/auth.json
	cmdDir := filepath.Join(tempDir, ".commandcode")
	if err := os.MkdirAll(cmdDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	authData := `{"apiKey":"user_test_token_123","userName":"testuser","userId":"usr_456"}`
	if err := os.WriteFile(filepath.Join(cmdDir, "auth.json"), []byte(authData), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}

	got, err = store.Get(ProviderCommandCode)
	if err != nil {
		t.Fatalf("Get with legacy: %v", err)
	}
	if got == nil || got.AccessToken != "user_test_token_123" || got.Account != "testuser" {
		t.Fatalf("unexpected legacy auth: %+v", got)
	}

	// Verify List includes it
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var foundCC bool
	for _, a := range list {
		if a.Provider == ProviderCommandCode && a.AccessToken == "user_test_token_123" {
			foundCC = true
		}
	}
	if !foundCC {
		t.Fatalf("expected legacy commandcode in List(), got %+v", list)
	}
}

func TestParseAndFormatAuthRef(t *testing.T) {
	prov, acc := ParseAuthRef("claude:work")
	if prov != "claude" || acc != "work" {
		t.Fatalf("expected claude, work; got %s, %s", prov, acc)
	}

	prov, acc = ParseAuthRef("commandcode:default")
	if prov != "commandcode" || acc != "default" {
		t.Fatalf("expected commandcode, default; got %s, %s", prov, acc)
	}

	prov, acc = ParseAuthRef("commandcode")
	if prov != "commandcode" || acc != "" {
		t.Fatalf("expected commandcode, ''; got %s, %s", prov, acc)
	}

	if got := FormatAuthRef("claude", "work"); got != "claude:work" {
		t.Fatalf("expected claude:work, got %s", got)
	}
	if got := FormatAuthRef("claude", "default"); got != "claude" {
		t.Fatalf("expected claude, got %s", got)
	}
	if got := FormatAuthRef("claude", ""); got != "claude" {
		t.Fatalf("expected claude, got %s", got)
	}
}

func TestStore_MultiAccount(t *testing.T) {
	tempDir := t.TempDir()
	store := NewStore(filepath.Join(tempDir, "auth"))

	acc1 := &Auth{
		Provider:    ProviderClaude,
		Account:     "personal@example.com",
		AccessToken: "token-personal",
	}
	acc2 := &Auth{
		Provider:    ProviderClaude,
		Account:     "work@example.com",
		AccessToken: "token-work",
	}

	if err := store.Save(acc1); err != nil {
		t.Fatalf("Save personal: %v", err)
	}
	if err := store.Save(acc2); err != nil {
		t.Fatalf("Save work: %v", err)
	}

	// Get by ref
	gotPersonal, err := store.GetByRef("claude:personal@example.com")
	if err != nil || gotPersonal == nil {
		t.Fatalf("GetByRef personal failed: %v, %+v", err, gotPersonal)
	}
	if gotPersonal.AccessToken != "token-personal" {
		t.Fatalf("expected token-personal, got %s", gotPersonal.AccessToken)
	}

	gotWork, err := store.GetByRef("claude:work@example.com")
	if err != nil || gotWork == nil {
		t.Fatalf("GetByRef work failed: %v, %+v", err, gotWork)
	}
	if gotWork.AccessToken != "token-work" {
		t.Fatalf("expected token-work, got %s", gotWork.AccessToken)
	}

	// ListAccounts
	claudeAccs, err := store.ListAccounts(ProviderClaude)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(claudeAccs) != 2 {
		t.Fatalf("expected 2 claude accounts, got %d", len(claudeAccs))
	}

	// DeleteAccount
	if err := store.DeleteAccount(ProviderClaude, "personal@example.com"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	gotDeleted, err := store.GetByRef("claude:personal@example.com")
	if err != nil || gotDeleted != nil {
		t.Fatalf("expected nil after delete, got %+v", gotDeleted)
	}
}

func TestStore_RejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	authDir := filepath.Join(root, "auth")
	unrelated := filepath.Join(root, "unrelated")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(unrelated, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(authDir)
	if err := store.Save(&Auth{Provider: ProviderClaude, AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"../unrelated", "..", "foo/bar", `foo\bar`, "claude/../unrelated"} {
		if err := store.Delete(id); !errors.Is(err, ErrInvalidStoreID) {
			t.Fatalf("Delete(%q) err = %v, want ErrInvalidStoreID", id, err)
		}
		if _, err := store.Get(id); !errors.Is(err, ErrInvalidStoreID) {
			t.Fatalf("Get(%q) err = %v, want ErrInvalidStoreID", id, err)
		}
		if err := store.Save(&Auth{Provider: id, AccessToken: "x"}); !errors.Is(err, ErrInvalidStoreID) {
			t.Fatalf("Save(%q) err = %v, want ErrInvalidStoreID", id, err)
		}
		if err := store.DeleteAccount(id, "default"); !errors.Is(err, ErrInvalidStoreID) {
			t.Fatalf("DeleteAccount(%q) err = %v, want ErrInvalidStoreID", id, err)
		}
	}

	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file was modified or deleted: %v", err)
	}
	got, err := store.Get(ProviderClaude)
	if err != nil || got == nil || got.AccessToken != "tok" {
		t.Fatalf("valid record should remain: got=%+v err=%v", got, err)
	}

	link := filepath.Join(authDir, "escape")
	if err := os.Symlink(unrelated, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := store.Delete("escape"); !errors.Is(err, ErrInvalidStoreID) {
		t.Fatalf("Delete symlink escape err = %v, want ErrInvalidStoreID", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("symlink delete escaped store root: %v", err)
	}

	if err := store.Delete(ProviderClaude); err != nil {
		t.Fatalf("Delete valid provider: %v", err)
	}
	got, err = store.Get(ProviderClaude)
	if err != nil || got != nil {
		t.Fatalf("expected nil after valid delete, got %+v err=%v", got, err)
	}
}

func TestStore_RejectsDanglingSymlinkWrites(t *testing.T) {
	root := t.TempDir()
	authDir := filepath.Join(root, "auth")
	if err := os.MkdirAll(filepath.Join(authDir, ProviderClaude), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.json")
	accountLink := filepath.Join(authDir, ProviderClaude, "default.json")
	if err := os.Symlink(outside, accountLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	flatOutside := filepath.Join(root, "flat-outside.json")
	flatLink := filepath.Join(authDir, ProviderClaude+".json")
	if err := os.Symlink(flatOutside, flatLink); err != nil {
		t.Fatal(err)
	}

	store := NewStore(authDir)
	if err := store.Save(&Auth{Provider: ProviderClaude, AccessToken: "secret"}); !errors.Is(err, ErrInvalidStoreID) {
		t.Fatalf("Save dangling symlink err = %v, want ErrInvalidStoreID", err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("account symlink write escaped store: %v", err)
	}
	if _, err := os.Stat(flatOutside); !os.IsNotExist(err) {
		t.Fatalf("flat symlink write escaped store: %v", err)
	}

	if _, err := store.Get(ProviderClaude); !errors.Is(err, ErrInvalidStoreID) && err != nil {
		t.Fatalf("Get dangling symlink err = %v", err)
	}
	listed, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("List followed symlink: %+v", listed)
	}
}

func TestCatalogFor(t *testing.T) {
	cat, ok := CatalogFor("claude")
	if !ok {
		t.Fatal("CatalogFor claude not found")
	}
	if cat.DefaultProtocol != "anthropic_messages" || cat.DefaultModel == "" {
		t.Fatalf("unexpected catalog for claude: %+v", cat)
	}

	ccCat, ok := CatalogFor("commandcode")
	if !ok {
		t.Fatal("CatalogFor commandcode not found")
	}
	if ccCat.DefaultProtocol != "openai_chat_completions" || ccCat.DefaultEndpoint == "" {
		t.Fatalf("unexpected catalog for commandcode: %+v", ccCat)
	}
}
