package codex

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/compat/engine"
	"spark/internal/config"
	"spark/internal/thinking"
	"spark/internal/usage"
)

type Handler struct {
	engine          *engine.Engine
	profile         *config.Profile
	profileResolver func(r *http.Request, modelName string) *config.Profile
	logf            func(format string, args ...any)
	sessionLogf     func(req map[string]any) func(format string, args ...any)
}

func NewHandler(eng *engine.Engine, profile *config.Profile, logf func(format string, args ...any)) *Handler {
	return &Handler{
		engine:  eng,
		profile: profile,
		logf:    logf,
	}
}

func (h *Handler) SetProfileResolver(fn func(r *http.Request, modelName string) *config.Profile) {
	h.profileResolver = fn
}

func (h *Handler) SetSessionLogf(fn func(map[string]any) func(format string, args ...any)) {
	h.sessionLogf = fn
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var rawReq map[string]any
	if err := json.Unmarshal(body, &rawReq); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
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
	thinking.Apply(rawReq, config.ProtocolOpenAIResponses, thinking.Resolve(activeProfile.Thinking, suffixThinking))

	provider, cleanedBase, _ := engine.MapProfileToProvider(activeProfile)
	cred := engine.ResolveRequestCredential(r.Context(), activeProfile, r)
	effectiveKey := cred.Value
	if effectiveKey == "" {
		effectiveKey = activeProfile.EffectiveAPIKey()
	}
	_ = h.engine.ConfigureProviderWithHeaders(provider, cleanedBase, effectiveKey, cred.ExtraHeaders)

	bReq := ToBifrostRequest(rawReq, activeProfile)
	bCtx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	engine.BindDirectKey(bCtx, bReq.Provider, effectiveKey)

	isStream := boolValue(rawReq["stream"])

	if isStream {
		if logf != nil {
			logf("POST /v1/responses streaming request model=%s provider=%s", bReq.Model, bReq.Provider)
		}
		streamChan, bErr := h.engine.Client().ChatCompletionStreamRequest(bCtx, bReq)
		if bErr != nil {
			writeError(w, bErr)
			return
		}

		ForwardStream(w, streamChan, func(u *schemas.BifrostLLMUsage, model string) {
			if u != nil {
				cached := 0
				if u.PromptTokensDetails != nil {
					cached = u.PromptTokensDetails.CachedReadTokens
				}
				if model == "" {
					model = bReq.Model
				}
				if err := usage.AppendDefault(usage.Record{
					Client:            "codex",
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
		logf("POST /v1/responses non-stream request model=%s provider=%s", bReq.Model, bReq.Provider)
	}

	resp, bErr := h.engine.Client().ChatCompletionRequest(bCtx, bReq)
	if bErr != nil {
		writeError(w, bErr)
		return
	}

	if resp.Usage != nil {
		cached := 0
		if resp.Usage.PromptTokensDetails != nil {
			cached = resp.Usage.PromptTokensDetails.CachedReadTokens
		}
		if err := usage.AppendDefault(usage.Record{
			Client:            "codex",
			Model:             resp.Model,
			Stream:            false,
			InputTokens:       resp.Usage.PromptTokens,
			OutputTokens:      resp.Usage.CompletionTokens,
			TotalTokens:       resp.Usage.TotalTokens,
			CachedInputTokens: cached,
		}); err != nil && logf != nil {
			logf("token usage record failed: %v", err)
		}
	}

	responsesResp := resp.ToBifrostResponsesResponse()
	if responsesResp.Status == nil {
		status := "completed"
		responsesResp.Status = &status
	}
	ensureResponsesUsageDetails(responsesResp.Usage)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(responsesResp)
}

func writeError(w http.ResponseWriter, bErr *schemas.BifrostError) {
	statusCode := http.StatusInternalServerError
	if bErr.StatusCode != nil && *bErr.StatusCode > 0 {
		statusCode = *bErr.StatusCode
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	var code any
	if bErr.Error != nil {
		code = bErr.Error.Code
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": bErr.GetErrorString(),
			"code":    code,
			"type":    bErr.Type,
		},
	})
}
