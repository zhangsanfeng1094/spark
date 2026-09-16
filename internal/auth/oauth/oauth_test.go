package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spark/internal/auth"
)

func TestDeviceFlow(t *testing.T) {
	var pollCount int
	var tokenRequests []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device/code":
			_ = json.NewEncoder(w).Encode(deviceAuthorizationResponse{
				DeviceCode:      "dev-12345",
				UserCode:        "ABCD-EFGH",
				VerificationURI: "https://example.com/verify",
				ExpiresIn:       60,
				Interval:        1,
			})
		case "/token":
			pollCount++
			if pollCount == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(tokenResponse{
					Error:     "authorization_pending",
					ErrorDesc: "The user has not yet approved",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken:  "access-token-999",
				RefreshToken: "refresh-token-999",
				ExpiresIn:    3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	spec := auth.ProviderSpec{
		Provider:                    "test-provider",
		SupportsDeviceFlow:          true,
		DeviceAuthorizationEndpoint: ts.URL + "/device/code",
		DeviceTokenEndpoint:         ts.URL + "/token",
	}

	var notified bool
	result, err := DeviceFlow(context.Background(), spec, func(res DeviceResult) error {
		notified = true
		if res.UserCode != "ABCD-EFGH" || res.VerificationURI != "https://example.com/verify" {
			t.Errorf("unexpected progress: %+v", res)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("DeviceFlow failed: %v", err)
	}
	if !notified {
		t.Errorf("onProgress callback was not invoked")
	}
	if result == nil || result.AccessToken != "access-token-999" {
		t.Fatalf("unexpected auth result: %+v", result)
	}
	_ = tokenRequests
}

func TestBrowserFlow(t *testing.T) {
	// Free port for test callback
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken:  "browser-access-token",
				RefreshToken: "browser-refresh-token",
				ExpiresIn:    1800,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	spec := auth.ProviderSpec{
		Provider:            "browser-provider",
		AuthEndpoint:        "https://example.com/oauth/auth",
		TokenEndpoint:       ts.URL + "/token",
		DefaultCallbackPort: port,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var authURLCaptured string
	openURLFunc := func(rawURL string) error {
		authURLCaptured = rawURL
		// Simulate browser visiting callback URL with code and state
		go func() {
			time.Sleep(50 * time.Millisecond)
			u, parseErr := url.Parse(rawURL)
			if parseErr != nil {
				return
			}
			state := u.Query().Get("state")
			callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?code=mock_code&state=%s", port, state)
			resp, reqErr := http.Get(callbackURL)
			if reqErr == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	authRec, err := BrowserFlow(ctx, spec, BrowserOptions{
		CallbackPort: port,
		OpenURL:      openURLFunc,
	})
	if err != nil {
		t.Fatalf("BrowserFlow: %v", err)
	}
	if authRec == nil || authRec.AccessToken != "browser-access-token" {
		t.Fatalf("unexpected auth record: %+v", authRec)
	}
	if !strings.Contains(authURLCaptured, "code_challenge=") {
		t.Errorf("expected code_challenge in auth URL: %s", authURLCaptured)
	}
}

func TestRefresh_And_EnsureFresh(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken:  "new-access-token",
				RefreshToken: "new-refresh-token",
				ExpiresIn:    3600,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	store := auth.NewStore(filepath.Join(tempDir, "auth"))

	spec := auth.ProviderSpec{
		Provider:      auth.ProviderClaude,
		TokenEndpoint: ts.URL + "/token",
	}

	// 1. EnsureFresh with no record in store -> returns nil
	got, err := EnsureFresh(context.Background(), store, spec, time.Minute)
	if err != nil {
		t.Fatalf("EnsureFresh nil store record: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty store, got %+v", got)
	}

	// 2. Fresh record -> no refresh needed
	freshAuth := &auth.Auth{
		Provider:     auth.ProviderClaude,
		AccessToken:  "valid-token",
		RefreshToken: "refresh-123",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	if err := store.Save(freshAuth); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err = EnsureFresh(context.Background(), store, spec, 10*time.Minute)
	if err != nil {
		t.Fatalf("EnsureFresh on valid token: %v", err)
	}
	if got.AccessToken != "valid-token" {
		t.Fatalf("expected valid-token, got %s", got.AccessToken)
	}

	// 3. Expired record -> refreshed and persisted
	expiredAuth := &auth.Auth{
		Provider:     auth.ProviderClaude,
		AccessToken:  "expired-token",
		RefreshToken: "refresh-123",
		ExpiresAt:    time.Now().Add(-5 * time.Minute),
		Account:      "user@example.com",
	}
	if err := store.Save(expiredAuth); err != nil {
		t.Fatalf("Save expired: %v", err)
	}

	got, err = EnsureFresh(context.Background(), store, spec, 10*time.Minute)
	if err != nil {
		t.Fatalf("EnsureFresh on expired: %v", err)
	}
	if got == nil || got.AccessToken != "new-access-token" {
		t.Fatalf("expected refreshed token, got %+v", got)
	}
	if got.Account != "user@example.com" {
		t.Fatalf("account preserved: %s", got.Account)
	}

	// Verify it was persisted in store
	persisted, _ := store.Get(auth.ProviderClaude)
	if persisted == nil || persisted.AccessToken != "new-access-token" {
		t.Fatalf("persisted token not updated: %+v", persisted)
	}
}

func TestBrowserFlow_CommandCode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	spec, err := auth.SpecFor(auth.ProviderCommandCode)
	if err != nil {
		t.Fatalf("SpecFor commandcode: %v", err)
	}
	spec.DefaultCallbackPort = port

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var authURLCaptured string
	openURLFunc := func(rawURL string) error {
		authURLCaptured = rawURL
		go func() {
			time.Sleep(50 * time.Millisecond)
			u, parseErr := url.Parse(rawURL)
			if parseErr != nil {
				return
			}
			state := u.Query().Get("state")
			callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

			// 1. Preflight OPTIONS request
			reqOpt, _ := http.NewRequest(http.MethodOptions, callbackURL, nil)
			reqOpt.Header.Set("Origin", "https://commandcode.ai")
			reqOpt.Header.Set("Access-Control-Request-Method", "POST")
			respOpt, errOpt := http.DefaultClient.Do(reqOpt)
			if errOpt == nil {
				if respOpt.StatusCode != http.StatusNoContent && respOpt.StatusCode != http.StatusOK {
					t.Errorf("expected 204 or 200 on OPTIONS, got %d", respOpt.StatusCode)
				}
				_ = respOpt.Body.Close()
			}

			// 2. POST callback with JSON payload
			bodyJSON := fmt.Sprintf(`{"apiKey":"user_cc_secret_key_123","state":"%s","userName":"coder_bob","userId":"usr_777","keyName":"default"}`, state)
			reqPost, _ := http.NewRequest(http.MethodPost, callbackURL, strings.NewReader(bodyJSON))
			reqPost.Header.Set("Content-Type", "application/json")
			reqPost.Header.Set("Origin", "https://commandcode.ai")
			respPost, errPost := http.DefaultClient.Do(reqPost)
			if errPost != nil {
				t.Errorf("POST callback error: %v", errPost)
				return
			}
			defer respPost.Body.Close()
			if respPost.StatusCode != http.StatusOK {
				t.Errorf("expected 200 on POST callback, got %d", respPost.StatusCode)
			}
			var postResp map[string]any
			_ = json.NewDecoder(respPost.Body).Decode(&postResp)
			if postResp["success"] != true {
				t.Errorf("expected success:true in response, got %+v", postResp)
			}
		}()
		return nil
	}

	authRec, err := BrowserFlow(ctx, spec, BrowserOptions{
		CallbackPort: port,
		OpenURL:      openURLFunc,
	})
	if err != nil {
		t.Fatalf("BrowserFlow commandcode: %v", err)
	}
	if authRec == nil {
		t.Fatal("expected non-nil auth record")
	}
	if authRec.Provider != auth.ProviderCommandCode {
		t.Errorf("expected provider %q, got %q", auth.ProviderCommandCode, authRec.Provider)
	}
	if authRec.AccessToken != "user_cc_secret_key_123" {
		t.Errorf("unexpected access token: %s", authRec.AccessToken)
	}
	if !strings.Contains(authRec.Account, "coder_bob") {
		t.Errorf("expected coder_bob in account, got %s", authRec.Account)
	}
	if !strings.Contains(authURLCaptured, "https://commandcode.ai/studio/auth/cli?callback=") {
		t.Errorf("unexpected auth URL: %s", authURLCaptured)
	}
}

func TestBrowserFlow_ManualApiKey(t *testing.T) {
	spec, err := auth.SpecFor(auth.ProviderCommandCode)
	if err != nil {
		t.Fatalf("SpecFor commandcode: %v", err)
	}

	manualCh := make(chan string, 1)
	manualCh <- "user_direct_key_abc123"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	authRec, err := BrowserFlow(ctx, spec, BrowserOptions{
		NoBrowser:         true,
		ManualCallbackURL: manualCh,
	})
	if err != nil {
		t.Fatalf("BrowserFlow manual: %v", err)
	}
	if authRec == nil || authRec.AccessToken != "user_direct_key_abc123" {
		t.Fatalf("unexpected manual auth record: %+v", authRec)
	}
}

func TestCodexDeviceFlow(t *testing.T) {
	var pollCount int
	mockIDToken := "eyJhbGciOiJub25lIn0.eyJlbWFpbCI6ImNvZGV4QHhhaS5jb20iLCJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjLWNvZGV4LTk5OSIsImNoYXRncHRfcGxhbl90eXBlIjoicHJvIn19.sig"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["client_id"] != "app_EMoamEEZ73f0CkXaXp7hrann" {
				t.Errorf("unexpected client_id in usercode req: %s", req["client_id"])
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_auth_id": "devauth_test_123",
				"user_code":      "S99U-TEST",
				"interval":       "1",
			})
		case "/api/accounts/deviceauth/token":
			pollCount++
			if pollCount == 1 {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_code": "code_auth_xyz",
				"code_verifier":      "verifier_xyz_123",
				"code_challenge":     "challenge_xyz_123",
			})
		case "/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "authorization_code" ||
				r.Form.Get("code") != "code_auth_xyz" ||
				r.Form.Get("redirect_uri") != "https://auth.openai.com/deviceauth/callback" ||
				r.Form.Get("code_verifier") != "verifier_xyz_123" {
				t.Errorf("unexpected token form: %+v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "codex-access-token-999",
				"refresh_token": "codex-refresh-token-999",
				"id_token":      mockIDToken,
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	spec := auth.ProviderSpec{
		Provider:                    auth.ProviderCodex,
		ClientID:                    "app_EMoamEEZ73f0CkXaXp7hrann",
		SupportsDeviceFlow:          true,
		DeviceAuthorizationEndpoint: ts.URL + "/api/accounts/deviceauth/usercode",
		DeviceTokenEndpoint:         ts.URL + "/api/accounts/deviceauth/token",
		DeviceVerificationEndpoint:  "https://auth.openai.com/codex/device",
		TokenEndpoint:               ts.URL + "/oauth/token",
	}

	var notified bool
	result, err := DeviceFlow(context.Background(), spec, func(res DeviceResult) error {
		notified = true
		if res.UserCode != "S99U-TEST" || res.VerificationURI != "https://auth.openai.com/codex/device" {
			t.Errorf("unexpected progress: %+v", res)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("DeviceFlow failed: %v", err)
	}
	if !notified {
		t.Errorf("expected onProgress callback to be called")
	}
	if result == nil || result.AccessToken != "codex-access-token-999" {
		t.Fatalf("unexpected token result: %+v", result)
	}
	if result.AccountID != "acc-codex-999" {
		t.Errorf("expected AccountID acc-codex-999, got %s", result.AccountID)
	}
	if result.Metadata["plan_type"] != "pro" {
		t.Errorf("expected plan_type pro, got %s", result.Metadata["plan_type"])
	}
}

func TestRefresh_PreservesOldRefreshTokenWhenOmitted(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.Form.Get("refresh_token") != "keep-me" {
			t.Errorf("expected old refresh token in request, got %q", r.Form.Get("refresh_token"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	spec := auth.ProviderSpec{Provider: "claude", TokenEndpoint: ts.URL, ClientID: "client"}
	old := &auth.Auth{
		Provider:     "claude",
		Kind:         auth.KindOAuth,
		AccessToken:  "old-access",
		RefreshToken: "keep-me",
		Account:      "user@example.com",
		IDToken:      "id-token",
		Scope:        "user:read",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}
	got, err := Refresh(context.Background(), spec, old)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got.AccessToken != "new-access" {
		t.Fatalf("access token = %q", got.AccessToken)
	}
	if got.RefreshToken != "keep-me" {
		t.Fatalf("refresh token = %q, want keep-me", got.RefreshToken)
	}
	if got.Account != "user@example.com" || got.IDToken != "id-token" || got.Scope != "user:read" {
		t.Fatalf("preserved fields lost: %+v", got)
	}
}

func TestRefresh_ReplacesRefreshTokenWhenReturned(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "rotated",
			"expires_in":    3600,
		})
	}))
	defer ts.Close()

	spec := auth.ProviderSpec{Provider: "claude", TokenEndpoint: ts.URL, ClientID: "client"}
	old := &auth.Auth{
		Provider:     "claude",
		Kind:         auth.KindOAuth,
		AccessToken:  "old-access",
		RefreshToken: "keep-me",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}
	got, err := Refresh(context.Background(), spec, old)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got.RefreshToken != "rotated" {
		t.Fatalf("refresh token = %q, want rotated", got.RefreshToken)
	}
}

func TestEnsureFresh_PersistsPreservedRefreshToken(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		switch calls {
		case 1:
			if r.Form.Get("refresh_token") != "keep-me" {
				t.Errorf("first refresh used %q", r.Form.Get("refresh_token"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-1",
				"expires_in":   1,
			})
		case 2:
			if r.Form.Get("refresh_token") != "keep-me" {
				t.Errorf("second refresh used %q, old token was lost", r.Form.Get("refresh_token"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-2",
				"refresh_token": "rotated-later",
				"expires_in":    3600,
			})
		default:
			http.Error(w, "too many calls", http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	store := auth.NewStore(filepath.Join(t.TempDir(), "auth"))
	spec := auth.ProviderSpec{Provider: auth.ProviderClaude, TokenEndpoint: ts.URL, ClientID: "client"}
	if err := store.Save(&auth.Auth{
		Provider:     auth.ProviderClaude,
		Kind:         auth.KindOAuth,
		AccessToken:  "expired",
		RefreshToken: "keep-me",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	first, err := EnsureFresh(context.Background(), store, spec, time.Minute)
	if err != nil {
		t.Fatalf("EnsureFresh 1: %v", err)
	}
	if first.RefreshToken != "keep-me" || first.AccessToken != "access-1" {
		t.Fatalf("first persist = %+v", first)
	}
	saved, err := store.Get(auth.ProviderClaude)
	if err != nil || saved == nil || saved.RefreshToken != "keep-me" {
		t.Fatalf("persisted refresh token lost: %+v err=%v", saved, err)
	}

	saved.ExpiresAt = time.Now().Add(-time.Minute)
	if err := store.Save(saved); err != nil {
		t.Fatal(err)
	}
	second, err := EnsureFresh(context.Background(), store, spec, time.Minute)
	if err != nil {
		t.Fatalf("EnsureFresh 2: %v", err)
	}
	if second.RefreshToken != "rotated-later" || second.AccessToken != "access-2" {
		t.Fatalf("second persist = %+v", second)
	}
	if calls != 2 {
		t.Fatalf("token endpoint calls = %d, want 2", calls)
	}
}
