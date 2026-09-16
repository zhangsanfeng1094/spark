// Package auth provides CPA-style (CLIProxyAPI) authentication: it manages
// upstream OAuth / Device-Flow login sessions for AI providers (Claude Code,
// Codex, Gemini, Grok) and persists them as Auth records in ~/.spark/auth.
//
// The package is deliberately provider-agnostic: the OAuth/device endpoints
// live in providers.go so the flows can be driven against any upstream.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrInvalidStoreID is returned when a provider or account identifier would
// escape the auth store directory or is otherwise unsafe as a path component.
var ErrInvalidStoreID = errors.New("auth: invalid provider or account identifier")

// Provider names used as store keys. Keep in sync with integrations and
// engine credential resolution.
const (
	ProviderClaude      = "claude"
	ProviderCodex       = "codex"
	ProviderGemini      = "gemini"
	ProviderGrok        = "grok"
	ProviderKimi        = "kimi"
	ProviderCommandCode = "commandcode"
)

// Kind describes how an Auth credential was obtained.
type Kind string

const (
	KindOAuth  Kind = "oauth"  // Authorization Code + PKCE (browser)
	KindDevice Kind = "device" // Device Authorization Grant (RFC 8628)
	KindAPIKey Kind = "apikey" // passthrough of a configured key (no login)
)

