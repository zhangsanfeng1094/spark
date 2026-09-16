package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/auth"
	"spark/internal/compat/engine"
	"spark/internal/config"
	"spark/internal/thinking"
	"spark/internal/usage"
)

type Handler struct {
	engine          *engine.Engine
	profile         *config.Profile
	profileResolver func(r *http.Request, modelName string) *config.Profile
	preferredModel  string
	logf            func(format string, args ...any)
	sessionLogf     func(req map[string]any) func(format string, args ...any)
}

func NewHandler(eng *engine.Engine, profile *config.Profile, preferredModel string, logf func(format string, args ...any)) *Handler {
	return &Handler{
		engine:         eng,
		profile:        profile,
		preferredModel: preferredModel,
		logf:           logf,
	}
}

func (h *Handler) SetProfileResolver(fn func(r *http.Request, modelName string) *config.Profile) {
	h.profileResolver = fn
}

func (h *Handler) SetSessionLogf(fn func(map[string]any) func(format string, args ...any)) {
	h.sessionLogf = fn
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimRight(r.URL.Path, "/")
	if r.Method == http.MethodGet && (strings.HasSuffix(path, "/models") || path == "/models") {
		h.handleListModels(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "failed to read request body", "invalid_request_error", "")
		return
	}
	defer r.Body.Close()

	var rawReq map[string]any
	if err := json.Unmarshal(body, &rawReq); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "invalid json body", "invalid_request_error", "")
		return
	}

	if h.preferredModel != "" && stringValue(rawReq["model"]) == "" {
		rawReq["model"] = h.preferredModel
	}

	logf := h.logf
	if h.sessionLogf != nil {
		if scoped := h.sessionLogf(rawReq); scoped != nil {
			logf = scoped
		}
	}

	reqModel := stringValue(rawReq["model"])
	baseModel, suffixThinking := thinking.SplitModelSuffix(reqModel)
	if suffixThinking != nil {
		rawReq["model"] = baseModel
		reqModel = baseModel
	}
	activeProfile := h.profile
	if h.profileResolver != nil {
		if p := h.profileResolver(r, reqModel); p != nil {
			activeProfile = p
		}
	}
	if activeProfile == nil {
		activeProfile = &config.Profile{}
	}
	thinking.Apply(rawReq, config.ProtocolOpenAIChat, thinking.Resolve(activeProfile.Thinking, suffixThinking))
	// ToBifrostRequest also unmarshals the raw payload for OpenAI parameters.
	// Re-encode after policy application so reasoning fields are not stale.
	body, _ = json.Marshal(rawReq)

	provider, cleanedBase, _ := engine.MapProfileAndModelToProvider(activeProfile, reqModel)
	cred := engine.ResolveRequestCredential(r.Context(), activeProfile, r)
	effectiveKey := cred.Value
	if effectiveKey == "" {
		effectiveKey = activeProfile.EffectiveAPIKey()
	}
	_ = h.engine.ConfigureProviderWithHeaders(provider, cleanedBase, effectiveKey, cred.ExtraHeaders)

	bReq := ToBifrostRequest(rawReq, body, activeProfile)
	bCtx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	engine.BindDirectKey(bCtx, bReq.Provider, effectiveKey)

	isStream := boolValue(rawReq["stream"])

	if isStream {
		if logf != nil {
			logf("POST /v1/chat/completions streaming request model=%s provider=%s", bReq.Model, bReq.Provider)
		}
		streamChan, bErr := h.engine.Client().ChatCompletionStreamRequest(bCtx, bReq)
		if bErr != nil {
			writeOpenAIError(w, bErr)
			return
		}

		ForwardStream(w, streamChan, bReq.Model, func(u *schemas.BifrostLLMUsage, model string) {
			if u != nil {
				cached := 0
				if u.PromptTokensDetails != nil {
					cached = u.PromptTokensDetails.CachedReadTokens
				}
				if model == "" {
					model = bReq.Model
				}
				if err := usage.AppendDefault(usage.Record{
					Client:            "openai",
					Model:             model,
					Stream:            true,
					InputTokens:       u.PromptTokens,
					OutputTokens:      u.CompletionTokens,
					TotalTokens:       u.TotalTokens,
					CachedInputTokens: cached,
				}); err != nil && logf != nil {
					logf("token usage record failed: %v", err)
				}
			}
		})
		return
	}

	if logf != nil {
		logf("POST /v1/chat/completions unary request model=%s provider=%s", bReq.Model, bReq.Provider)
	}

	cr, bErr := h.engine.Client().ChatCompletionRequest(bCtx, bReq)
	if bErr != nil {
		writeOpenAIError(w, bErr)
		return
	}

	if cr == nil {
		writeErrorResponse(w, http.StatusBadGateway, "empty response from upstream provider", "api_error", "")
		return
	}

	if cr.Usage != nil {
		cached := 0
		if cr.Usage.PromptTokensDetails != nil {
			cached = cr.Usage.PromptTokensDetails.CachedReadTokens
		}
		recModel := cr.Model
		if recModel == "" {
			recModel = bReq.Model
		}
		if err := usage.AppendDefault(usage.Record{
			Client:            "openai",
			Model:             recModel,
			Stream:            false,
			InputTokens:       cr.Usage.PromptTokens,
			OutputTokens:      cr.Usage.CompletionTokens,
			TotalTokens:       cr.Usage.TotalTokens,
			CachedInputTokens: cached,
		}); err != nil && logf != nil {
			logf("token usage record failed: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cr)
}

func (h *Handler) handleListModels(w http.ResponseWriter, r *http.Request) {
	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	modelsSeen := make(map[string]bool)
	var modelList []modelEntry

	addModel := func(name string, ownedBy string) {
		name = strings.TrimSpace(name)
		if name == "" || modelsSeen[name] {
			return
		}
		modelsSeen[name] = true
		modelList = append(modelList, modelEntry{
			ID:      name,
			Object:  "model",
			Created: 1677610602,
			OwnedBy: ownedBy,
		})
	}

	// 1. From active profile
	if h.profile != nil {
		addModel(h.profile.DefaultModel, "profile")
		for _, m := range h.profile.Models {
			addModel(m, "profile")
		}
	}

	// 2. From all configured profiles
	cfg, err := config.Load()
	if err == nil && cfg != nil {
		for pName, p := range cfg.Profiles {
			if p != nil {
				addModel(p.DefaultModel, "profile:"+pName)
				for _, m := range p.Models {
					addModel(m, "profile:"+pName)
				}
			}
		}
	}

	// 3. From auth catalogs
	for _, cat := range auth.Catalogs {
		addModel(cat.DefaultModel, "auth:"+cat.Provider)
		for _, m := range cat.SuggestedModels {
			addModel(m, "auth:"+cat.Provider)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   modelList,
	})
}

func writeOpenAIError(w http.ResponseWriter, bErr *schemas.BifrostError) {
	status := http.StatusInternalServerError
	if bErr != nil && bErr.StatusCode != nil && *bErr.StatusCode > 0 {
		status = *bErr.StatusCode
	}
	errType := "api_error"
	code := ""
	msg := "unknown error"
	if bErr != nil {
		if bErr.Type != nil && *bErr.Type != "" {
			errType = *bErr.Type
		}
		msg = bErr.GetErrorString()
	}
	writeErrorResponse(w, status, msg, errType, code)
}

func writeErrorResponse(w http.ResponseWriter, status int, message, errType, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	errObj := map[string]any{
		"message": message,
		"type":    errType,
	}
	if code != "" {
		errObj["code"] = code
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": errObj,
	})
}
