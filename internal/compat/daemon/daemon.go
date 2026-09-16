package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"spark/internal/auth"
	"spark/internal/compat/engine"
	claudeingress "spark/internal/compat/ingress/claude"
	codexingress "spark/internal/compat/ingress/codex"
	geminiingress "spark/internal/compat/ingress/gemini"
	openaiingress "spark/internal/compat/ingress/openai"
	"spark/internal/compat/proxyutil"
	"spark/internal/config"
	"spark/internal/thinking"
)

const (
	DefaultDaemonPort = 42337
	DefaultDaemonAddr = "127.0.0.1:42337"
)

type Server struct {
	addr     string
	listener net.Listener
	server   *http.Server
	engine   *engine.Engine
	mux      *http.ServeMux
	logf     func(format string, args ...any)

	logMu       sync.Mutex
	logFile     *os.File
	sessionLogs map[string]*os.File

	startTime       time.Time
	identity        BinaryIdentity
	managementToken string
}

func NewServer(ctx context.Context, addr string, logf func(format string, args ...any)) (*Server, error) {
	if addr == "" {
		addr = DefaultDaemonAddr
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil && addr == DefaultDaemonAddr {
		// If default port is occupied, try alternative ports in range
		for p := DefaultDaemonPort + 1; p <= DefaultDaemonPort+10; p++ {
			altAddr := fmt.Sprintf("127.0.0.1:%d", p)
			if altLn, altErr := net.Listen("tcp", altAddr); altErr == nil {
				ln = altLn
				addr = altAddr
				err = nil
				break
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("daemon failed to listen on %s: %w", addr, err)
	}

	if logf == nil {
		logf = func(string, ...any) {}
	}

	eng, err := engine.New(ctx, nil, engine.NewSparkLLMPlugin(logf))
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("daemon failed to init bifrost engine: %w", err)
	}

	token, err := generateManagementToken()
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("daemon failed to generate management token: %w", err)
	}

	s := &Server{
		addr:            ln.Addr().String(),
		listener:        ln,
		engine:          eng,
		mux:             http.NewServeMux(),
		logf:            logf,
		sessionLogs:     make(map[string]*os.File),
		startTime:       time.Now(),
		identity:        CurrentIdentity(),
		managementToken: token,
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	// 1. Health check
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/health", s.handleHealth)

	// 2. Profile resolver for dynamic routing
	profileResolver := func(r *http.Request, modelName string) *config.Profile {
		return ResolveProfileFromRequest(r, modelName)
	}

	// 3. Codex Ingress (/v1/responses, /responses)
	codexHandler := codexingress.NewHandler(s.engine, nil, s.logf)
	codexHandler.SetProfileResolver(profileResolver)
	codexHandler.SetSessionLogf(func(req map[string]any) func(format string, args ...any) {
		sessionID := ""
		if md, ok := req["client_metadata"].(map[string]any); ok {
			if sid, ok := md["x-codex-window-id"].(string); ok && sid != "" {
				sessionID = sid
			}
		}
		if sessionID == "" {
			return s.logf
		}
		return s.sessionLogf(sessionID, "codex")
	})

	s.mux.HandleFunc("/v1/responses", codexHandler.ServeHTTP)
	s.mux.HandleFunc("/responses", codexHandler.ServeHTTP)

	// 4. Claude Ingress (/v1/messages, /messages)
	claudeHandler := claudeingress.NewHandler(s.engine, nil, "", s.logf)
	claudeHandler.SetProfileResolver(profileResolver)
	claudeHandler.SetSessionLogf(func(req map[string]any) func(format string, args ...any) {
		sessionID := ""
		if md, ok := req["metadata"].(map[string]any); ok {
			if sid, ok := md["session_id"].(string); ok && sid != "" {
				sessionID = sid
			}
		}
		if sessionID == "" {
			return s.logf
		}
		return s.sessionLogf(sessionID, "claude")
	})

	s.mux.HandleFunc("/v1/messages", claudeHandler.ServeHTTP)
	s.mux.HandleFunc("/messages", claudeHandler.ServeHTTP)

	// 5. OpenAI Ingress (/v1/chat/completions, /chat/completions, /v1/models, /models)
	openaiHandler := openaiingress.NewHandler(s.engine, nil, "", s.logf)
	openaiHandler.SetProfileResolver(profileResolver)
	openaiHandler.SetSessionLogf(func(req map[string]any) func(format string, args ...any) {
		sessionID := ""
		if user, ok := req["user"].(string); ok && user != "" {
			sessionID = user
		}
		if sessionID == "" {
			return s.logf
		}
		return s.sessionLogf(sessionID, "openai")
	})

	s.mux.HandleFunc("/v1/chat/completions", openaiHandler.ServeHTTP)
	s.mux.HandleFunc("/chat/completions", openaiHandler.ServeHTTP)
	s.mux.HandleFunc("/v1/models", openaiHandler.ServeHTTP)
	s.mux.HandleFunc("/models", openaiHandler.ServeHTTP)

	// 6. Gemini Ingress (/v1beta/models/{model}:generateContent)
	geminiHandler := geminiingress.NewHandler(s.engine, nil, "", s.logf)
	geminiHandler.SetProfileResolver(profileResolver)
	s.mux.HandleFunc("/v1beta/", geminiHandler.ServeHTTP)
	s.mux.HandleFunc("/v1/models/", geminiHandler.ServeHTTP)
	s.mux.HandleFunc("/models/", geminiHandler.ServeHTTP)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := geminiingress.ParseGeminiPath(r.URL.Path); ok {
			geminiHandler.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// 6. Management API (/v0/management/...)
	s.mux.HandleFunc("/v0/management/accounts", s.protectManagement(s.handleManagementAccounts))
	s.mux.HandleFunc("/v0/management/auth/accounts", s.protectManagement(s.handleManagementAccounts))
	s.mux.HandleFunc("/v0/management/providers", s.protectManagement(s.handleManagementProviders))
	s.mux.HandleFunc("/v0/management/auth/providers", s.protectManagement(s.handleManagementProviders))
	s.mux.HandleFunc("/v0/management/logout", s.protectManagement(s.handleManagementLogout))
	s.mux.HandleFunc("/v0/management/", s.protectManagement(s.handleManagementDynamic))
}

func (s *Server) handleManagementAccounts(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	store, err := auth.DefaultStore()
	if err != nil {
		http.Error(w, fmt.Sprintf("open auth store: %v", err), http.StatusInternalServerError)
		return
	}
	records, err := store.List()
	if err != nil {
		http.Error(w, fmt.Sprintf("list accounts: %v", err), http.StatusInternalServerError)
		return
	}
	type accountResp struct {
		Provider  string `json:"provider"`
		Kind      string `json:"kind"`
		Account   string `json:"account,omitempty"`
		Expired   bool   `json:"expired"`
		ExpiresAt string `json:"expires_at,omitempty"`
	}
	resp := make([]accountResp, 0, len(records))
	for _, rec := range records {
		resp = append(resp, accountResp{
			Provider:  rec.Provider,
			Kind:      string(rec.Kind),
			Account:   rec.Account,
			Expired:   rec.Expired(0),
			ExpiresAt: rec.ExpiresAt.Format(time.RFC3339),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"accounts": resp,
		"count":    len(resp),
	})
}

func (s *Server) handleManagementProviders(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	type provResp struct {
		Provider           string `json:"provider"`
		Label              string `json:"label"`
		SupportsDeviceFlow bool   `json:"supports_device_flow"`
	}
	specs := make([]provResp, 0, len(auth.Specs))
	for _, spec := range auth.Specs {
		specs = append(specs, provResp{
			Provider:           spec.Provider,
			Label:              spec.Label,
			SupportsDeviceFlow: spec.SupportsDeviceFlow,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"providers": specs,
	})
}

func (s *Server) handleManagementLogout(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost, http.MethodDelete) {
		return
	}
	provider := r.URL.Query().Get("provider")
	if provider == "" && r.Body != nil {
		var req struct {
			Provider string `json:"provider"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		provider = req.Provider
	}
	s.deleteProviderAuth(w, provider)
}

func (s *Server) handleManagementDynamic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v0/management/")
	switch {
	case strings.HasSuffix(path, "-logout"):
		if !requireMethod(w, r, http.MethodPost, http.MethodDelete) {
			return
		}
		s.deleteProviderAuth(w, strings.TrimSuffix(path, "-logout"))
	case strings.HasSuffix(path, "-auth-url"):
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		provider := managementAliasProvider(strings.TrimSuffix(path, "-auth-url"))
		spec, err := auth.SpecFor(provider)
		if err != nil {
			http.Error(w, fmt.Sprintf("unknown provider: %s", provider), http.StatusBadRequest)
			return
		}
		port := spec.DefaultCallbackPort
		if port <= 0 {
			port = 54545
		}
		redirectURI := fmt.Sprintf("http://localhost:%d%s", port, spec.EffectiveCallbackPath())
		state := auth.GenerateRandomState()
		verifier := auth.GenerateCodeVerifier()
		challenge := auth.PKCEChallenge(verifier)
		authURL := spec.BuildAuthURL(redirectURI, state, challenge)
		respPayload := map[string]any{
			"provider":      provider,
			"auth_url":      authURL,
			"callback_port": port,
			"redirect_uri":  redirectURI,
			"state":         state,
			"code_verifier": verifier,
		}
		if spec.SupportsDeviceFlow && spec.DeviceVerificationEndpoint != "" {
			respPayload["device_url"] = spec.DeviceVerificationEndpoint
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respPayload)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) deleteProviderAuth(w http.ResponseWriter, rawProvider string) {
	provider, err := resolveManagementProvider(rawProvider)
	if err != nil {
		http.Error(w, fmt.Sprintf("unknown provider: %s", rawProvider), http.StatusBadRequest)
		return
	}
	store, err := auth.DefaultStore()
	if err != nil {
		http.Error(w, fmt.Sprintf("open auth store: %v", err), http.StatusInternalServerError)
		return
	}
	if err := store.Delete(provider); err != nil {
		http.Error(w, fmt.Sprintf("delete auth: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"provider": provider,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"pid":        os.Getpid(),
		"addr":       s.addr,
		"version":    s.identity.Version,
		"uptime_sec": int(time.Since(s.startTime).Seconds()),
		"protocols":  []string{ProtocolCodex, ProtocolClaude, ProtocolGemini},
		"exe_path":   s.identity.ExePath,
		"exe_mtime":  s.identity.ExeMTime,
		"exe_size":   s.identity.ExeSize,
	})
}

func (s *Server) Start() error {
	s.server = &http.Server{
		Handler: s.mux,
	}

	info := DaemonInfo{
		PID:             os.Getpid(),
		Addr:            s.addr,
		BaseURL:         "http://" + s.addr,
		StartTime:       s.startTime.Format(time.RFC3339),
		Version:         s.identity.Version,
		ExePath:         s.identity.ExePath,
		ExeMTime:        s.identity.ExeMTime,
		ExeSize:         s.identity.ExeSize,
		ManagementToken: s.managementToken,
	}
	_ = writeDaemonInfo(info)

	s.logf("spark shared daemon started on http://%s (pid: %d)", s.addr, os.Getpid())
	return s.server.Serve(s.listener)
}

func (s *Server) Shutdown(ctx context.Context) error {
	_ = removeDaemonInfo()
	s.logMu.Lock()
	for _, f := range s.sessionLogs {
		_ = f.Close()
	}
	s.sessionLogs = make(map[string]*os.File)
	if s.logFile != nil {
		_ = s.logFile.Close()
		s.logFile = nil
	}
	s.logMu.Unlock()

	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) BaseURL() string {
	return "http://" + s.addr
}

func (s *Server) sessionLogf(sessionID, prefix string) func(format string, args ...any) {
	clean := proxyutil.SanitizeLogFilePart(sessionID)
	return func(format string, args ...any) {
		line := fmt.Sprintf("[%s:%s] %s\n", prefix, clean, fmt.Sprintf(format, args...))
		s.logf("[%s:%s] %s", prefix, clean, fmt.Sprintf(format, args...))

		s.logMu.Lock()
		defer s.logMu.Unlock()
		f, ok := s.sessionLogs[clean]
		if !ok {
			dir, err := configDir()
			if err == nil {
				now := time.Now()
				sessionDir := filepath.Join(dir, "logs", prefix, now.Format("2006"), now.Format("01"), now.Format("02"))
				_ = os.MkdirAll(sessionDir, 0o755)
				logPath := filepath.Join(sessionDir, fmt.Sprintf("%s-%s.log", prefix, clean))
				if file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
					f = file
					s.sessionLogs[clean] = file
				}
			}
		}
		if f != nil {
			_, _ = f.WriteString(time.Now().Format("15:04:05.000 ") + line)
		}
	}
}

// ResolveProfileFromRequest dynamically extracts profile name from headers or tokens and resolves Profile.
func ResolveProfileFromRequest(r *http.Request, modelName string) *config.Profile {
	profileName := strings.TrimSpace(r.Header.Get("X-Spark-Profile"))
	if profileName == "" {
		profileName = strings.TrimSpace(r.Header.Get("X-Profile"))
	}
	rawKey := ""
	var sessionThinking *config.ThinkingConfig
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		applyAuthToken(strings.TrimSpace(auth[7:]), &profileName, &rawKey, &sessionThinking)
	}
	if profileName == "" {
		for _, header := range []string{"x-api-key", "x-goog-api-key"} {
			applyAuthToken(r.Header.Get(header), &profileName, &rawKey, &sessionThinking)
			if profileName != "" || rawKey != "" {
				break
			}
		}
	}
	if profileName == "" && rawKey == "" {
		applyAuthToken(r.URL.Query().Get("key"), &profileName, &rawKey, &sessionThinking)
	}

	cfg, err := config.Load()
	if err == nil && cfg != nil {
		// 1. Explicit profile name
		if profileName != "" {
			if p, ok := cfg.Profiles[profileName]; ok && p != nil {
				return withSessionThinking(p, sessionThinking)
			}
		}
		// 2. Matching API key
		if rawKey != "" {
			for _, p := range cfg.Profiles {
				if p != nil && (p.EffectiveAPIKey() == rawKey || p.APIKey == rawKey) {
					return withSessionThinking(p, sessionThinking)
				}
			}
		}
		// 3. Matching requested model
		if modelName != "" {
			normalized := config.NormalizeModel(modelName)
			for _, p := range cfg.Profiles {
				if p != nil && config.NormalizeModel(p.DefaultModel) == normalized {
					return withSessionThinking(p, sessionThinking)
				}
			}
			for _, p := range cfg.Profiles {
				if p != nil {
					for _, m := range p.Models {
						if config.NormalizeModel(m) == normalized {
							return p
						}
					}
				}
			}
		}
		// 4. Default configured profile
		if p, ok := cfg.Profiles[cfg.DefaultProfile]; ok && p != nil {
			return withSessionThinking(p, sessionThinking)
		}
	}

	// Fallback to minimal profile if none configured
	return &config.Profile{
		Endpoint:      "https://api.openai.com/v1",
		Protocol:      config.ProtocolOpenAIChat,
		OpenAIBaseURL: "https://api.openai.com/v1",
	}
}

func applyAuthToken(token string, profileName, rawKey *string, sessionThinking **config.ThinkingConfig) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	if i := strings.IndexByte(token, '?'); i >= 0 {
		query, err := url.ParseQuery(token[i+1:])
		if err == nil && sessionThinking != nil && *sessionThinking == nil {
			if cfg, ok := thinking.ParseShorthand(query.Get("think")); ok {
				*sessionThinking = cfg
			}
		}
		token = token[:i]
	}
	switch {
	case strings.HasPrefix(token, "spark-profile:"):
		if profileName != nil && *profileName == "" {
			*profileName = strings.TrimPrefix(token, "spark-profile:")
		}
	case strings.HasPrefix(token, "spark-key:"):
		if rawKey != nil && *rawKey == "" {
			*rawKey = strings.TrimPrefix(token, "spark-key:")
		}
	case token != "ollama" && token != "spark-compat":
		if rawKey != nil && *rawKey == "" {
			*rawKey = token
		}
	}
}

// withSessionThinking keeps loaded profile objects immutable: config.Load can
// cache/share them while individual requests carry different --think values.
func withSessionThinking(profile *config.Profile, session *config.ThinkingConfig) *config.Profile {
	if profile == nil {
		return nil
	}
	resolved := thinking.Resolve(profile.Thinking, session)
	if resolved == profile.Thinking {
		return profile
	}
	copy := *profile
	copy.Thinking = resolved
	return &copy
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "spark"), nil
}