// Auth is a single upstream account login session. It corresponds to CPA's
// Auth record: the tokens needed to make authenticated requests against an
// upstream provider as a logged-in account.
type Auth struct {
	Provider     string    `json:"provider"`
	Kind         Kind      `json:"kind"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	// ClientID / Scope capture the OAuth parameters used at login time so a
	// refresh can reuse them if the upstream requires it.
	ClientID string `json:"client_id,omitempty"`
	Scope    string `json:"scope,omitempty"`
	// Account is a human-readable identifier for the logged-in principal
	// (e.g. an email or account id) surfaced in CLI/TUI status.
	Account string `json:"account,omitempty"`

	// IDToken holds the OpenID Connect id_token JWT if issued by the provider.
	IDToken string `json:"id_token,omitempty"`
	// AccountID holds the provider-specific account identifier (e.g. ChatGPT Account ID for Codex).
	AccountID string `json:"account_id,omitempty"`
	// Metadata holds arbitrary provider-specific session metadata (e.g. plan_type, chatgpt_account_id).
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt records when the session was established.
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// GenerateRandomState produces a 32-character hexadecimal random state string (aligned with CLIProxyAPI).
func GenerateRandomState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// GenerateCodeVerifier produces a 128-character URL-safe PKCE code verifier (96 random bytes, unpadded base64URL).
func GenerateCodeVerifier() string {
	b := make([]byte, 96)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// GenerateRandomString produces a URL-safe cryptographically random string of length n bytes.
func GenerateRandomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// PKCEChallenge computes the S256 PKCE code challenge from a code verifier.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ParseJWTClaims decodes the payload of an unverified JWT token.
func ParseJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid jwt format")
	}
	payloadSegment := parts[1]
	if m := len(payloadSegment) % 4; m != 0 {
		payloadSegment += strings.Repeat("=", 4-m)
	}
	decoded, err := base64.URLEncoding.DecodeString(payloadSegment)
	if err != nil {
		return nil, fmt.Errorf("decode jwt payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal jwt payload: %w", err)
	}
	return claims, nil
}

// ExtractCodexClaims extracts account_id, plan_type, and email from a Codex ID token JWT.
func ExtractCodexClaims(idToken string) (accountID, planType, email string, err error) {
	claims, err := ParseJWTClaims(idToken)
	if err != nil {
		return "", "", "", err
	}
	if em, ok := claims["email"].(string); ok {
		email = em
	}
	if authObj, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if acc, ok := authObj["chatgpt_account_id"].(string); ok {
			accountID = acc
		}
		if plan, ok := authObj["chatgpt_plan_type"].(string); ok {
			planType = plan
		}
	}
	if accountID == "" {
		if acc, ok := claims["chatgpt_account_id"].(string); ok {
			accountID = acc
		} else if acc, ok := claims["account_id"].(string); ok {
			accountID = acc
		}
	}
	return accountID, planType, email, nil
}

// Expired reports whether the access token is missing, already past, or within
// the given skew of expiry.
func (a *Auth) Expired(skew time.Duration) bool {
	if a == nil {
		return true
	}
	if a.AccessToken == "" {
		return true
	}
	if a.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(skew).After(a.ExpiresAt)
}

// ParseAuthRef parses an auth reference string in "provider:account" or "provider" format.
// Examples:
//
//	"claude:work" -> ("claude", "work")
//	"commandcode:default" -> ("commandcode", "default")
//	"claude" -> ("claude", "")
func ParseAuthRef(ref string) (provider, account string) {
	ref = strings.TrimSpace(ref)
	if idx := strings.Index(ref, ":"); idx >= 0 {
		return strings.TrimSpace(ref[:idx]), strings.TrimSpace(ref[idx+1:])
	}
	return ref, ""
}

// FormatAuthRef formats provider and account into a canonical auth reference string.
func FormatAuthRef(provider, account string) string {
	provider = strings.TrimSpace(provider)
	account = strings.TrimSpace(account)
	if account == "" || account == "default" {
		return provider
	}
	return provider + ":" + account
}

// sanitizeAccountFileName converts an account name to a safe filesystem filename.
func sanitizeAccountFileName(account string) string {
	account = strings.TrimSpace(account)
	if account == "" {
		return "default"
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	name := replacer.Replace(account)
	if name == "." || name == ".." {
		return "_" + name
	}
	return name
}

func validPathComponent(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.Contains(id, string(os.PathSeparator)) {
		return false
	}
	if strings.ContainsAny(id, `/\`) {
		return false
	}
	if strings.Contains(id, "\x00") {
		return false
	}
	for _, r := range id {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validProviderID(provider string) bool {
	if !validPathComponent(provider) {
		return false
	}
	return !strings.Contains(provider, ":")
}

func (s *Store) rootDir() (string, error) {
	root, err := filepath.Abs(s.dir)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		return resolved, nil
	}
	return root, nil
}

func pathWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

func (s *Store) confinedPath(parts ...string) (string, error) {
	root, err := s.rootDir()
	if err != nil {
		return "", err
	}
	joined := filepath.Join(append([]string{s.dir}, parts...)...)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(root, abs) {
		return "", ErrInvalidStoreID
	}

	info, lerr := os.Lstat(abs)
	switch {
	case lerr == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return "", ErrInvalidStoreID
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			if !pathWithinRoot(root, resolved) {
				return "", ErrInvalidStoreID
			}
			return resolved, nil
		}
		return abs, nil
	case !os.IsNotExist(lerr):
		return "", lerr
	}

	parent := filepath.Dir(abs)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", ErrInvalidStoreID
		}
		if !pathWithinRoot(root, parent) {
			return "", ErrInvalidStoreID
		}
		return abs, nil
	}
	if !pathWithinRoot(root, resolvedParent) {
		return "", ErrInvalidStoreID
	}
	candidate := filepath.Join(resolvedParent, filepath.Base(abs))
	if !pathWithinRoot(root, candidate) {
		return "", ErrInvalidStoreID
	}
	if info, err := os.Lstat(candidate); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidStoreID
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return candidate, nil
}

func writeFileReplace(path string, data []byte, perm os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidStoreID
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".auth-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func (s *Store) providerFile(provider string) (string, error) {
	if !validProviderID(provider) {
		return "", ErrInvalidStoreID
	}
	return s.confinedPath(provider + ".json")
}

func (s *Store) providerDir(provider string) (string, error) {
	if !validProviderID(provider) {
		return "", ErrInvalidStoreID
	}
	return s.confinedPath(provider)
}

func (s *Store) accountFile(provider, account string) (string, error) {
	if !validProviderID(provider) {
		return "", ErrInvalidStoreID
	}
	name := sanitizeAccountFileName(account)
	if !validPathComponent(name) {
		return "", ErrInvalidStoreID
	}
	return s.confinedPath(provider, name+".json")
}

// Store persists Auth records on disk under ~/.spark/auth.
// Supports both nested accounts (~/.spark/auth/<provider>/<account>.json)
// and legacy flat files (~/.spark/auth/<provider>.json).
// Files are written with 0600 because they contain credentials.
type Store struct {
	dir string
}

// DefaultStore returns a Store rooted at the default Spark config directory.
func DefaultStore() (*Store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	return &Store{dir: filepath.Join(dir, "auth")}, nil
}

// NewStore builds a Store rooted at dir (the parent that contains auth/).
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the directory holding Auth record files.
func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) path(provider string) string {
	p, err := s.providerFile(provider)
	if err != nil {
		return filepath.Join(s.dir, sanitizeAccountFileName(provider)+".json")
	}
	return p
}

func (s *Store) readAuthFile(path string) (*Auth, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidStoreID
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a Auth
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse auth file %s: %w", path, err)
	}
	return &a, nil
}

// Get loads the Auth record for provider or auth reference.
// If providerOrRef contains a colon (e.g. "claude:work"), it calls GetByRef.
func (s *Store) Get(providerOrRef string) (*Auth, error) {
	if strings.Contains(providerOrRef, ":") {
		return s.GetByRef(providerOrRef)
	}
	return s.GetAccount(providerOrRef, "")
}

// GetByRef loads an Auth record by reference string ("provider:account" or "provider").
func (s *Store) GetByRef(ref string) (*Auth, error) {
	provider, account := ParseAuthRef(ref)
	return s.GetAccount(provider, account)
}

// GetAccount loads an Auth record for a specific provider and account.
// If account is empty or "default", it resolves the default account.
func (s *Store) GetAccount(provider, account string) (*Auth, error) {
	provider = strings.TrimSpace(provider)
	account = strings.TrimSpace(account)
	if provider == "" {
		return nil, nil
	}
	if !validProviderID(provider) {
		return nil, ErrInvalidStoreID
	}

	// 1. If a specific account is requested
	if account != "" && account != "default" {
		if accPath, err := s.accountFile(provider, account); err == nil {
			if a, err := s.readAuthFile(accPath); err == nil && a != nil {
				return a, nil
			}
		} else if !errors.Is(err, ErrInvalidStoreID) {
			return nil, err
		}
		// Search all account files in provider dir for matching a.Account
		if provDir, err := s.providerDir(provider); err == nil {
			if entries, err := os.ReadDir(provDir); err == nil {
				for _, e := range entries {
					if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
						if a, err := s.readAuthFile(filepath.Join(provDir, e.Name())); err == nil && a != nil {
							if a.Account == account {
								return a, nil
							}
						}
					}
				}
			}
		}
		// Check legacy flat file
		if flat, err := s.providerFile(provider); err == nil {
			if a, err := s.readAuthFile(flat); err == nil && a != nil {
				if a.Account == account {
					return a, nil
				}
			}
		}
		return nil, nil
	}

	// 2. Default/empty account:
	// Check legacy/active flat file first: ~/.spark/auth/<provider>.json
	if flat, err := s.providerFile(provider); err == nil {
		if a, err := s.readAuthFile(flat); err == nil && a != nil {
			return a, nil
		}
	}

	// Check default.json in provider dir
	if defPath, err := s.accountFile(provider, "default"); err == nil {
		if a, err := s.readAuthFile(defPath); err == nil && a != nil {
			return a, nil
		}
	}

	// Check first file in provider dir if any
	if provDir, err := s.providerDir(provider); err == nil {
		if entries, err := os.ReadDir(provDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
					if a, err := s.readAuthFile(filepath.Join(provDir, e.Name())); err == nil && a != nil {
						return a, nil
					}
				}
			}
		}
	}

	// Fallback for commandcode
	if provider == ProviderCommandCode {
		if a := loadCommandCodeLegacyAuth(); a != nil {
			return a, nil
		}
	}

	return nil, nil
}

// Save persists an Auth record for provider/account.
func (s *Store) Save(a *Auth) error {
	if a == nil {
		return s.Delete("")
	}
	if a.Provider == "" {
		return fmt.Errorf("auth provider is required")
	}
	if !validProviderID(a.Provider) {
		return ErrInvalidStoreID
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}

	provDir, err := s.providerDir(a.Provider)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(provDir, 0o700); err != nil {
		return err
	}
	accPath, err := s.accountFile(a.Provider, a.Account)
	if err != nil {
		return err
	}
	if err := writeFileReplace(accPath, data, 0o600); err != nil {
		return err
	}

	flat, err := s.providerFile(a.Provider)
	if err != nil {
		return err
	}
	return writeFileReplace(flat, data, 0o600)
}

// Delete removes the Auth record for provider or auth reference.
func (s *Store) Delete(providerOrRef string) error {
	if strings.Contains(providerOrRef, ":") {
		provider, account := ParseAuthRef(providerOrRef)
		return s.DeleteAccount(provider, account)
	}
	provider := strings.TrimSpace(providerOrRef)
	if provider == "" {
		return nil
	}
	if !validProviderID(provider) {
		return ErrInvalidStoreID
	}
	flat, err := s.providerFile(provider)
	if err != nil {
		return err
	}
	if err := os.Remove(flat); err != nil && !os.IsNotExist(err) {
		return err
	}
	provDir, err := s.providerDir(provider)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(provDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// DeleteAccount removes a specific account record for a provider.
func (s *Store) DeleteAccount(provider, account string) error {
	provider = strings.TrimSpace(provider)
	account = strings.TrimSpace(account)
	if provider == "" {
		return nil
	}
	if !validProviderID(provider) {
		return ErrInvalidStoreID
	}
	if account == "" || account == "default" {
		if defPath, err := s.accountFile(provider, "default"); err == nil {
			_ = os.Remove(defPath)
		} else if !errors.Is(err, ErrInvalidStoreID) {
			return err
		}
		if flat, err := s.providerFile(provider); err == nil {
			_ = os.Remove(flat)
		} else {
			return err
		}
		return nil
	}
	accPath, err := s.accountFile(provider, account)
	if err != nil {
		return err
	}
	if err := os.Remove(accPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if flatPath, err := s.providerFile(provider); err == nil {
		if flat, err := s.readAuthFile(flatPath); err == nil && flat != nil {
			if flat.Account == account {
				_ = os.Remove(flatPath)
			}
		}
	} else {
		return err
	}
	return nil
}

// List returns all Auth records (deduplicating by provider and account).
func (s *Store) List() ([]*Auth, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	out := make([]*Auth, 0, len(entries))
	seen := make(map[string]bool)
	hasCommandCode := false

	// 1. Read nested directories (~/.spark/auth/<provider>/<account>.json)
	for _, e := range entries {
		if e.IsDir() {
			provider := e.Name()
			if !validProviderID(provider) {
				continue
			}
			provDir, err := s.providerDir(provider)
			if err != nil {
				continue
			}
			subEntries, err := os.ReadDir(provDir)
			if err != nil {
				continue
			}
			for _, sub := range subEntries {
				if sub.IsDir() || filepath.Ext(sub.Name()) != ".json" {
					continue
				}
				accPath, err := s.confinedPath(provider, sub.Name())
				if err != nil {
					continue
				}
				a, err := s.readAuthFile(accPath)
				if err != nil || a == nil {
					continue
				}
				if a.Provider == ProviderCommandCode {
					hasCommandCode = true
				}
				key := a.Provider + ":" + a.Account
				if !seen[key] {
					seen[key] = true
					out = append(out, a)
				}
			}
		}
	}

	// 2. Read legacy flat files (~/.spark/auth/<provider>.json)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		provider := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if !validProviderID(provider) {
			continue
		}
		flat, err := s.providerFile(provider)
		if err != nil {
			continue
		}
		a, err := s.readAuthFile(flat)
		if err != nil || a == nil {
			continue
		}
		if a.Provider == ProviderCommandCode {
			hasCommandCode = true
		}
		key := a.Provider + ":" + a.Account
		if !seen[key] {
			seen[key] = true
			out = append(out, a)
		}
	}

	// 3. CommandCode legacy fallback
	if !hasCommandCode {
		if a := loadCommandCodeLegacyAuth(); a != nil {
			out = append(out, a)
		}
	}
	return out, nil
}

// ListAccounts returns all accounts for a specific provider.
func (s *Store) ListAccounts(provider string) ([]*Auth, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []*Auth
	norm := strings.ToLower(strings.TrimSpace(provider))
	for _, a := range all {
		if strings.ToLower(strings.TrimSpace(a.Provider)) == norm {
			out = append(out, a)
		}
	}
	return out, nil
}

type legacyCommandCodeAuth struct {
	APIKey   string `json:"apiKey"`
	UserID   string `json:"userId,omitempty"`
	UserName string `json:"userName,omitempty"`
	KeyName  string `json:"keyName,omitempty"`
}

func loadCommandCodeLegacyAuth() *Auth {
	// 1. Check ~/.commandcode/auth.json
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		filePath := filepath.Join(home, ".commandcode", "auth.json")
		if data, err := os.ReadFile(filePath); err == nil {
			var legacy legacyCommandCodeAuth
			if err := json.Unmarshal(data, &legacy); err == nil && strings.TrimSpace(legacy.APIKey) != "" {
				account := legacy.UserName
				if account == "" {
					account = legacy.UserID
				}
				if legacy.KeyName != "" && account != "" && account != legacy.KeyName {
					account = fmt.Sprintf("%s (%s)", account, legacy.KeyName)
				}
				return &Auth{
					Provider:    ProviderCommandCode,
					Kind:        KindOAuth,
					AccessToken: strings.TrimSpace(legacy.APIKey),
					Account:     account,
					CreatedAt:   time.Now(),
				}
			}
		}
	}

	// 2. Check COMMAND_CODE_API_KEY environment variable
	if envKey := strings.TrimSpace(os.Getenv("COMMAND_CODE_API_KEY")); envKey != "" {
		return &Auth{
			Provider:    ProviderCommandCode,
			Kind:        KindAPIKey,
			AccessToken: envKey,
			CreatedAt:   time.Now(),
		}
	}

	return nil
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".spark"), nil
}
