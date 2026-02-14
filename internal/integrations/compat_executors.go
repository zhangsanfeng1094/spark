package integrations

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

type codexChatExecutor struct {
	proxy *responsesCompatProxy
}

func newCodexChatExecutor(proxy *responsesCompatProxy) ChatExecutor {
	return codexChatExecutor{proxy: proxy}
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
		"upstream error on initial mapped request status=%d content_type=%q content_encoding=%q body=%s",
		upResp.StatusCode,
		upResp.Header.Get("Content-Type"),
		upResp.Header.Get("Content-Encoding"),
		truncateForLog(string(data), 16*1024),
	)
	if !shouldRetryWithMinimalChatReq(upResp.StatusCode, data) {
		return &http.Response{
			StatusCode: upResp.StatusCode,
			Body:       io.NopCloser(bytes.NewReader(data)),
			Header:     upResp.Header,
		}, nil
	}

	e.proxy.logf("retrying with minimal chat request due to status=%d body=%q", upResp.StatusCode, truncateForLog(string(data), 240))
	minReq := minimalChatCompletionsRequest(chatReq)
	e.proxy.logf("mapped chat request(minimal)=%s", mustJSONForLog(minReq))
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
		"upstream error on minimal retry status=%d content_type=%q content_encoding=%q body=%s",
		upResp.StatusCode,
		upResp.Header.Get("Content-Type"),
		upResp.Header.Get("Content-Encoding"),
		truncateForLog(string(data), 16*1024),
	)
	if !shouldRetryWithMinimalChatReq(upResp.StatusCode, data) {
		return &http.Response{
			StatusCode: upResp.StatusCode,
			Body:       io.NopCloser(bytes.NewReader(data)),
			Header:     upResp.Header,
		}, nil
	}

	e.proxy.logf("retrying with ultra-minimal chat request due to status=%d body=%q", upResp.StatusCode, truncateForLog(string(data), 240))
	ultraReq := ultraMinimalChatCompletionsRequest(chatReq)
	e.proxy.logf("mapped chat request(ultra-minimal)=%s", mustJSONForLog(ultraReq))
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
			"upstream error on ultra-minimal retry status=%d content_type=%q content_encoding=%q body=%s",
			upResp.StatusCode,
			upResp.Header.Get("Content-Type"),
			upResp.Header.Get("Content-Encoding"),
			truncateForLog(string(data), 16*1024),
		)
		return &http.Response{
			StatusCode: upResp.StatusCode,
			Body:       io.NopCloser(bytes.NewReader(data)),
			Header:     upResp.Header,
		}, nil
	}
	return upResp, nil
}

type anthropicChatExecutor struct {
	proxy *anthropicCompatProxy
}

func newAnthropicChatExecutor(proxy *anthropicCompatProxy) ChatExecutor {
	return anthropicChatExecutor{proxy: proxy}
}

func (e anthropicChatExecutor) Do(ctx context.Context, chatReq map[string]any) (*http.Response, error) {
	return e.proxy.postChatCompletions(ctx, chatReq)
}
