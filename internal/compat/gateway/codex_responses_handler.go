package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"spark/internal/compat/client/codex"
	"spark/internal/compat/gateway/bridge"
	"spark/internal/compat/gateway/core"
	reasoningfeature "spark/internal/compat/gateway/features/reasoning"
	"spark/internal/compat/httpjson"
	"spark/internal/compat/policy"
)

const (
	ResponsesModeChatCompletionsOnly   = "chat_completions_only"
	ResponsesModePreferResponses       = "prefer_responses"
	ResponsesModeAnthropicMessagesOnly = "anthropic_messages_only"
)

type codexResponsesHandler struct {
	mode          string
	route         core.Route
	upstreamBase  string
	logf          func(format string, args ...any)
	warnf         func(summary string)
	postResponses func(ctx context.Context, req map[string]any) (*http.Response, error)
	executor      core.Executor
}

type CodexResponsesOptions struct {
	Mode          string
	UpstreamBase  string
	Logf          func(format string, args ...any)
	Warnf         func(summary string)
	PostResponses func(ctx context.Context, req map[string]any) (*http.Response, error)
	Executor      core.Executor
}

func NewCodexResponsesHandler(opts CodexResponsesOptions) codexResponsesHandler {
	mode := opts.Mode
	if mode == "" {
		mode = ResponsesModeChatCompletionsOnly
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	warnf := opts.Warnf
	if warnf == nil {
		warnf = func(string) {}
	}
	route := core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat}
	if mode == ResponsesModeAnthropicMessagesOnly {
		route.Target = core.TargetAnthropicMessages
	}
	return codexResponsesHandler{
		mode:          mode,
		route:         route,
		upstreamBase:  opts.UpstreamBase,
		logf:          logf,
		warnf:         warnf,
		postResponses: opts.PostResponses,
		executor:      opts.Executor,
	}
}

func (h codexResponsesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.logf("request method=%s path=%s content_type=%q content_encoding=%q user_agent=%q",
		r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Content-Encoding"), r.Header.Get("User-Agent"))

	if r.Method != http.MethodPost {
		httpjson.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	req, rawBody, err := httpjson.DecodeRequest(r)
	if err != nil {
		h.logf("raw incoming body bytes=%d", len(rawBody))
		h.logf("decode request failed: %v", err)
		h.warnf("request decode failed")
		httpjson.WriteError(w, http.StatusBadRequest, "invalid json (adapter request decode failed: "+err.Error()+")")
		return
	}
	h.logf("raw incoming body bytes=%d", len(rawBody))
	h.logf("decoded responses request structure=%s", structureJSONForLog(req))

	if h.mode == ResponsesModePreferResponses && h.postResponses != nil {
		upResp, err := h.postResponses(r.Context(), req)
		if err != nil {
			h.logf("upstream responses request failed: %v", err)
			h.warnf("upstream request failed")
			httpjson.WriteError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
			return
		}
		if upResp.StatusCode < 400 {
			h.logf("route=request->responses_passthrough status=%d", upResp.StatusCode)
			defer upResp.Body.Close()
			ForwardResponsesPassthrough(w, upResp, h.logf)
			return
		}
		errBody, _ := io.ReadAll(upResp.Body)
		_ = upResp.Body.Close()
		if !ShouldFallbackFromResponses(upResp.StatusCode, errBody) {
			h.warnf(fmt.Sprintf("forward responses upstream status %d", upResp.StatusCode))
			httpjson.WriteUpstreamError(w, &http.Response{
				StatusCode: upResp.StatusCode,
				Body:       io.NopCloser(bytes.NewReader(errBody)),
				Header:     upResp.Header,
			})
			return
		}
		h.logf("responses passthrough fallback triggered status=%d body_bytes=%d", upResp.StatusCode, len(errBody))
		h.logf("route=request->chat_fallback reason=responses_not_supported status=%d", upResp.StatusCode)
	}

	selection, err := bridge.SelectRouteWithOptions(h.route, bridge.SelectionOptions{
		Reasoning: h.reasoningPolicyForRequest(req),
	})
	if err != nil {
		h.logf("route selection failed: %v", err)
		httpjson.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.logDroppedReasoningControls(req)
	if h.executor == nil {
		h.logf("upstream executor missing")
		h.warnf("upstream executor missing")
		httpjson.WriteError(w, http.StatusBadGateway, "upstream executor missing")
		return
	}

	upstreamReq, upResp, err := core.ExecuteTranslated(r.Context(), req, selection.Translator, h.executor, h.prepareRequestForSelection(selection))
	if err != nil {
		var perr core.PipelineError
		if errors.As(err, &perr) && perr.Stage == core.PipelineStageTranslate {
			h.logf("request translate failed: %v", perr.Err)
			httpjson.WriteError(w, http.StatusBadRequest, "invalid request")
			return
		}
		h.logf("upstream request failed: %v", err)
		h.warnf("upstream request failed")
		httpjson.WriteError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	h.logf("mapped upstream request(initial) structure=%s", structureJSONForLog(upstreamReq))
	h.logf("route=request->%s status=%d", selection.Route.Target, upResp.StatusCode)
	defer upResp.Body.Close()

	stream, _ := req["stream"].(bool)
	if stream {
		ForwardCodexStreamWithWriter(w, upResp, selection.Stream, h.warnf, h.logf)
		return
	}
	ForwardCodexNonStreamWithWriter(w, upResp, selection.NonStream, h.warnf, h.logf)
}

func (h codexResponsesHandler) prepareRequestForSelection(selection bridge.RouteSelection) core.RequestPreparer {
	if selection.Route.Client == core.ClientCodexResponses && selection.Route.Target == core.TargetOpenAIChat {
		return reasoningfeature.ChatReasoningAdapter{UpstreamBase: h.upstreamBase}.ApplyToChatRequest
	}
	return nil
}

func (h codexResponsesHandler) logDroppedReasoningControls(req map[string]any) {
	if h.route.Normalize().Target != core.TargetOpenAIChat {
		return
	}
	irReq := codex.ResponsesInbound(req)
	reasoning := policy.OpenAIChatReasoningPolicy(h.upstreamBase, irReq.Model)
	_, dropped := reasoning.ChatReasoningControls(irReq.Generation.Reasoning)
	if len(dropped) == 0 {
		return
	}
	h.logf("reasoning controls degraded target=%s model=%s dropped=%s", core.TargetOpenAIChat, irReq.Model, strings.Join(dropped, ","))
}

func (h codexResponsesHandler) reasoningPolicyForRequest(req map[string]any) policy.ReasoningPolicy {
	if h.route.Normalize().Target != core.TargetOpenAIChat {
		return policy.ReasoningPolicy{}
	}
	model, _ := req["model"].(string)
	return policy.OpenAIChatReasoningPolicy(h.upstreamBase, model)
}
