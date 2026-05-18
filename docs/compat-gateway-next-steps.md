# Compatibility Gateway Current Status and Next TODO

## Current Status

The compatibility gateway has been reshaped around a clear middle-layer
architecture:

```text
caller protocol
  -> compat client adapter
  -> compat/ir request
  -> compat target adapter
  -> configured upstream endpoint
  -> compat target stream/response parser
  -> compat/ir stream events or response
  -> compat client response writer
  -> caller protocol response
```

The primary implemented path is:

```text
Codex Responses
  -> internal/compat/client/codex
  -> internal/compat/ir
  -> internal/compat/target/openai_chat
  -> OpenAI Chat Completions compatible upstream
  -> internal/compat/ir
  -> internal/compat/client/codex
```

Streaming is the preferred path. Non-streaming follows the same adapter and IR
boundaries, but converts complete upstream JSON into a complete client response.

## What Changed

- Added architecture documentation in `docs/compat-gateway-architecture.md`.
- Added completed migration checklist in `docs/compat-gateway-todo.md`.
- Moved the provider-neutral middle layer from `internal/compatir` to
  `internal/compat/ir`.
- Moved Codex/OpenAI Responses client protocol handling into
  `internal/compat/client/codex`.
- Moved OpenAI Chat Completions upstream handling into
  `internal/compat/target/openai_chat`.
- Added `internal/compat/gateway` for route selection, upstream execution, and
  stream/non-stream orchestration.
- Kept `internal/integrations` focused on service startup, endpoint wiring,
  upstream HTTP calls, retry behavior, logging, and local compatibility state.
- Added typed reasoning controls in `ir.GenerationConfig.Reasoning`.
- Mapped request-side reasoning controls from Codex/OpenAI Responses,
  Anthropic, and Gemini into IR.
- Removed business use of `Generation.Raw` for known reasoning controls.
- Added target-side tests that unsupported reasoning controls are not leaked to
  OpenAI Chat request payloads.

## Current Boundaries

`internal/compat/ir` owns the gateway's shared request, response, stream,
content, tool, usage, stop reason, generation, and reasoning concepts.

`internal/compat/client/*` owns caller-facing protocol details. A client adapter
may parse caller JSON into IR and write IR back to caller JSON or SSE. It must
not know which upstream endpoint is configured.

`internal/compat/target/*` owns upstream API details. A target adapter may build
upstream JSON from IR and parse upstream JSON or SSE back into IR. It must not
know which client called the gateway.

`internal/compat/gateway` owns orchestration only: method validation, request
decode, client-to-target pipeline selection, upstream execution, fallback
choice, stream/non-stream forwarding, and gateway-level errors.

`internal/integrations` owns runtime integration only: proxy startup, configured
base URLs and keys, HTTP clients, retry policy, logs, and local state that is
not protocol mapping.

## Validation

The current tree passes:

```bash
env GOCACHE=/tmp/spark-go-build go test ./...
```

## Next TODO

### Phase 1: Commit-Ready Cleanup

- [ ] Review `git status --short` and make sure all file renames are staged as
  intentional moves, not delete/add accidents.
- [ ] Review generated diffs for `docs/compat-ir-migration-plan.md`,
  `docs/compat-proxy-architecture.md`, and `docs/internal-architecture.md`;
  keep only updates that match the new gateway design.
- [ ] Run `rg` checks before commit:
  - [ ] No runtime imports of `internal/compatir`.
  - [ ] No runtime imports of `internal/compat/codec/openai`.
  - [ ] No runtime imports of `internal/compat/target/openai`.
  - [ ] No protocol field mapping added back into `internal/integrations`.
- [ ] Run full tests again before commit.

### Phase 2: Codex Stream Hardening

- [x] Add focused tests for Codex Responses stream output event ordering:
  `response.created`, reasoning deltas, text deltas, tool call deltas, usage,
  and `response.completed`.
- [x] Add tests for upstream stream edge cases:
  empty stream, malformed SSE line, `[DONE]` without content, usage-only chunk,
  and tool-call arguments split across multiple chunks.
- [x] Confirm stream fallback behavior is explicit when upstream Chat
  Completions fails before the first valid chunk.
- [x] Keep all stream conversion through:
  `target/openai_chat -> ir.StreamEvent -> client/codex`.

### Phase 3: Reasoning Policy

- [x] Decide whether OpenAI Chat targets should always allow
  `reasoning_effort`, or whether it should be model/provider gated.
- [x] Add policy tests for provider-specific reasoning degradation:
  OpenAI-compatible generic, DeepSeek/MiMo-style reasoning echo, and providers
  that reject unknown top-level fields.
- [x] Make unsupported reasoning control behavior observable in logs without
  leaking full prompt content.
- [x] Keep all known reasoning request controls in `ir.GenerationConfig.Reasoning`;
  use `Generation.Raw` only as fallback storage for unknown fields.

### Phase 4: Anthropic and Gemini Naming Cleanup

- [x] Decide whether existing `internal/compat/codec/anthropic` should become:
  - `internal/compat/client/anthropic_messages` when serving Anthropic callers.
  - `internal/compat/target/anthropic_messages` when calling Anthropic upstreams.
- [x] Decide whether existing `internal/compat/codec/gemini` should become:
  - `internal/compat/client/gemini_generate_content` when serving Gemini callers.
  - `internal/compat/target/gemini_generate_content` when calling Gemini upstreams.
- [x] Do not move both directions into one package. Keep caller protocol and
  upstream target protocol separate.
- [x] Move only after the Codex/OpenAI Chat path remains stable.

### Phase 5: Multi-Endpoint Route Selection

- [x] Add an explicit route/config model for selecting target API type:
  `openai_chat`, future `openai_responses`, future `anthropic_messages`, future
  `gemini_generate_content`.
- [x] Keep route selection in `internal/compat/gateway`; keep endpoint config
  loading in the existing config/integration layer.
- [x] Add tests that Codex caller + OpenAI Chat target selects the expected
  translator and stream writer.
- [x] Add tests that unsupported caller/target combinations fail with a clear
  gateway error.

### Phase 6: Documentation

- [x] Update `docs/internal-architecture.md` with the final compat package
  diagram.
- [x] Update old proxy docs so they describe the new `client -> ir -> target`
  shape and do not mention obsolete package names.
- [x] Add one concrete request/response trace for Codex stream mode.
- [x] Add one concrete request/response trace for Codex non-stream mode.
