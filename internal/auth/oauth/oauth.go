// Package oauth implements CPA-style upstream login flows: the Device
// Authorization Grant (RFC 8628) and browser Authorization Code with PKCE.
// Both flows exchange credentials for an auth.Auth record that the caller
// persists via auth.Store.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"spark/internal/auth"
)

// HTTPClient is the client used for token/device requests.
var HTTPClient = &http.Client{Timeout: 30 * time.Second}

// tokenResponse is the common shape of a token-endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Email        string `json:"email,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// postForm performs a form-encoded POST to u and decodes the JSON body.
func postForm(ctx context.Context, u string, form url.Values) (*tokenResponse, error) {
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
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode token response (status %d): %w: %s", resp.StatusCode, err, truncate(body))
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("token endpoint error: %s %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint returned no access_token (status %d): %s", resp.StatusCode, truncate(body))
	}
	return &tr, nil
}

func truncate(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// buildAuth converts a token response into an auth.Auth record.
func buildAuth(spec auth.ProviderSpec, tr *tokenResponse, kind auth.Kind) *auth.Auth {
	a := &auth.Auth{
		Provider:     spec.Provider,
		Kind:         kind,
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		IDToken:      tr.IDToken,
		Scope:        tr.Scope,
		ClientID:     spec.ClientID,
		Metadata:     make(map[string]string),
	}
	if tr.ExpiresIn > 0 {
		a.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	if a.Scope == "" {
		a.Scope = spec.Scope
	}

	if spec.Provider == auth.ProviderCodex && tr.IDToken != "" {
		if accID, plan, email, err := auth.ExtractCodexClaims(tr.IDToken); err == nil {
			if accID != "" {
				a.AccountID = accID
				a.Metadata["chatgpt_account_id"] = accID
			}
			if plan != "" {
				a.Metadata["plan_type"] = plan
			}
			if email != "" && a.Account == "" {
				a.Account = email
			}
		}
	}

	if spec.Provider == auth.ProviderGrok {
		if tr.Email != "" && a.Account == "" {
			a.Account = tr.Email
		}
		if tr.IDToken != "" && a.Account == "" {
			if claims, err := auth.ParseJWTClaims(tr.IDToken); err == nil {
				if email, ok := claims["email"].(string); ok && email != "" {
					a.Account = email
				}
			}
		}
	}

	if tr.Email != "" && a.Account == "" {
		a.Account = tr.Email
	}

	return a
}