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
	mux.HandleFunc("/api/prompts", api.prompts)
	mux.HandleFunc("/api/prompts/enabled", api.promptEnabled)
	mux.HandleFunc("/api/prompts/presets", api.promptPresets)
	mux.HandleFunc("/api/prompts/presets/", api.promptPresetByName)
	mux.HandleFunc("/api/prompts/bindings", api.promptBindings)
	mux.HandleFunc("/api/prompts/bindings/", api.promptBindingByKey)
	mux.HandleFunc("/api/prompts/templates", api.promptTemplates)
	mux.HandleFunc("/api/prompts/validate", api.promptValidate)
	mux.HandleFunc("/api/profiles", api.profiles)
	mux.HandleFunc("/api/profiles/default", api.profileDefault)
	mux.HandleFunc("/api/profiles/", api.profileByName)
	mux.HandleFunc("/api/profiles/fetch-models", api.profileFetchModels)
	mux.HandleFunc("/api/codex/models", api.codexModels)
	mux.HandleFunc("/api/codex/prompt", api.codexPrompt)
	mux.HandleFunc("/api/claude/models", api.claudeModels)
	mux.HandleFunc("/api/claude/prompt", api.claudePrompt)

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
	issues := cfg.CheckPrompts()
	errors, warnings := 0, 0
	for _, issue := range issues {
		if issue.Severity == config.PromptValidationError {
			errors++
		} else {
			warnings++
		}
	}
	writeJSON(w, map[string]any{
		"default_profile": cfg.DefaultProfile,
		"prompt_enabled":  cfg.Prompts.IsEnabled(),
		"profile_count":   len(cfg.Profiles),
		"preset_count":    len(cfg.Prompts.Presets),
		"binding_count":   len(cfg.Prompts.Bindings),
		"issue_counts":    map[string]int{"errors": errors, "warnings": warnings},
	})
}

type promptPresetDTO struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	File        string  `json:"file"`
	Mode        string  `json:"mode"`
	Content     *string `json:"content,omitempty"`
}

type promptBindingDTO struct {
	Integration string `json:"integration"`
	Model       string `json:"model"`
	Preset      string `json:"preset"`
	Enabled     bool   `json:"enabled"`
}

type promptTemplateDTO struct {
	Integration string `json:"integration"`
	Model       string `json:"model"`
	Content     string `json:"content"`
}

var promptTemplates = []promptTemplateDTO{
	{
		Integration: config.PromptIntegrationCodex,
		Model:       "gpt-5",
		Content: strings.TrimSpace(`You are Codex, a coding agent based on GPT-5. You are working inside a local repository with the user.

Follow the repository's existing patterns, keep changes scoped, and verify the result before handing work back.

[Add your changes below]`) + "\n",
	},
	{
		Integration: config.PromptIntegrationClaude,
		Model:       "claude-sonnet-4-20250514",
		Content: strings.TrimSpace(`You are Claude, an AI assistant created by Anthropic.

You should be helpful, harmless, and honest. Answer the user's request directly and adapt to their context.

[Add your changes below]`) + "\n",
	},
	{
		Integration: config.PromptIntegrationClaude,
		Model:       "claude-3-opus",
		Content: strings.TrimSpace(`You are Claude, an AI assistant created by Anthropic.

The current date is {{date}}.

Be thoughtful, precise, and transparent about uncertainty when it matters.

[Add your changes below]`) + "\n",
	},
}

func (s *apiServer) prompts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	writeJSON(w, promptResponse(cfg))
}

