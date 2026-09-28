package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"spark/internal/config"
	"spark/internal/tui"
	"spark/internal/version"
	"spark/web"
)

type Options struct {
	Addr  string
	DevUI string
}

type Server struct {
	server *http.Server
}

func New(opts Options) (*Server, error) {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "127.0.0.1:8765"
	}
	h, err := newHandler(strings.TrimSpace(opts.DevUI))
	if err != nil {
		return nil, err
	}
	return &Server{server: &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}}, nil
}

func (s *Server) ListenAndServe() error {
	if s == nil || s.server == nil {
		return errors.New("http server is nil")
	}
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) Addr() string {
	if s == nil || s.server == nil {
		return ""
	}
	return s.server.Addr
}

func IsWideListenAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSpace(host)
	return host == "" || host == "0.0.0.0" || host == "::"
}

type apiServer struct{}

func newHandler(devUI string) (http.Handler, error) {
	api := &apiServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", api.health)
	mux.HandleFunc("/api/config/summary", api.summary)
	mux.HandleFunc("/api/profiles", api.profiles)
	mux.HandleFunc("/api/profiles/default", api.profileDefault)
	mux.HandleFunc("/api/profiles/", api.profileByName)
	mux.HandleFunc("/api/profiles/fetch-models", api.profileFetchModels)
	mux.HandleFunc("/api/codex/models", api.codexModels)
	mux.HandleFunc("/api/claude/models", api.claudeModels)

	if devUI != "" {
		target, err := url.Parse(devUI)
		if err != nil {
			return nil, fmt.Errorf("invalid --dev-ui URL: %w", err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		mux.Handle("/", proxy)
		return withJSONHeaders(mux), nil
	}

	static, err := web.Dist()
	if err != nil {
		return nil, err
	}
	mux.Handle("/", spaHandler{fsys: static})
	return withJSONHeaders(mux), nil
}

func withJSONHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		next.ServeHTTP(w, r)
	})
}

type spaHandler struct{ fsys fs.FS }

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || fileMissing(h.fsys, name) {
		data, err := fs.ReadFile(h.fsys, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
		return
	}
	http.FileServer(http.FS(h.fsys)).ServeHTTP(w, r)
}

func fileMissing(fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return true
	}
	_ = f.Close()
	return false
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	if message == "" {
		message = http.StatusText(status)
	}
	w.WriteHeader(status)
	var out apiError
	out.Error.Code = code
	out.Error.Message = message
	_ = json.NewEncoder(w).Encode(out)
}

func writeJSON(w http.ResponseWriter, value any) {
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst) == nil
}

func loadConfig(w http.ResponseWriter) (*config.RootConfig, bool) {
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load_failed", err.Error())
		return nil, false
	}
	return cfg, true
}

func saveConfig(w http.ResponseWriter, cfg *config.RootConfig) bool {
	if err := config.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return false
	}
	return true
}

func (s *apiServer) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	configPath, _ := config.ConfigPath()
	writeJSON(w, map[string]any{"ok": true, "version": version.Get().String(), "config_path": configPath})
}

func (s *apiServer) summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	writeJSON(w, map[string]any{
		"default_profile": cfg.DefaultProfile,
		"profile_count":   len(cfg.Profiles),
	})
}

type profileDTO struct {
	Name             string   `json:"name"`
	ProviderType     string   `json:"provider_type,omitempty"`
	AuthProvider     string   `json:"auth_provider,omitempty"`
	OpenAIBaseURL    string   `json:"openai_base_url"`
	APIKey           *string  `json:"api_key,omitempty"`
	ClearAPIKey      bool     `json:"clear_api_key,omitempty"`
	OpenAIAPIType    string   `json:"openai_api_type"`
	ModelListURL     string   `json:"model_list_url"`
	Models           []string `json:"models"`
	DefaultModel     string   `json:"default_model"`
	HasAPIKey        bool     `json:"has_api_key"`
	AnthropicBaseURL string   `json:"anthropic_base_url,omitempty"`
}

