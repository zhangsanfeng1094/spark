# Compat Proxy Architecture

## Goal
Keep compatibility conversion layered without changing external behavior:
- `client`: caller protocol <-> Compat IR
- `ir`: provider-neutral request, response, stream event, tool, usage, and reasoning model
- `target`: Compat IR <-> upstream provider protocol
- `gateway`: route selection, HTTP entry, stream/non-stream dispatch, gateway errors
- `integration`: upstream URL/key loading, HTTP client, retry policy, logs, local state

## Current State (Function Mapping)

### Codex Responses caller -> OpenAI Chat target
- Gateway handler: `internal/compat/gateway.CodexResponsesHandler`
- Integration executor: `internal/integrations.codexChatExecutor`
- Retry strategy: `shouldRetryWithMinimalChatReq`, `minimalChatCompletionsRequest`, `ultraMinimalChatCompletionsRequest`
- Client request adapter: `internal/compat/client/codex.ResponsesInbound`
- Target adapter: `internal/compat/target/openai_chat.ChatOutbound`
- Stream conversion: `target/openai_chat.ChatStreamEvents` -> `ir.StreamEvent` -> `client/codex.ResponsesStreamWriter`
- Non-stream conversion: `target/openai_chat.ChatResponse` -> `ir.Response` -> `client/codex.ResponsesClientResponse`
- Route: `gateway.Route{Client: codex_responses, Target: openai_chat}`

### Anthropic Messages caller -> OpenAI Chat target
- Gateway handler: `internal/compat/gateway.AnthropicMessagesHandler`
- Integration executor: `internal/integrations.anthropicCompatProxy.postChatCompletions`
- Client request adapter: `internal/compat/client/anthropic_messages.MessagesInbound`
- Target adapter: `internal/compat/target/openai_chat.ChatOutbound`
- Response writer: `target/openai_chat.ChatResponse` -> `client/anthropic_messages.MessagesClientResponse`
- Stream writer: `client/anthropic_messages.WriteMessagesStream`

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

### Phase 2 (Introduce request translator interface)
- Move `RequestTranslator` and route selection into `internal/compat/gateway`.
- Implement translators by composing client adapter + target adapter directly.
- Response and stream paths use client writer functions through gateway orchestration.
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

Current target locations:
- `internal/compat/gateway/stream.go` for Codex Responses stream conversion
- `internal/compat/gateway/anthropic_handler.go` for Anthropic Messages stream conversion

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
