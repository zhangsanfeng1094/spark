package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"spark/internal/compat/gateway/core"
	"spark/internal/compat/logutil"
)

type codexChatExecutor struct {
	proxy *ResponsesProxy
}

type codexAnthropicMessagesExecutor struct {
	proxy *ResponsesProxy
}

func newCodexChatExecutor(proxy *ResponsesProxy) core.Executor {
	return codexChatExecutor{proxy: proxy}
}

func newCodexAnthropicMessagesExecutor(proxy *ResponsesProxy) core.Executor {
	return codexAnthropicMessagesExecutor{proxy: proxy}
}

func (e codexAnthropicMessagesExecutor) Do(ctx context.Context, req map[string]any) (*http.Response, error) {
	upResp, err := e.proxy.postAnthropicMessages(ctx, req)
	if err != nil {
		return nil, err
	}
	e.proxy.logf("upstream status=%d on mapped anthropic messages request", upResp.StatusCode)
	if upResp.StatusCode >= 400 {
		e.proxy.warnf(fmt.Sprintf("upstream returned status %d", upResp.StatusCode))
	}
	return upResp, nil
}

func (e codexChatExecutor) Do(ctx context.Context, chatReq map[string]any) (*http.Response, error) {
	upResp, err := e.proxy.postChatCompletions(ctx, chatReq)
	if err != nil {
		return nil, err
	}
	e.proxy.logf("upstream status=%d on initial mapped request", upResp.StatusCode)
	if upResp.StatusCode < 400 {
		return upResp, nil
	}

	e.proxy.warnf(fmt.Sprintf("upstream returned status %d", upResp.StatusCode))
	data, _ := io.ReadAll(upResp.Body)
	_ = upResp.Body.Close()
	e.proxy.logf(
		"upstream error on initial mapped request status=%d content_type=%q content_encoding=%q body_bytes=%d",
		upResp.StatusCode,
		upResp.Header.Get("Content-Type"),
		upResp.Header.Get("Content-Encoding"),
		len(data),
	)
	if !shouldRetryWithMinimalChatReq(upResp.StatusCode, data) {
		return &http.Response{
			StatusCode: upResp.StatusCode,
			Body:       io.NopCloser(bytes.NewReader(data)),
			Header:     upResp.Header,
		}, nil
	}

	e.proxy.logf("retrying with minimal chat request due to status=%d body_bytes=%d", upResp.StatusCode, len(data))
	minReq := minimalChatCompletionsRequest(chatReq)
	e.proxy.logf("mapped chat request(minimal) structure=%s", logutil.StructureJSONForLog(minReq))
	upResp, err = e.proxy.postChatCompletions(ctx, minReq)
	if err != nil {
		e.proxy.logf("upstream minimal retry failed: %v", err)
		return nil, err
	}
	e.proxy.logf("upstream status=%d on minimal retry", upResp.StatusCode)
	if upResp.StatusCode < 400 {
		return upResp, nil
	}

	data, _ = io.ReadAll(upResp.Body)
	_ = upResp.Body.Close()
	e.proxy.logf(
		"upstream error on minimal retry status=%d content_type=%q content_encoding=%q body_bytes=%d",
		upResp.StatusCode,
		upResp.Header.Get("Content-Type"),
		upResp.Header.Get("Content-Encoding"),
		len(data),
	)
	if !shouldRetryWithMinimalChatReq(upResp.StatusCode, data) {
		return &http.Response{
			StatusCode: upResp.StatusCode,
			Body:       io.NopCloser(bytes.NewReader(data)),
			Header:     upResp.Header,
		}, nil
	}

	e.proxy.logf("retrying with ultra-minimal chat request due to status=%d body_bytes=%d", upResp.StatusCode, len(data))
	ultraReq := ultraMinimalChatCompletionsRequest(chatReq)
	e.proxy.logf("mapped chat request(ultra-minimal) structure=%s", logutil.StructureJSONForLog(ultraReq))
	upResp, err = e.proxy.postChatCompletions(ctx, ultraReq)
	if err != nil {
		e.proxy.logf("upstream ultra-minimal retry failed: %v", err)
		return nil, err
	}
	e.proxy.logf("upstream status=%d on ultra-minimal retry", upResp.StatusCode)
	if upResp.StatusCode >= 400 {
		data, _ := io.ReadAll(upResp.Body)
		_ = upResp.Body.Close()
		e.proxy.logf(
			"upstream error on ultra-minimal retry status=%d content_type=%q content_encoding=%q body_bytes=%d",
			upResp.StatusCode,
			upResp.Header.Get("Content-Type"),
			upResp.Header.Get("Content-Encoding"),
			len(data),
		)
		return &http.Response{
			StatusCode: upResp.StatusCode,
			Body:       io.NopCloser(bytes.NewReader(data)),
			Header:     upResp.Header,
		}, nil
	}
	return upResp, nil
}
