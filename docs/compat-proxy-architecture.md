# Compat Proxy Architecture (Cautious Migration Plan)

## Goal
Refactor compat proxies to a layered architecture without changing behavior:
- `handler`: HTTP entry + request validation + stream/non-stream dispatch
- `translator`: protocol mapping (`responses/anthropic <-> chat/completions`)
- `executor`: upstream HTTP call + retry + SSE reading
- `writer`: response/error shaping and SSE/json writeback

This document defines a phased path so each step is testable and reversible.

## Current State (Function Mapping)

### Codex compat (`internal/integrations/codex_compat_proxy.go`)
- Handler: `handleResponses`
- Executor: `postChatCompletions`
- Retry strategy: `shouldRetryWithMinimalChatReq`, `minimalChatCompletionsRequest`, `ultraMinimalChatCompletionsRequest`
- Translators: `responsesToChatCompletions`, `responsesInputToMessages`, `responsesToolsToChatTools`, `responsesToolChoiceToChatToolChoice`
- Stream writer/adapter: `forwardStream`, `writeSSE`
- Non-stream writer/adapter: `forwardNonStream`
- Error/write helpers: `writeUpstreamErrorAsJSON`, `writeJSONError`, `decodeResponsesRequest`

### Claude compat (`internal/integrations/claude_compat_proxy.go`)
- Handler: `handleMessages`
- Executor: `postChatCompletions`
- Translator (request): `anthropicToChatCompletions`, `anthropicMessagesToChatMessages`, `anthropicContentToChatParts`, `anthropicToolsToChatTools`, `anthropicToolChoiceToChatToolChoice`
- Translator (response): `chatToAnthropicMessage`, `chatStopReason`
- Stream writer/adapter: `forwardAnthropicStream`, `writeAnthropicStreamFromMessage`, `writeAnthropicSSE`
- Error/write helpers: `writeAnthropicError`

## Phased Plan

### Phase 0 (No behavior change)
- Split big files by responsibility only, still same package:
  - `codex_compat_handler.go`
  - `codex_compat_translate.go`
  - `codex_compat_stream.go`
  - `codex_compat_errors.go`
  - `codex_compat_util.go`
  - same split for `claude_compat_*`
- Acceptance:
  - `go test ./internal/integrations -run Compat -v` passes
  - `go test ./...` passes
- Rollback:
  - restore file split commit only (no logic delta)

### Phase 1 (Extract shared IO primitives)
- Move shared body decoding/log truncation/json logging to `internal/integrations/compatio.go`:
  - `decode*Request` common gzip/zstd/identity reader
  - `truncateForLog`, `mustJSONForLog`
  - common open-log helper with path env and mkdir
- Keep error envelope format separate (`responses`/`anthropic` differ).
- Acceptance:
  - no HTTP payload schema changes
  - golden tests for decode and malformed payloads remain identical

### Phase 2 (Introduce translator interfaces)
- Add interfaces in `internal/integrations/compat_types.go`:
  - `type RequestTranslator interface { ToChat(map[string]any) (map[string]any, error) }`
  - `type NonStreamTranslator interface { FromChat(map[string]any, string) (map[string]any, error) }`
  - `type StreamTranslator interface { ConsumeChunk([]byte) ([]map[string]any, error); Finalize() []map[string]any }`
- Wrap existing functions with adapter structs; keep old functions internally.
- Acceptance:
  - stream and non-stream snapshot tests unchanged

### Phase 3 (Executor boundary)
- Add `ChatExecutor` abstraction:
  - `Do(ctx, chatReq) (*http.Response, error)`
- Move retry policy from handler into a codex-specific executor policy object.
- Acceptance:
  - same retry count and same fallback request shape
  - existing logs still include initial/minimal/ultra-minimal trace

### Phase 4 (Unified pipeline, format-specific writers)
- Generic pipeline function:
  - decode -> translate request -> execute -> route stream/non-stream -> translate response -> write
- Keep format-specific writers:
  - codex writer (`response.*` SSE events)
  - anthropic writer (`event: message_*` SSE events)
- Acceptance:
  - integration tests compare entire SSE output sequence for representative fixtures

## High-Risk Points (Do Not Change Early)
- Codex stream event ordering in `forwardStream`:
  - `response.created` -> `response.in_progress` -> item events -> completed events
- Tool call delta aggregation state machine (`toolStates` + `toolOrder`)
- Reasoning fallback behavior when no content delta is present
- Anthropic stream event names and block index semantics

## Immediate Reliability Actions (for unexpected EOF)
These are safe, low-scope improvements before large refactor:
1. Add log fields when scanner ends:
   - whether `[DONE]` observed
   - bytes/chunks parsed count
   - first/last valid chunk sample
2. If stream exits with scan error and zero parsed chunks:
   - return explicit upstream-stream error event/body instead of silent empty output
3. Guard against very long lines:
   - keep scanner max-size setting and log if limit likely hit

Suggested target locations:
- `internal/integrations/codex_compat_proxy.go` in `forwardStream`
- `internal/integrations/claude_compat_proxy.go` in `forwardAnthropicStream`

## Test Strategy Per Phase
- Unit tests:
  - request translation edge cases (tools/tool_choice/mixed content)
  - error envelope wrapping
  - malformed stream chunk handling
- Snapshot tests:
  - codex stream output sequence
  - anthropic stream output sequence
- Smoke:
  - local loopback server with forced early EOF and malformed SSE

## Definition of Done
- Layered files and interfaces are in place.
- Behavior-compatible with current tests/fixtures.
- Stream EOF diagnostics are explicit and actionable in logs.