func (s *apiServer) profiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, ok := loadConfig(w)
		if ok {
			writeJSON(w, profilesResponse(cfg))
		}
	case http.MethodPost:
		var body profileDTO
		if !decodeJSON(r, &body) {
			writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
			return
		}
		cfg, ok := loadConfig(w)
		if !ok {
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "validation_error", "profile name is required")
			return
		}
		if _, exists := cfg.Profiles[name]; exists {
			writeError(w, http.StatusConflict, "conflict", "profile already exists")
			return
		}
		profile, err := profileFromDTO(nil, body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		cfg.Profiles[name] = profile
		if saveConfig(w, cfg) {
			writeJSON(w, profilesResponse(cfg))
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *apiServer) profileByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	name, err := url.PathUnescape(name)
	if err != nil || strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid profile name")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	if _, exists := cfg.Profiles[name]; !exists {
		writeError(w, http.StatusNotFound, "not_found", "profile not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body profileDTO
		if !decodeJSON(r, &body) {
			writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
			return
		}
		newName := strings.TrimSpace(body.Name)
		if newName == "" {
			writeError(w, http.StatusBadRequest, "validation_error", "profile name is required")
			return
		}
		if newName != name {
			if _, exists := cfg.Profiles[newName]; exists {
				writeError(w, http.StatusConflict, "conflict", "profile already exists")
				return
			}
		}
		profile, err := profileFromDTO(cfg.Profiles[name], body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		cfg.Profiles[newName] = profile
		if newName != name {
			delete(cfg.Profiles, name)
			if cfg.DefaultProfile == name {
				cfg.DefaultProfile = newName
			}
			if cfg.History.LastProfile == name {
				cfg.History.LastProfile = newName
			}
			for _, ic := range cfg.Integrations {
				if ic != nil && ic.Profile == name {
					ic.Profile = newName
				}
			}
		}
		if saveConfig(w, cfg) {
			writeJSON(w, profilesResponse(cfg))
		}
	case http.MethodDelete:
		if len(cfg.Profiles) <= 1 {
			writeError(w, http.StatusConflict, "conflict", "cannot delete the last profile")
			return
		}
		delete(cfg.Profiles, name)
		if cfg.DefaultProfile == name {
			cfg.DefaultProfile = firstProfileName(cfg)
		}
		if cfg.History.LastProfile == name {
			cfg.History.LastProfile = cfg.DefaultProfile
		}
		for _, ic := range cfg.Integrations {
			if ic != nil && ic.Profile == name {
				ic.Profile = cfg.DefaultProfile
			}
		}
		if saveConfig(w, cfg) {
			writeJSON(w, profilesResponse(cfg))
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *apiServer) profileFetchModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	var body profileDTO
	if !decodeJSON(r, &body) {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
		return
	}

	// Load config to get existing API key if the profile exists
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}

	// Build a temporary profile from the request body
	profile := &config.Profile{
		Endpoint:         strings.TrimSpace(body.OpenAIBaseURL),
		Protocol:         config.NormalizeAPIProtocol(body.OpenAIAPIType),
		OpenAIBaseURL:    strings.TrimSpace(body.OpenAIBaseURL),
		OpenAIAPIType:    strings.TrimSpace(body.OpenAIAPIType),
		ModelListURL:     strings.TrimSpace(body.ModelListURL),
		AnthropicBaseURL: strings.TrimSpace(body.AnthropicBaseURL),
		AuthProvider:     strings.TrimSpace(body.AuthProvider),
	}

	// Use API key from request if provided, otherwise try to get from saved profile
	if body.APIKey != nil && strings.TrimSpace(*body.APIKey) != "" {
		profile.APIKey = strings.TrimSpace(*body.APIKey)
	} else if body.Name != "" {
		// Try to get API key from existing profile
		if existing, exists := cfg.Profiles[strings.TrimSpace(body.Name)]; exists && existing != nil {
			profile.APIKey = existing.EffectiveAPIKey()
			profile.Credential = existing.Credential
			profile.AuthRef = existing.AuthRef
			profile.Thinking = existing.Thinking
			if profile.AuthProvider == "" {
				profile.AuthProvider = existing.AuthProvider
			}
		}
	}
	profile.Credential.APIKey = profile.APIKey
	if profile.Credential.Mode == "" {
		profile.Credential.Mode = config.CredentialModeAuto
	}
	if profile.Credential.AuthRef == "" && profile.AuthProvider != "" {
		profile.Credential.AuthRef = profile.AuthProvider + ":default"
	}
	if profile.Endpoint == "" {
		profile.Endpoint = profile.EffectiveEndpoint()
	}

	// Use the TUI's FetchOpenAIModels function to get the models
	models, err := fetchOpenAIModelsForProfile(profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fetch_error", err.Error())
		return
	}

	writeJSON(w, map[string]any{"models": models})
}

func (s *apiServer) profileDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		body, err := readBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", "invalid request body")
			return
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err == nil {
			if _, ok := raw["openai_base_url"]; ok {
				r.Body = io.NopCloser(bytes.NewReader(body))
				s.profileByName(w, r)
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	if r.Method != http.MethodPut {
		s.profileByName(w, r)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(r, &body) {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	if err := cfg.SetDefaultProfile(strings.TrimSpace(body.Name)); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if saveConfig(w, cfg) {
		writeJSON(w, profilesResponse(cfg))
	}
}

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func profilesResponse(cfg *config.RootConfig) map[string]any {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]profileDTO, 0, len(names))
	for _, name := range names {
		p := cfg.Profiles[name]
		profiles = append(profiles, profileToDTO(name, p))
	}
	return map[string]any{"default_profile": cfg.DefaultProfile, "profiles": profiles}
}

func profileToDTO(name string, p *config.Profile) profileDTO {
	if p == nil {
		p = &config.Profile{}
	}
	return profileDTO{
		Name:             name,
		ProviderType:     detectProviderType(p),
		AuthProvider:     p.AuthProvider,
		OpenAIBaseURL:    p.EffectiveEndpoint(),
		OpenAIAPIType:    displayAPIType(p.OpenAIAPIType),
		ModelListURL:     p.ModelListURL,
		Models:           append([]string{}, p.Models...),
		DefaultModel:     p.DefaultModel,
		HasAPIKey:        p.EffectiveAPIKey() != "",
		AnthropicBaseURL: p.AnthropicBaseURL,
	}
}

func profileFromDTO(existing *config.Profile, body profileDTO) (*config.Profile, error) {
	baseURL := strings.TrimSpace(body.OpenAIBaseURL)
	if baseURL == "" {
		return nil, errors.New("openai_base_url is required")
	}
	apiType := config.CanonicalizeOpenAIAPITypes(body.OpenAIAPIType)
	if apiType == "" {
		apiType = config.DefaultOpenAIAPIType
	}
	key := ""
	if existing != nil {
		key = existing.EffectiveAPIKey()
	}
	if body.ClearAPIKey {
		key = ""
	} else if body.APIKey != nil {
		key = strings.TrimSpace(*body.APIKey)
	}
	authProvider := strings.TrimSpace(body.AuthProvider)
	if authProvider == "" && existing != nil {
		authProvider = existing.AuthProvider
	}
	providerChanged := existing != nil && strings.TrimSpace(existing.AuthProvider) != authProvider
	p := &config.Profile{
		Endpoint:      baseURL,
		Protocol:      config.NormalizeAPIProtocol(apiType),
		OpenAIBaseURL: baseURL,
		APIKey:        key,
		AuthProvider:  authProvider,
		OpenAIAPIType: apiType,
		ModelListURL:  strings.TrimSpace(body.ModelListURL),
		Models:        config.NormalizeModels(body.Models),
		DefaultModel:  config.NormalizeModel(body.DefaultModel),
		OpenAIOrg:     "",
		OpenAIProject: "",
	}
	if existing != nil {
		p.OpenAIOrg = existing.OpenAIOrg
		p.OpenAIProject = existing.OpenAIProject
		p.Thinking = existing.Thinking
		if !providerChanged {
			p.Credential = existing.Credential
			p.AuthRef = existing.AuthRef
		}
	}
	p.Credential.APIKey = key
	if p.Credential.Mode == "" {
		p.Credential.Mode = config.CredentialModeAuto
	}
	if providerChanged && authProvider != "" {
		ref := authProvider + ":default"
		p.Credential.AuthRef = ref
		p.AuthRef = ref
		// Keep api_key mode only when the request explicitly supplies a
		// replacement key for the new provider. An inherited key from the
		// previous provider must not stay active.
		submittedNewKey := !body.ClearAPIKey && body.APIKey != nil && strings.TrimSpace(*body.APIKey) != ""
		if submittedNewKey {
			p.Credential.Mode = config.CredentialModeAPIKey
		} else {
			p.Credential.Mode = config.CredentialModeAuth
		}
	} else if p.Credential.AuthRef == "" && authProvider != "" {
		p.Credential.AuthRef = authProvider + ":default"
	}
	if p.AuthRef == "" && authProvider != "" {
		p.AuthRef = p.Credential.AuthRef
	}
	if config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeAnthropicMessages) {
		p.AnthropicBaseURL = baseURL
	}
	return p, nil
}

func firstProfileName(cfg *config.RootConfig) string {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "default"
	}
	for _, name := range names {
		if name == "default" {
			return name
		}
	}
	return names[0]
}

