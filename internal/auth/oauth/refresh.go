package oauth

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"spark/internal/auth"
)

// Refresh attempts to renew an Auth's access token using its refresh token.
// It returns a new Auth record with refreshed tokens, preserving the
// original provider/kind. If the Auth has no refresh token or the provider
// lacks a token endpoint, an error is returned.
func Refresh(ctx context.Context, spec auth.ProviderSpec, a *auth.Auth) (*auth.Auth, error) {
	if a == nil {
		return nil, fmt.Errorf("auth: cannot refresh nil auth")
	}
	if a.RefreshToken == "" {
		return nil, fmt.Errorf("auth: %s has no refresh token; re-login required", a.Provider)
	}
	if spec.TokenEndpoint == "" {
		return nil, fmt.Errorf("auth: %s has no token endpoint", a.Provider)
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {a.RefreshToken},
		"client_id":     {clientIDFor(spec)},
	}
	if spec.ClientSecret != "" {
		form.Set("client_secret", spec.ClientSecret)
	}
	if spec.Provider == auth.ProviderCodex {
		form.Set("scope", "openid profile email")
	} else if a.Scope != "" {
		form.Set("scope", a.Scope)
	}
	tr, err := postForm(ctx, spec.TokenEndpoint, form)
	if err != nil {
		return nil, fmt.Errorf("auth: refresh %s: %w", a.Provider, err)
	}
	refreshed := buildAuth(spec, tr, a.Kind)
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = a.RefreshToken
	}
	if refreshed.Account == "" {
		refreshed.Account = a.Account
	}
	if refreshed.AccountID == "" {
		refreshed.AccountID = a.AccountID
	}
	if refreshed.IDToken == "" {
		refreshed.IDToken = a.IDToken
	}
	if len(refreshed.Metadata) == 0 && len(a.Metadata) > 0 {
		refreshed.Metadata = make(map[string]string, len(a.Metadata))
		for k, v := range a.Metadata {
			refreshed.Metadata[k] = v
		}
	}
	refreshed.ClientID = a.ClientID
	if refreshed.Scope == "" {
		refreshed.Scope = a.Scope
	}
	return refreshed, nil
}

// EnsureFresh returns a non-expired Auth for the provider, refreshing the
// stored record if needed and persisting the result. It returns nil (with no
// error) when the provider has no Auth record.
func EnsureFresh(ctx context.Context, store *auth.Store, spec auth.ProviderSpec, skew time.Duration) (*auth.Auth, error) {
	if store == nil {
		return nil, nil
	}
	a, err := store.Get(spec.Provider)
	if err != nil || a == nil {
		return nil, err
	}
	if !a.Expired(skew) {
		return a, nil
	}
	if a.RefreshToken == "" {
		// Expired with no way to refresh: treat as not logged in.
		return nil, nil
	}
	refreshed, err := Refresh(ctx, spec, a)
	if err != nil {
		return nil, err
	}
	if err := store.Save(refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}
