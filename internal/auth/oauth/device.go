package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"spark/internal/auth"
)

// DeviceResult carries what a Device Flow returns to the caller: the auth
// record once polling succeeds, plus the intermediate values a TUI/CLI may
// want to surface (verification URI, user code, and the polling interval).
type DeviceResult struct {
	Auth *auth.Auth

	VerificationURI string
	UserCode        string
	Interval        time.Duration
}

// DeviceFunc reports progress to the caller (e.g. to print the URL/code).
type DeviceFunc func(result DeviceResult) error

// DeviceFlow runs the Device Authorization Grant (RFC 8628) for the provider.
//
// The flow POSTs to the device-authorization endpoint to obtain a device_code
// and user_code, surfaces the verification URI + user code to the user, then
// polls the token endpoint until the user approves or the device_code expires.
func DeviceFlow(ctx context.Context, spec auth.ProviderSpec, onProgress DeviceFunc) (*auth.Auth, error) {
	if !spec.SupportsDeviceFlow {
		return nil, fmt.Errorf("auth: provider %s does not support device flow", spec.Provider)
	}
	if spec.Provider == auth.ProviderCodex {
		return codexDeviceFlow(ctx, spec, onProgress)
	}
	if spec.DeviceAuthorizationEndpoint == "" || spec.DeviceTokenEndpoint == "" {
		return nil, fmt.Errorf("auth: provider %s has no device endpoints configured", spec.Provider)
	}

	// 1. Request a device + user code.
	form := url.Values{}
	if spec.ClientID != "" {
		form.Set("client_id", spec.ClientID)
	} else {
		// Fall back to the provider key as client id when no public client exists.
		form.Set("client_id", spec.Provider)
	}
	if spec.Scope != "" {
		form.Set("scope", spec.Scope)
	}
	devBody, err := postDevice(ctx, spec.DeviceAuthorizationEndpoint, form)
	if err != nil {
		return nil, err
	}

	interval := time.Duration(devBody.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}

	verificationURI := devBody.VerificationURIComplete
	if verificationURI == "" {
		verificationURI = devBody.VerificationURI
	}
	if verificationURI == "" && spec.DeviceVerificationEndpoint != "" {
		verificationURI = spec.DeviceVerificationEndpoint
	}

	if onProgress != nil {
		if err := onProgress(DeviceResult{
			VerificationURI: verificationURI,
			UserCode:        devBody.UserCode,
			Interval:        interval,
		}); err != nil {
			return nil, err
		}
	}

	// 2. Poll the token endpoint.
	expiresIn := devBody.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 600
	}
	pollDeadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	tokenForm := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {devBody.DeviceCode},
		"client_id":   {form.Get("client_id")},
	}
	if spec.Scope != "" {
		tokenForm.Set("scope", spec.Scope)
	}

	for time.Now().Before(pollDeadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		tr, err := postForm(ctx, spec.DeviceTokenEndpoint, tokenForm)
		if err == nil {
			return buildAuth(spec, tr, auth.KindDevice), nil
		}
		// Retry on authorization-pending / slow-down; fail on others.
		if !isRetryableDeviceError(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("auth: device flow timed out for %s (verification URI: %s)", spec.Provider, devBody.VerificationURI)
}

type codexUserCodeResponse struct {
	DeviceAuthID string          `json:"device_auth_id"`
	UserCode     string          `json:"user_code"`
	UserCodeAlt  string          `json:"usercode"`
	Interval     json.RawMessage `json:"interval"`
}

type codexTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
	CodeChallenge     string `json:"code_challenge"`
}