func promptResponse(cfg *config.RootConfig) map[string]any {
	presets := make([]promptPresetDTO, 0, len(cfg.Prompts.Presets))
	for _, name := range cfg.PromptPresetNames() {
		p := cfg.Prompts.Presets[name]
		preset := promptPresetDTO{Name: p.Name, Description: p.Description, File: p.File, Mode: config.NormalizePromptMode(p.Mode)}
		if _, content, err := config.ResolvePromptPresetFile(p); err == nil {
			preset.Content = &content
		}
		presets = append(presets, preset)
	}
	bindings := append([]config.PromptBinding{}, cfg.Prompts.Bindings...)
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Integration == bindings[j].Integration {
			return bindings[i].Model < bindings[j].Model
		}
		return bindings[i].Integration < bindings[j].Integration
	})
	bindingDTOs := make([]promptBindingDTO, 0, len(bindings))
	for _, b := range bindings {
		bindingDTOs = append(bindingDTOs, promptBindingDTO{Integration: b.Integration, Model: b.Model, Preset: b.Preset, Enabled: b.IsEnabled()})
	}
	return map[string]any{"enabled": cfg.Prompts.IsEnabled(), "presets": presets, "bindings": bindingDTOs, "issues": promptIssues(cfg.CheckPrompts())}
}

func promptIssues(in []config.PromptValidationIssue) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, issue := range in {
		out = append(out, map[string]any{"severity": issue.Severity, "active": issue.Active, "message": issue.Message})
	}
	return out
}

func (s *apiServer) promptTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	out := append([]promptTemplateDTO{}, promptTemplates...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Integration == out[j].Integration {
			return out[i].Model < out[j].Model
		}
		return out[i].Integration < out[j].Integration
	})
	writeJSON(w, map[string]any{"templates": out})
}

func (s *apiServer) promptEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(r, &body) {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	cfg.Prompts.SetEnabled(body.Enabled)
	if saveConfig(w, cfg) {
		writeJSON(w, promptResponse(cfg))
	}
}

