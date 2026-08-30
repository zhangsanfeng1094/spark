# Compatibility Gateway TODO

## Phase 1: Directory Clarity

- [x] Move `internal/compatir` to `internal/compat/ir`.
- [x] Move Codex/OpenAI Responses client protocol code to `internal/compat/client/codex`.
- [x] Move OpenAI Chat Completions target code to `internal/compat/target/openai_chat`.
- [x] Update imports and tests.
- [x] Keep logic changes minimal during the move.

## Phase 2: Main Codex -> OpenAI Chat Path

- [x] Make the request path explicit:
  `client/codex -> ir.Request -> target/openai_chat`.
- [x] Make the stream response path explicit:
  `target/openai_chat -> ir.StreamEvent -> client/codex`.
- [x] Make the non-stream response path explicit:
  `target/openai_chat -> ir.Response -> client/codex`.
- [x] Keep stream as the preferred path in gateway orchestration.

## Phase 3: Gateway Package

- [x] Move request translation pipeline interfaces into `internal/compat/gateway`.
- [x] Move Codex Responses -> OpenAI Chat request orchestration into gateway.
- [x] Move OpenAI Chat -> Codex Responses stream/non-stream orchestration into gateway.
- [x] Extract HTTP handlers from `internal/integrations/*_compat_*` into
  `internal/compat/gateway`.
- [x] Keep gateway code limited to route selection, upstream execution, and
  stream/non-stream orchestration.
- [x] Leave protocol field mapping in client and target packages only.

## Phase 4: Reasoning Controls

- [x] Keep request-side reasoning controls in `ir.GenerationConfig.Reasoning`.
- [x] Remove remaining business use of `Generation.Raw` for known reasoning
  controls.
- [x] Add target policy tests for unsupported reasoning controls.

## Phase 5: Follow-on Targets

- [x] Anthropic Messages exists as client (`client/anthropic_messages`) and
  target (`target/anthropic_messages`); live local proxy route is Claude
  Messages → OpenAI Chat (and related gateway handlers).
- [x] Gemini GenerateContent exists as client codec under
  `client/gemini_generate_content` (profile/probe); **no** local HTTP proxy
  route yet.
- [ ] Optional: Gemini as a full gateway client/target route if product needs it.
- [x] Codex/OpenAI Chat path is the primary clean route; keep adding contract
  tests for reasoning policy (GLM drop effort, DeepSeek echo, etc.).
