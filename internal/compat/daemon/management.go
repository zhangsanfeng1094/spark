package daemon

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"spark/internal/auth"
)

const managementTokenHeader = "X-Spark-Management-Token"

func generateManagementToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) ManagementToken() string {
	if s == nil {
		return ""
	}
	return s.managementToken
}

func (s *Server) protectManagement(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requestFromLoopback(r) {
			http.Error(w, "management API is only available from loopback", http.StatusForbidden)
			return
		}
		if !originAllowed(r) {
			http.Error(w, "invalid management request origin", http.StatusForbidden)
			return
		}
		if !s.managementTokenOK(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) managementTokenOK(r *http.Request) bool {
	if s == nil || s.managementToken == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get(managementTokenHeader))
	if got == "" {
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			got = strings.TrimSpace(authz[7:])
		}
	}
	if got == "" {
		got = strings.TrimSpace(r.URL.Query().Get("management_token"))
	}
	if len(got) != len(s.managementToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.managementToken)) == 1
}

func requestFromLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func originAllowed(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
			origin = ref
		}
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requireMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	return false
}

func resolveManagementProvider(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("provider is required")
	}
	if strings.Contains(raw, ":") {
		provider, account := auth.ParseAuthRef(raw)
		spec, err := auth.SpecFor(provider)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(account) == "" {
			return spec.Provider, nil
		}
		return auth.FormatAuthRef(spec.Provider, account), nil
	}
	spec, err := auth.SpecFor(raw)
	if err != nil {
		return "", err
	}
	return spec.Provider, nil
}

func managementAliasProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "anthropic":
		return auth.ProviderClaude
	case "antigravity":
		return auth.ProviderGemini
	case "xai":
		return auth.ProviderGrok
	case "commandcode-proxy", "command-code", "cmd":
		return auth.ProviderCommandCode
	default:
		return strings.TrimSpace(raw)
	}
}