func fetchOpenAIModelsForProfile(profile *config.Profile) ([]string, error) {
	return tui.FetchOpenAIModels(profile)
}

func displayAPIType(v string) string {
	canonical := config.CanonicalizeOpenAIAPITypes(v)
	if canonical == "" {
		return config.DefaultOpenAIAPIType
	}
	return canonical
}

func detectProviderType(p *config.Profile) string {
	if p == nil {
		return "OpenAI Compatible"
	}
	if p.AuthProvider == "commandcode" || strings.Contains(strings.ToLower(p.EffectiveEndpoint()), ":3050") || strings.Contains(strings.ToLower(p.EffectiveEndpoint()), "commandcode") {
		return "Command Code"
	}
	if strings.TrimSpace(p.AnthropicBaseURL) != "" || p.AuthProvider == "claude" {
		return "Anthropic"
	}
	base := strings.ToLower(strings.TrimSpace(p.EffectiveEndpoint()))
	switch {
	case strings.Contains(base, "localhost:11434") || strings.Contains(base, "127.0.0.1:11434"):
		return "Ollama"
	case p.AuthProvider == "gemini" || strings.Contains(base, "generativelanguage.googleapis.com") || strings.Contains(base, "ai.google.dev"):
		return "Gemini"
	case base == "https://api.openai.com/v1" || base == "" || p.AuthProvider == "codex":
		return "OpenAI"
	default:
		return "OpenAI Compatible"
	}
}

