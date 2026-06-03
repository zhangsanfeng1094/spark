# Gateway Refactor TODO

This TODO tracks gateway cleanup in the order Spark should execute it: clean responsibilities first, split files second, move packages last.

## Goal

Keep the compatibility gateway organized around protocol roles and conversion direction, not vendor names.

Preferred mental model:

```text
client protocol -> IR -> target protocol
```

Examples:

- Codex Responses -> IR -> OpenAI Chat
- Codex Responses -> IR -> Anthropic Messages
- Anthropic Messages -> IR -> OpenAI Chat
- Gemini GenerateContent -> IR -> target protocol

Avoid using `provider/openai` or `provider/anthropic` as the main boundary, because OpenAI and Anthropic can each appear as either client protocols or target protocols.

## Phase 1: Clean Responsibility Boundaries

Do this before moving directories.

- [x] Keep `reasoning_effort` policy out of proxy/integration layers.
- [x] Move Claude/Anthropic Messages `reasoning_effort` selection into gateway handler.
- [x] Keep proxy layer limited to runtime wiring: upstream URL, key, preferred model, log hooks, executor, and cache lifetime.
- [x] Move `reasoning_content` apply/remember orchestration into gateway handlers.
- [x] Remove reasoning callback injection from Claude proxy.
- [x] Remove `PrepareChat` reasoning callback injection from Codex proxy.
- [x] Delegate reasoning echo target detection to `policy.RequiresOpenAIChatReasoningEcho`.
- [x] Remove `gateway.ShouldPassReasoningContentForTarget` wrapper after call sites can use policy directly.
- [x] Move proxy-package tests that directly exercise `ChatReasoningAdapter` into gateway tests.
- [x] Add route-handler regression tests for:
  - LiteLLM/GLM drops `reasoning_effort`.
  - MiMo/DeepSeek keeps or restores `reasoning_content` for assistant tool-call messages.
  - Generic OpenAI-compatible targets strip message-level `reasoning_content`.

## Phase 2: Clarify Gateway Files Without Package Moves

Rename and split files inside `internal/compat/gateway` first. This keeps import paths stable.

- [x] Rename `codex_handler.go` to `codex_responses_handler.go`.
- [x] Rename `anthropic_handler.go` to `anthropic_messages_handler.go`.
- [x] Rename `codex_openai_chat.go` to `codex_responses_to_openai_chat.go`.
- [x] Rename `codex_anthropic_messages.go` to `codex_responses_to_anthropic_messages.go`.
- [x] Split `helpers.go` into narrower files:
  - logging/redaction helpers
  - dynamic value extraction helpers
- [x] Split Codex Responses handler support code into narrower files:
  - request JSON decoding helpers
  - Responses passthrough forwarding
  - Codex Responses stream/non-stream forwarding
  - Responses fallback detection
- [x] Split route data types from concrete route registry inside the gateway package.
- [x] Split logging support into structure redaction, usage formatting, and callback helpers.
- [x] Rename HTTP helper files so request decoding and JSON error responses are not confused with core request/error models.
- [x] Move e2e-style tests into clearly named files by route:
  - `codex_responses_to_openai_chat_e2e_test.go`
  - `anthropic_messages_to_openai_chat_e2e_test.go`
  - shared helpers in `gateway_e2e_helpers_test.go`

## Phase 3: Separate Core, Routes, and Features

Only do this after Phase 1 and Phase 2 are stable.

Candidate structure:

```text
internal/compat/gateway/
  core/
    route.go
    pipeline.go
    errors.go
    logging.go
  bridge/
    registry.go
    types.go
    translators.go
    stream.go
    nonstream.go
  features/
    reasoning/
      cache.go
      adapter.go
```

Before moving packages, check for circular imports:

- `core` should not import `bridge`.
- `features/reasoning` may import `policy`, but should avoid importing route handlers.
- `bridge` may import `core`, `client/*`, `target/*`, `policy`, and `ir`.
- `proxy` should import route/handler constructors but not policy internals.

Current pre-move status:

- [x] Move minimal core package to `internal/compat/gateway/core`.
- [x] `core/route.go` contains route data types, default normalization, and unsupported-route errors.
- [x] `route_registry.go` contains concrete route registration and writer type bindings.
- [x] `core/pipeline.go` no longer imports client protocol packages.
- [x] `StreamWriter` returns gateway-owned `RouteStreamResult` instead of `codex.ResponsesStreamResult`.
- [x] Move `RouteSelection` out of core route types; route core now owns route keys and errors only.
- [x] Move reasoning cache/adapter to `internal/compat/gateway/features/reasoning`.
- [x] Keep `features/reasoning` independent from route handlers and gateway package internals.
- [x] Move request conversion helpers and codec composition to `internal/compat/gateway/bridge`.
- [x] Move non-stream response bridging to `internal/compat/gateway/bridge`.
- [x] Move stream bridging to `internal/compat/gateway/bridge`.
- [x] Keep route conversion code independent from gateway HTTP handlers and response forwarding.
- [x] Move route registry and route selection types to `internal/compat/gateway/bridge`.
- [x] Add `ClientCodec` + `TargetCodec` composition so Spark adds O(N+M)
  protocol adapters instead of O(N*M) pairwise mappers.
- [x] Move OpenAI Chat response/stream fallback extraction helpers out of gateway root into `target/openai_chat`.
- [x] Move shared compat payload log redaction to `internal/compat/logutil`.
- [x] Move shared compat HTTP JSON request/error helpers to `internal/compat/httpjson`.

## Phase 4: Route Construction API

Once route handlers are split, introduce small constructor functions so proxy wiring stays simple.

- [x] Add `NewAnthropicMessagesToOpenAIChatHandler`.
- [x] Add `NewCodexResponsesHandler`.
- [x] Move proxy wiring to route constructors instead of direct handler field assembly.
- [x] Move route tests to constructors where route defaults are part of the behavior under test.
- [ ] Decide whether handler structs should remain directly constructible for narrowly scoped unit tests.

Example shape:

```go
gateway.NewAnthropicMessagesToOpenAIChatHandler(gateway.AnthropicMessagesOptions{
  UpstreamBase:        upstreamBase,
  PreferredModel:      preferredModel,
  ReasoningCache:      cache,
  PostChatCompletions: post,
  Logf:                logf,
})
```

Expected benefit:

- Proxy sees a route constructor, not internal handler fields.
- Gateway owns policy selection and feature orchestration.
- Tests can instantiate route handlers with explicit options.

## Non-Goals

- Do not split by provider/vendor as the primary boundary.
- Do not move `client/*` and `target/*` into gateway.
- Do not make proxy know model capability allowlists.
- Do not add new abstractions until the current handler boundaries are clean.

## Current State Snapshot

The current gateway is functionally sound but still relatively flat. The most important responsibility cleanup has already happened for reasoning:

- `reasoning_effort` selection is gateway/policy-driven.
- `reasoning_content` orchestration is gateway-driven.
- Proxy owns cache lifetime but no longer injects reasoning behavior callbacks.

Remaining cleanup should focus on shrinking the gateway root orchestration files
without moving HTTP handlers before their dependencies are ready.
