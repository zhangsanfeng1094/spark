package claude

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
	thinking.Apply(rawReq, config.ProtocolAnthropic, thinking.Resolve(activeProfile.Thinking, suffixThinking))

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
			logf("POST /v1/messages streaming request model=%s provider=%s", bReq.Model, bReq.Provider)
		}
		streamChan, bErr := h.engine.Client().ChatCompletionStreamRequest(bCtx, bReq)
		if bErr != nil {
			writeAnthropicError(w, bErr)
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
					Client:            "claude",
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

	if h.logf != nil {
		h.logf("claude non-stream request model=%s provider=%s", bReq.Model, bReq.Provider)
	}

	resp, bErr := h.engine.Client().ChatCompletionRequest(bCtx, bReq)
	if bErr != nil {
		writeAnthropicError(w, bErr)
		return
	}

	if resp.Usage != nil {
		cached := 0
		if resp.Usage.PromptTokensDetails != nil {
			cached = resp.Usage.PromptTokensDetails.CachedReadTokens
		}
		if err := usage.AppendDefault(usage.Record{
			Client:            "claude",
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

	contentBlocks := make([]map[string]any, 0, 4)
	stopReason := "end_turn"

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			fr := strings.ToLower(*choice.FinishReason)
			if strings.Contains(fr, "tool") {
				stopReason = "tool_use"
			} else if strings.Contains(fr, "length") {
				stopReason = "max_tokens"
			}
		}

		var msg *schemas.ChatMessage
		if choice.ChatNonStreamResponseChoice != nil {
			msg = choice.ChatNonStreamResponseChoice.Message
		}

		if msg != nil {
			if msg.ChatAssistantMessage != nil {
				if msg.ChatAssistantMessage.Reasoning != nil && *msg.ChatAssistantMessage.Reasoning != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type":      "thinking",
						"thinking":  *msg.ChatAssistantMessage.Reasoning,
						"signature": "",
					})
				}

				for _, tc := range msg.ChatAssistantMessage.ToolCalls {
					stopReason = "tool_use"
					callID := ""
					if tc.ID != nil {
						callID = *tc.ID
					}
					fnName := ""
					if tc.Function.Name != nil {
						fnName = *tc.Function.Name
					}
					var inputMap any = map[string]any{}
					if tc.Function.Arguments != "" {
						var parsed any
						if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err == nil {
							inputMap = parsed
						}
					}
					contentBlocks = append(contentBlocks, map[string]any{
						"type":  "tool_use",
						"id":    callID,
						"name":  fnName,
						"input": inputMap,
					})
				}
			}

			if msg.Content != nil && msg.Content.ContentStr != nil && *msg.Content.ContentStr != "" {
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "text",
					"text": *msg.Content.ContentStr,
				})
			}
		}
	}

	inputTokens := 0
	outputTokens := 0
	if resp.Usage != nil {
		inputTokens = resp.Usage.PromptTokens
		outputTokens = resp.Usage.CompletionTokens
	}

	msgResp := map[string]any{
		"id":            resp.ID,
		"type":          "message",
		"role":          "assistant",
		"model":         resp.Model,
		"content":       contentBlocks,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(msgResp)
}

func writeAnthropicError(w http.ResponseWriter, bErr *schemas.BifrostError) {
	statusCode := http.StatusInternalServerError
	if bErr.StatusCode != nil && *bErr.StatusCode > 0 {
		statusCode = *bErr.StatusCode
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	errType := "api_error"
	if bErr.Type != nil && *bErr.Type != "" {
		errType = *bErr.Type
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errType,
			"message": bErr.GetErrorString(),
		},
	})
}