func (s *apiServer) codexModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	models, err := fetchCodexModelsFromCache()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fetch_error", err.Error())
		return
	}

	// Extract just the model slugs
	modelSlugs := make([]string, 0, len(models))
	for _, model := range models {
		modelSlugs = append(modelSlugs, model.Slug)
	}

	writeJSON(w, map[string]interface{}{
		"models": modelSlugs,
	})
}

// CodexModelInfo represents a simplified model info from Codex cache
type CodexModelInfo struct {
	Slug string `json:"slug"`
}

// fetchCodexModelsFromCache reads models from ~/.codex/models_cache.json
func fetchCodexModelsFromCache() ([]CodexModelInfo, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get user home: %w", err)
		}
		codexHome = filepath.Join(userHome, ".codex")
	}

	cachePath := filepath.Join(codexHome, "models_cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("read models cache: %w", err)
	}

	var cache struct {
		Models []CodexModelInfo `json:"models"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse models cache: %w", err)
	}

	return cache.Models, nil
}

func (s *apiServer) claudeModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	// Claude Code doesn't have a models cache like Codex, so return common models
	models := []string{
		"claude-opus-4-8",
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-sonnet-3-7-20250219",
		"claude-haiku-4-5-20251001",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-sonnet-20240620",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
	}

	writeJSON(w, map[string]interface{}{
		"models": models,
	})
}
