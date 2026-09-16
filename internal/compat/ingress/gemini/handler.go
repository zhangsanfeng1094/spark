package gemini

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

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
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pathModel, pathStream, ok := ParseGeminiPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !pathStream && strings.EqualFold(r.URL.Query().Get("alt"), "sse") {
		pathStream = true
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
	if rawReq == nil {
		rawReq = map[string]any{}
	}

	if stringValue(rawReq["model"]) == "" {
		if pathModel != "" {
			rawReq["model"] = pathModel
		} else if h.preferredModel != "" {
			rawReq["model"] = h.preferredModel
		}
	}
	rawReq["stream"] = pathStream

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
	thinking.Apply(rawReq, config.ProtocolGemini, thinking.Resolve(activeProfile.Thinking, suffixThinking))

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

	if pathStream {
		if logf != nil {
			logf("POST %s streaming request model=%s provider=%s", r.URL.Path, bReq.Model, bReq.Provider)
		}
		if !shouldUseUpstreamChatStream(activeProfile) {
			if logf != nil {
				logf("skipping upstream chat stream for api_type=%s; using non-stream", strings.TrimSpace(activeProfile.OpenAIAPIType))
			}
			h.writeStreamFromNonStream(w, r, bReq, effectiveKey, logf)
			return
		}
		streamChan, bErr := h.engine.Client().ChatCompletionStreamRequest(bCtx, bReq)
		if bErr != nil {
			if logf != nil {
				logf("upstream stream failed (%s); retrying non-stream", bifrostErrorMessage(bErr))
			}
			h.writeStreamFromNonStream(w, r, bReq, effectiveKey, logf)
			return
		}
		first, ok := takeFirstStreamChunk(streamChan)
		if !ok || (first != nil && first.BifrostError != nil) {
			if logf != nil {
				logf("upstream stream chunk failed (%s); retrying non-stream", bifrostErrorMessage(streamChunkError(first)))
			}
			drainStream(streamChan)
			h.writeStreamFromNonStream(w, r, bReq, effectiveKey, logf)
			return
		}
		ForwardStream(w, prependStreamChunk(first, streamChan), bReq.Model, func(u *schemas.BifrostLLMUsage, model string) {
			recordGeminiUsage(u, model, bReq.Model, true, logf)
		})
		return
	}

	if logf != nil {
		logf("POST %s non-stream request model=%s provider=%s", r.URL.Path, bReq.Model, bReq.Provider)
	}
	resp, bErr := h.chatCompletion(r, bReq, activeProfile.EffectiveAPIKey(), logf)
	if bErr != nil {
		writeGeminiError(w, bErr)
		return
	}
	if resp != nil {
		recordGeminiUsage(resp.Usage, resp.Model, bReq.Model, false, logf)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(geminiNonStreamResponse(resp))
}

func (h *Handler) writeStreamFromNonStream(w http.ResponseWriter, r *http.Request, bReq *schemas.BifrostChatRequest, apiKey string, logf func(format string, args ...any)) {
	if bReq == nil {
		http.Error(w, "missing request", http.StatusInternalServerError)
		return
	}
	if bReq.Params != nil {
		bReq.Params.StreamOptions = nil
	}
	resp, bErr := h.chatCompletion(r, bReq, apiKey, logf)
	if bErr != nil {
		writeGeminiError(w, bErr)
		return
	}
	if resp != nil {
		recordGeminiUsage(resp.Usage, resp.Model, bReq.Model, true, logf)
	}
	writeGeminiStreamFromNonStream(w, resp)
}

func (h *Handler) chatCompletion(r *http.Request, bReq *schemas.BifrostChatRequest, apiKey string, logf func(format string, args ...any)) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	resp, bErr := h.doChatCompletion(r, bReq, apiKey)
	if bErr == nil || !isAuthUnavailable(bErr) || !stripUnsupportedChatParams(bReq) {
		return resp, bErr
	}
	if logf != nil {
		logf("retrying without tools/reasoning after %s", bifrostErrorMessage(bErr))
	}
	return h.doChatCompletion(r, bReq, apiKey)
}

func (h *Handler) doChatCompletion(r *http.Request, bReq *schemas.BifrostChatRequest, apiKey string) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	bCtx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	engine.BindDirectKey(bCtx, bReq.Provider, apiKey)
	return h.engine.Client().ChatCompletionRequest(bCtx, bReq)
}

func shouldUseUpstreamChatStream(profile *config.Profile) bool {
	if profile == nil {
		return true
	}
	apiType := strings.TrimSpace(profile.OpenAIAPIType)
	if apiType == "" {
		return true
	}
	return config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeChatCompletions)
}

func streamChunkError(chunk *schemas.BifrostStreamChunk) *schemas.BifrostError {
	if chunk == nil {
		return nil
	}
	return chunk.BifrostError
}

func bifrostErrorMessage(bErr *schemas.BifrostError) string {
	if bErr == nil {
		return "unknown error"
	}
	if msg := strings.TrimSpace(bErr.GetErrorString()); msg != "" {
		return msg
	}
	return "unknown error"
}

func isAuthUnavailable(bErr *schemas.BifrostError) bool {
	msg := strings.ToLower(bifrostErrorMessage(bErr))
	return strings.Contains(msg, "auth_unavailable") || strings.Contains(msg, "no auth available")
}

func stripUnsupportedChatParams(bReq *schemas.BifrostChatRequest) bool {
	if bReq == nil || bReq.Params == nil {
		return false
	}
	stripped := false
	if len(bReq.Params.Tools) > 0 {
		bReq.Params.Tools = nil
		stripped = true
	}
	if bReq.Params.ToolChoice != nil {
		bReq.Params.ToolChoice = nil
		stripped = true
	}
	if bReq.Params.Reasoning != nil {
		bReq.Params.Reasoning = nil
		stripped = true
	}
	return stripped
}

func recordGeminiUsage(u *schemas.BifrostLLMUsage, model, fallback string, stream bool, logf func(format string, args ...any)) {
	if u == nil {
		return
	}
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedReadTokens
	}
	if model == "" {
		model = fallback
	}
	if err := usage.AppendDefault(usage.Record{
		Client:            "agy",
		Model:             model,
		Stream:            stream,
		InputTokens:       u.PromptTokens,
		OutputTokens:      u.CompletionTokens,
		TotalTokens:       u.TotalTokens,
		CachedInputTokens: cached,
	}); err != nil && logf != nil {
		logf("token usage record failed: %v", err)
	}
}

func writeGeminiError(w http.ResponseWriter, bErr *schemas.BifrostError) {
	statusCode := http.StatusInternalServerError
	if bErr != nil && bErr.StatusCode != nil && *bErr.StatusCode > 0 {
		statusCode = *bErr.StatusCode
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(geminiErrorBody(bErr))
}