func codexDeviceFlow(ctx context.Context, spec auth.ProviderSpec, onProgress DeviceFunc) (*auth.Auth, error) {
	// 1. Request user code
	reqBody, _ := json.Marshal(map[string]string{
		"client_id": spec.ClientID,
	})
	userCodeURL := spec.DeviceAuthorizationEndpoint
	if userCodeURL == "" {
		userCodeURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, userCodeURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("auth: create codex device usercode request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: request codex device code: %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("auth: read codex device code response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("auth: codex device code request failed (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var userCodeResp codexUserCodeResponse
	if err := json.Unmarshal(respBytes, &userCodeResp); err != nil {
		return nil, fmt.Errorf("auth: decode codex device code response: %w", err)
	}

	userCode := strings.TrimSpace(userCodeResp.UserCode)
	if userCode == "" {
		userCode = strings.TrimSpace(userCodeResp.UserCodeAlt)
	}
	deviceAuthID := strings.TrimSpace(userCodeResp.DeviceAuthID)
	if userCode == "" || deviceAuthID == "" {
		return nil, fmt.Errorf("auth: codex device flow returned missing code or auth id")
	}

	interval := parseDevicePollInterval(userCodeResp.Interval, 5*time.Second)

	verificationURI := spec.DeviceVerificationEndpoint
	if verificationURI == "" {
		verificationURI = "https://auth.openai.com/codex/device"
	}

	if onProgress != nil {
		if err := onProgress(DeviceResult{
			VerificationURI: verificationURI,
			UserCode:        userCode,
			Interval:        interval,
		}); err != nil {
			return nil, err
		}
	}

	// 2. Poll token
	tokenURL := spec.DeviceTokenEndpoint
	if tokenURL == "" {
		tokenURL = "https://auth.openai.com/api/accounts/deviceauth/token"
	}

	deadline := time.Now().Add(15 * time.Minute)
	pollBody, _ := json.Marshal(map[string]string{
		"device_auth_id": deviceAuthID,
		"user_code":     userCode,
	})

	var tokenResp codexTokenResponse
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("auth: codex device authentication timed out after 15 minutes")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		pollReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(pollBody))
		if err != nil {
			return nil, fmt.Errorf("auth: create codex device poll request: %w", err)
		}
		pollReq.Header.Set("Content-Type", "application/json")
		pollReq.Header.Set("Accept", "application/json")

		pResp, err := HTTPClient.Do(pollReq)
		if err != nil {
			return nil, fmt.Errorf("auth: poll codex device token: %w", err)
		}
		pBytes, rErr := io.ReadAll(pResp.Body)
		_ = pResp.Body.Close()
		if rErr != nil {
			return nil, fmt.Errorf("auth: read codex device poll response: %w", rErr)
		}

		if pResp.StatusCode >= 200 && pResp.StatusCode < 300 {
			if err := json.Unmarshal(pBytes, &tokenResp); err != nil {
				return nil, fmt.Errorf("auth: decode codex device token response: %w", err)
			}
			break
		} else if pResp.StatusCode == http.StatusForbidden || pResp.StatusCode == http.StatusNotFound {
			// Pending authorization; continue polling
			continue
		} else {
			return nil, fmt.Errorf("auth: codex device token polling failed (status %d): %s", pResp.StatusCode, string(pBytes))
		}
	}

	// 3. Exchange authorization code for token
	tokenExchangeURL := spec.TokenEndpoint
	if tokenExchangeURL == "" {
		tokenExchangeURL = "https://auth.openai.com/oauth/token"
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {spec.ClientID},
		"code":          {tokenResp.AuthorizationCode},
		"redirect_uri":  {"https://auth.openai.com/deviceauth/callback"},
		"code_verifier": {tokenResp.CodeVerifier},
	}
	tr, err := postForm(ctx, tokenExchangeURL, form)
	if err != nil {
		return nil, fmt.Errorf("auth: exchange codex device code for tokens: %w", err)
	}

	return buildAuth(spec, tr, auth.KindDevice), nil
}

func parseDevicePollInterval(raw json.RawMessage, defaultInterval time.Duration) time.Duration {
	if len(raw) == 0 {
		return defaultInterval
	}
	var num int64
	if err := json.Unmarshal(raw, &num); err == nil && num > 0 {
		return time.Duration(num) * time.Second
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if val, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && val > 0 {
			return time.Duration(val) * time.Second
		}
	}
	return defaultInterval
}

type deviceAuthorizationResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
	Error           string `json:"error"`
	ErrorDesc       string `json:"error_description"`
}

func postDevice(ctx context.Context, u string, form url.Values) (*deviceAuthorizationResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var d deviceAuthorizationResponse
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("decode device authorization response (status %d): %w: %s", resp.StatusCode, err, truncate(body))
	}
	if d.Error != "" {
		return nil, fmt.Errorf("device authorization error: %s %s", d.Error, d.ErrorDesc)
	}
	if d.DeviceCode == "" || d.UserCode == "" || d.VerificationURI == "" {
		return nil, fmt.Errorf("device authorization returned incomplete response (status %d): %s", resp.StatusCode, truncate(body))
	}
	return &d, nil
}

// isRetryableDeviceError reports whether a polling error means "keep polling".
// RFC 8628 defines authorization_pending (keep polling) and slow_down
// (poll slower); any other error is terminal.
func isRetryableDeviceError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "authorization_pending") || strings.Contains(msg, "slow_down")
}