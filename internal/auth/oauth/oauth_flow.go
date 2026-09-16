package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"spark/internal/auth"
)

// BrowserOptions configures the browser OAuth flow.
type BrowserOptions struct {
	// NoBrowser prints the authorization URL instead of opening a browser.
	NoBrowser bool
	// CallbackPort overrides the provider's default loopback callback port.
	CallbackPort int
	// OpenURL opens a browser; defaults to opening the system browser.
	OpenURL func(rawURL string) error
	// OnAuthURL is called as soon as the authorization URL is constructed so callers can display it.
	OnAuthURL func(rawURL string)
	// ManualCallbackURL provides a channel through which a user can supply the redirected callback URL or code.
	ManualCallbackURL <-chan string
}

// BrowserFlow runs the Authorization Code + PKCE flow for the provider.
//
// It starts a loopback HTTP server, builds an authorization URL with a
// code_verifier (PKCE) and state, opens (or prints) the URL, and waits for the
// callback to exchange the code for an auth.Auth record.
func BrowserFlow(ctx context.Context, spec auth.ProviderSpec, opts BrowserOptions) (*auth.Auth, error) {
	if spec.AuthEndpoint == "" {
		return nil, fmt.Errorf("auth: provider %s has no browser auth endpoint configured", spec.Provider)
	}
	if spec.TokenEndpoint == "" && spec.Provider != auth.ProviderCommandCode {
		return nil, fmt.Errorf("auth: provider %s has no browser oauth endpoints configured", spec.Provider)
	}
	port := opts.CallbackPort
	if port <= 0 {
		port = spec.DefaultCallbackPort
	}
	if port <= 0 {
		port = 54545
	}

	state := auth.GenerateRandomState()
	verifier := auth.GenerateCodeVerifier()
	challenge := auth.PKCEChallenge(verifier)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("auth: cannot start local callback server on port %d: %w", port, err)
	}
	defer ln.Close()

	callbackPath := spec.EffectiveCallbackPath()
	resultCh := make(chan resultOrErr, 1)

	redirectURI := fmt.Sprintf("http://localhost:%d%s", port, callbackPath)

	mux := http.NewServeMux()
	handleCallback := func(w http.ResponseWriter, r *http.Request) {
		// Handle CORS preflight
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Handle POST callback (e.g. Command Code Studio which sends JSON:
		// { "apiKey": "user_...", "state": "...", "userId": "...", "userName": "...", "keyName": "..." })
		if r.Method == http.MethodPost {
			var body struct {
				APIKey           string `json:"apiKey"`
				State            string `json:"state"`
				UserID           string `json:"userId"`
				UserName         string `json:"userName"`
				KeyName          string `json:"keyName"`
				Error            string `json:"error"`
				ErrorDescription string `json:"error_description"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Invalid JSON"})
				resultCh <- resultOrErr{err: fmt.Errorf("auth: invalid callback JSON: %w", err)}
				return
			}
			if body.Error != "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": body.Error})
				resultCh <- resultOrErr{err: fmt.Errorf("auth: authorization error: %s", body.Error)}
				return
			}
			if subtle.ConstantTimeCompare([]byte(body.State), []byte(state)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Invalid state token"})
				resultCh <- resultOrErr{err: fmt.Errorf("auth: oauth state mismatch")}
				return
			}
			if body.APIKey == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Missing apiKey"})
				resultCh <- resultOrErr{err: fmt.Errorf("auth: callback missing apiKey")}
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})

			account := body.UserName
			if account == "" {
				account = body.UserID
			}
			if body.KeyName != "" && account != "" && account != body.KeyName {
				account = fmt.Sprintf("%s (%s)", account, body.KeyName)
			}

			resultCh <- resultOrErr{
				auth: &auth.Auth{
					Provider:    spec.Provider,
					Kind:        auth.KindOAuth,
					AccessToken: body.APIKey,
					Account:     account,
					CreatedAt:   time.Now(),
				},
			}
			return
		}

		q := r.URL.Query()
		gotState := q.Get("state")
		if subtle.ConstantTimeCompare([]byte(gotState), []byte(state)) != 1 {
			writeCallbackResponse(w, http.StatusBadRequest, "state mismatch")
			resultCh <- resultOrErr{err: fmt.Errorf("auth: oauth state mismatch")}
			return
		}

		// Support direct apiKey in query params (fallback)
		if apiKey := q.Get("apiKey"); apiKey != "" {
			writeCallbackResponse(w, http.StatusOK, "Authorization complete. You can close this tab and return to Spark.")
			account := q.Get("userName")
			if account == "" {
				account = q.Get("userId")
			}
			resultCh <- resultOrErr{
				auth: &auth.Auth{
					Provider:    spec.Provider,
					Kind:        auth.KindOAuth,
					AccessToken: apiKey,
					Account:     account,
					CreatedAt:   time.Now(),
				},
			}
			return
		}

		code := q.Get("code")
		if code == "" {
			if errDesc := q.Get("error"); errDesc != "" {
				writeCallbackResponse(w, http.StatusBadRequest, errDesc)
				resultCh <- resultOrErr{err: fmt.Errorf("auth: authorization error: %s", errDesc)}
				return
			}
			writeCallbackResponse(w, http.StatusBadRequest, "missing code")
			resultCh <- resultOrErr{err: fmt.Errorf("auth: callback missing code")}
			return
		}

		// Exchange the code in the background so we can answer the browser.
		go func() {
			form := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {code},
				"redirect_uri":  {redirectURI},
				"client_id":     {clientIDFor(spec)},
				"code_verifier": {verifier},
			}
			if spec.ClientSecret != "" {
				form.Set("client_secret", spec.ClientSecret)
			}
			tr, err := postForm(context.Background(), spec.TokenEndpoint, form)
			if err != nil {
				resultCh <- resultOrErr{err: err}
				return
			}
			resultCh <- resultOrErr{auth: buildAuth(spec, tr, auth.KindOAuth)}
		}()
		writeCallbackResponse(w, http.StatusOK, "Authorization complete. You can close this tab and return to Spark.")
	}

	mux.HandleFunc(callbackPath, handleCallback)
	if callbackPath != "/callback" {
		mux.HandleFunc("/callback", handleCallback)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	srv := &http.Server{Handler: mux}
	done := make(chan struct{})
	defer func() { close(done) }()
	go func() {
		<-done
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() { _ = srv.Serve(ln) }()

	authURL := spec.BuildAuthURL(redirectURI, state, challenge)

	if opts.OnAuthURL != nil {
		opts.OnAuthURL(authURL)
	}

	open := opts.OpenURL
	if open == nil && !opts.NoBrowser {
		open = openBrowser
	}
	if open != nil {
		_ = open(authURL)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case manualURL := <-opts.ManualCallbackURL:
		if strings.TrimSpace(manualURL) == "" {
			return nil, fmt.Errorf("auth: empty callback URL provided")
		}
		raw := strings.Trim(strings.TrimSpace(manualURL), "\"'` \t\r\n")

		// Direct API key (e.g. user_... or Command Code provider)
		if spec.Provider == auth.ProviderCommandCode || strings.HasPrefix(raw, "user_") {
			if !strings.HasPrefix(raw, "{") && !strings.Contains(raw, "?") {
				return &auth.Auth{
					Provider:    spec.Provider,
					Kind:        auth.KindOAuth,
					AccessToken: raw,
					CreatedAt:   time.Now(),
				}, nil
			}
		}

		// JSON string with apiKey
		if strings.HasPrefix(raw, "{") && strings.Contains(raw, "apiKey") {
			var body struct {
				APIKey   string `json:"apiKey"`
				UserName string `json:"userName"`
				UserID   string `json:"userId"`
			}
			if err := json.Unmarshal([]byte(raw), &body); err == nil && body.APIKey != "" {
				acc := body.UserName
				if acc == "" {
					acc = body.UserID
				}
				return &auth.Auth{
					Provider:    spec.Provider,
					Kind:        auth.KindOAuth,
					AccessToken: body.APIKey,
					Account:     acc,
					CreatedAt:   time.Now(),
				}, nil
			}
		}

		var gotCode string
		if strings.Contains(raw, "?") || strings.Contains(raw, "=") {
			var query url.Values
			if parsed, err := url.Parse(raw); err == nil && parsed.RawQuery != "" {
				query = parsed.Query()
			} else {
				query, _ = url.ParseQuery(raw)
			}
			if apiKey := query.Get("apiKey"); apiKey != "" {
				if gotState := query.Get("state"); gotState != "" && subtle.ConstantTimeCompare([]byte(gotState), []byte(state)) != 1 {
					return nil, fmt.Errorf("auth: oauth state mismatch in manual callback")
				}
				acc := query.Get("userName")
				if acc == "" {
					acc = query.Get("userId")
				}
				return &auth.Auth{
					Provider:    spec.Provider,
					Kind:        auth.KindOAuth,
					AccessToken: apiKey,
					Account:     acc,
					CreatedAt:   time.Now(),
				}, nil
			}
			gotCode = query.Get("code")
			if gotState := query.Get("state"); gotState != "" && subtle.ConstantTimeCompare([]byte(gotState), []byte(state)) != 1 {
				return nil, fmt.Errorf("auth: oauth state mismatch in manual callback")
			}
		} else {
			gotCode = raw
		}
		if gotCode == "" {
			return nil, fmt.Errorf("auth: could not extract code from callback URL %q", raw)
		}
		if spec.TokenEndpoint == "" {
			return &auth.Auth{
				Provider:    spec.Provider,
				Kind:        auth.KindOAuth,
				AccessToken: gotCode,
				CreatedAt:   time.Now(),
			}, nil
		}
		form := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {gotCode},
			"redirect_uri":  {redirectURI},
			"client_id":     {clientIDFor(spec)},
			"code_verifier": {verifier},
		}
		if spec.ClientSecret != "" {
			form.Set("client_secret", spec.ClientSecret)
		}
		tr, err := postForm(context.Background(), spec.TokenEndpoint, form)
		if err != nil {
			return nil, fmt.Errorf("auth: token exchange failed: %w", err)
		}
		return buildAuth(spec, tr, auth.KindOAuth), nil
	case res := <-resultCh:
		return res.auth, res.err
	}
}

// manualURLError reports that no browser was opened and the user must visit
// the URL manually in a browser.
type manualURLError struct{ URL string }

func (e *manualURLError) Error() string { return e.URL }

func clientIDFor(spec auth.ProviderSpec) string {
	if spec.ClientID != "" {
		return spec.ClientID
	}
	return spec.Provider
}

type resultOrErr struct {
	auth *auth.Auth
	err  error
}

func writeCallbackResponse(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, body)
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ManualURLError extracts the URL from a manualURLError, if any.
func ManualURL(err error) string {
	if m, ok := err.(*manualURLError); ok {
		return m.URL
	}
	return ""
}

func openBrowser(rawURL string) error {
	return openURL(rawURL)
}