func (s *apiServer) promptPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var body promptPresetDTO
	if !decodeJSON(r, &body) {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	if _, exists := cfg.Prompts.Presets[strings.TrimSpace(body.Name)]; exists {
		writeError(w, http.StatusConflict, "conflict", "prompt preset already exists")
		return
	}
	if err := upsertPromptPreset(cfg, "", body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if saveConfig(w, cfg) {
		writeJSON(w, promptResponse(cfg))
	}
}

func (s *apiServer) promptPresetByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/prompts/presets/")
	name, err := url.PathUnescape(name)
	if err != nil || strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid preset name")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body promptPresetDTO
		if !decodeJSON(r, &body) {
			writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
			return
		}
		if _, exists := cfg.Prompts.Presets[name]; !exists {
			writeError(w, http.StatusNotFound, "not_found", "prompt preset not found")
			return
		}
		if err := upsertPromptPreset(cfg, name, body); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		if saveConfig(w, cfg) {
			writeJSON(w, promptResponse(cfg))
		}
	case http.MethodDelete:
		if _, exists := cfg.Prompts.Presets[name]; !exists {
			writeError(w, http.StatusNotFound, "not_found", "prompt preset not found")
			return
		}
		if err := cfg.RemovePromptPreset(name); err != nil {
			writeError(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		if saveConfig(w, cfg) {
			writeJSON(w, promptResponse(cfg))
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func upsertPromptPreset(cfg *config.RootConfig, oldName string, body promptPresetDTO) error {
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return errors.New("preset name is required")
	}
	mode := config.NormalizePromptMode(body.Mode)
	if mode == "" {
		return errors.New("preset mode must be append or replace")
	}
	file := strings.TrimSpace(body.File)
	if file == "" {
		file = "prompts/" + name + ".md"
	}
	path, err := config.ResolvePromptPath(file)
	if err != nil {
		return err
	}
	if oldName != "" && oldName != name {
		if _, exists := cfg.Prompts.Presets[name]; exists {
			return errors.New("prompt preset already exists")
		}
		for i := range cfg.Prompts.Bindings {
			if cfg.Prompts.Bindings[i].Preset == oldName {
				cfg.Prompts.Bindings[i].Preset = name
			}
		}
		delete(cfg.Prompts.Presets, oldName)
	}
	cfg.Prompts.Presets[name] = &config.PromptPreset{Name: name, Description: strings.TrimSpace(body.Description), File: file, Mode: mode}
	if body.Content != nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(*body.Content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (s *apiServer) promptBindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var body promptBindingDTO
	if !decodeJSON(r, &body) {
		writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	if findBinding(cfg, body.Integration, body.Model) >= 0 {
		writeError(w, http.StatusConflict, "conflict", "prompt binding already exists")
		return
	}
	if err := upsertPromptBinding(cfg, -1, body); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if saveConfig(w, cfg) {
		writeJSON(w, promptResponse(cfg))
	}
}

func (s *apiServer) promptBindingByKey(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/prompts/bindings/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "validation_error", "binding path must include integration and model")
		return
	}
	integration, _ := url.PathUnescape(parts[0])
	model, _ := url.PathUnescape(parts[1])
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	idx := findBinding(cfg, integration, model)
	if idx < 0 {
		writeError(w, http.StatusNotFound, "not_found", "prompt binding not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body promptBindingDTO
		if !decodeJSON(r, &body) {
			writeError(w, http.StatusBadRequest, "validation_error", "invalid json body")
			return
		}
		if err := upsertPromptBinding(cfg, idx, body); err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		if saveConfig(w, cfg) {
			writeJSON(w, promptResponse(cfg))
		}
	case http.MethodDelete:
		cfg.Prompts.Bindings = append(cfg.Prompts.Bindings[:idx], cfg.Prompts.Bindings[idx+1:]...)
		if saveConfig(w, cfg) {
			writeJSON(w, promptResponse(cfg))
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func findBinding(cfg *config.RootConfig, integration, model string) int {
	integration = config.NormalizePromptIntegration(integration)
	model = config.NormalizePromptBindingModel(model)
	for i, b := range cfg.Prompts.Bindings {
		if b.Integration == integration && b.Model == model {
			return i
		}
	}
	return -1
}

func upsertPromptBinding(cfg *config.RootConfig, idx int, body promptBindingDTO) error {
	integration := config.NormalizePromptIntegration(body.Integration)
	model := config.NormalizePromptBindingModel(body.Model)
	preset := strings.TrimSpace(body.Preset)
	if integration == "" {
		return errors.New("integration must be codex or claude")
	}
	if model == "" {
		return errors.New("model is required")
	}
	if cfg.Prompts.Presets[preset] == nil {
		return errors.New("preset does not exist")
	}
	binding := config.PromptBinding{Integration: integration, Model: model, Preset: preset}
	binding.SetEnabled(body.Enabled)
	if idx >= 0 {
		cfg.Prompts.Bindings[idx] = binding
	} else {
		cfg.Prompts.Bindings = append(cfg.Prompts.Bindings, binding)
	}
	return nil
}

func (s *apiServer) promptValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	cfg, ok := loadConfig(w)
	if !ok {
		return
	}
	writeJSON(w, map[string]any{"issues": promptIssues(cfg.CheckPrompts())})
}

type profileDTO struct {
	Name             string   `json:"name"`
	ProviderType     string   `json:"provider_type,omitempty"`
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
		OpenAIBaseURL:     strings.TrimSpace(body.OpenAIBaseURL),
		OpenAIAPIType:     strings.TrimSpace(body.OpenAIAPIType),
		ModelListURL:      strings.TrimSpace(body.ModelListURL),
		AnthropicBaseURL:  strings.TrimSpace(body.AnthropicBaseURL),
	}

	// Use API key from request if provided, otherwise try to get from saved profile
	if body.APIKey != nil && strings.TrimSpace(*body.APIKey) != "" {
		profile.APIKey = strings.TrimSpace(*body.APIKey)
	} else if body.Name != "" {
		// Try to get API key from existing profile
		if existing, exists := cfg.Profiles[strings.TrimSpace(body.Name)]; exists && existing != nil {
			profile.APIKey = existing.EffectiveAPIKey()
		}
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
		OpenAIBaseURL:    p.OpenAIBaseURL,
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
	p := &config.Profile{
		OpenAIBaseURL: baseURL,
		APIKey:        key,
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
	if strings.TrimSpace(p.AnthropicBaseURL) != "" || config.SupportsOpenAIAPIType(p.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) {
		return "Anthropic"
	}
	base := strings.ToLower(strings.TrimSpace(p.OpenAIBaseURL))
	switch {
	case strings.Contains(base, "localhost:11434") || strings.Contains(base, "127.0.0.1:11434"):
		return "Ollama"
	case strings.Contains(base, "generativelanguage.googleapis.com") || strings.Contains(base, "ai.google.dev"):
		return "Gemini"
	case base == "https://api.openai.com/v1" || base == "":
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

func (s *apiServer) codexPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	modelSlug := r.URL.Query().Get("model")
	if modelSlug == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "model parameter is required")
		return
	}

	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "home_error", err.Error())
			return
		}
		codexHome = filepath.Join(userHome, ".codex")
	}

	cachePath := filepath.Join(codexHome, "models_cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_error", fmt.Sprintf("read models cache: %v", err))
		return
	}

	var cache struct {
		Models []struct {
			Slug             string `json:"slug"`
			BaseInstructions string `json:"base_instructions"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		writeError(w, http.StatusInternalServerError, "parse_error", fmt.Sprintf("parse models cache: %v", err))
		return
	}

	// Find the model by slug (exact match or prefix match)
	var baseInstructions string
	for _, model := range cache.Models {
		if model.Slug == modelSlug || strings.HasPrefix(model.Slug, modelSlug) {
			baseInstructions = model.BaseInstructions
			break
		}
	}

	if baseInstructions == "" {
		writeError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("model %q not found in cache", modelSlug))
		return
	}

	writeJSON(w, map[string]interface{}{
		"prompt": baseInstructions,
	})
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

func (s *apiServer) claudePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	modelSlug := r.URL.Query().Get("model")
	if modelSlug == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "model parameter is required")
		return
	}

	// Try to get the system prompt from Claude Code
	prompt, err := fetchClaudeSystemPrompt(modelSlug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fetch_error", err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"prompt": prompt,
	})
}

func fetchClaudeSystemPrompt(modelSlug string) (string, error) {
	// Try to use claude command to get the system prompt
	// Since Claude Code doesn't have a direct way to get system prompt,
	// we'll return a default template based on the model

	// Check if this is a Claude 4.x or 5.x model
	isClaudeOpus4 := strings.Contains(modelSlug, "opus-4") || strings.Contains(modelSlug, "opus-5")
	isClaudeSonnet4 := strings.Contains(modelSlug, "sonnet-4") || strings.Contains(modelSlug, "sonnet-5")
	isClaudeHaiku4 := strings.Contains(modelSlug, "haiku-4") || strings.Contains(modelSlug, "haiku-5")

	var basePrompt string
	if isClaudeOpus4 {
		basePrompt = `You are Claude, an AI assistant created by Anthropic. You are running in Claude Code CLI with the Opus model.

You are helpful, harmless, and honest. You should provide thoughtful, nuanced responses and be transparent about limitations.`
	} else if isClaudeSonnet4 {
		basePrompt = `You are Claude, an AI assistant created by Anthropic. You are running in Claude Code CLI with the Sonnet model.

You are helpful, harmless, and honest. You should be balanced between capability and speed.`
	} else if isClaudeHaiku4 {
		basePrompt = `You are Claude, an AI assistant created by Anthropic. You are running in Claude Code CLI with the Haiku model.

You are helpful, harmless, and honest. You should provide fast, efficient responses.`
	} else {
		// Default for Claude 3.x models
		basePrompt = `You are Claude, an AI assistant created by Anthropic.

You are helpful, harmless, and honest. Answer the user's request directly and adapt to their context.`
	}

	return basePrompt, nil
}

