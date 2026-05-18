# Compatibility Gateway Handoff

## Current State

The compat gateway migration is implemented through Phase 6 of
`docs/compat-gateway-next-steps.md`.

The intended layering is now:

```text
caller protocol
  -> internal/compat/client/*
  -> internal/compat/ir
  -> internal/compat/target/*
  -> internal/compat/gateway
  -> internal/integrations runtime shell
```

`internal/integrations` should now stay focused on agent startup and configuration
wiring. Local proxy runtime lives under `internal/integrations/compatproxy`;
shared proxy logging/body utilities live under `internal/integrations/proxyutil`.
Protocol field conversion should live under `internal/compat`.

## Important Completed Work

- Moved caller protocol adapters:
  - Codex Responses: `internal/compat/client/codex`
  - Anthropic Messages: `internal/compat/client/anthropic_messages`
  - Gemini GenerateContent: `internal/compat/client/gemini_generate_content`
- Moved OpenAI Chat target adapter to `internal/compat/target/openai_chat`.
- Added/expanded `internal/compat/gateway` for route selection, stream and
  non-stream orchestration, gateway errors, chat extraction helpers, and
  reasoning cache handling.
- Added explicit route selection for `codex_responses -> openai_chat`.
- Added provider/model-gated reasoning policy for OpenAI Chat targets.
- Hardened Codex Responses stream conversion edge cases.
- Removed `internal/integrations/compat_chat_extractors.go`.
- Removed obsolete `anthropicChatExecutor` and `forwardAnthropicStream`
  integration wrappers.
- Moved local proxy runtime from the `internal/integrations` root package to
  `internal/integrations/compatproxy`.
- Moved shared proxy utilities to `internal/integrations/proxyutil`.

## Current Validation

The latest validation passed:

```bash
go test ./...
```

Runtime scans also passed:

```bash
rg -n '"spark/internal/compatir|"spark/internal/compat/codec/|"spark/internal/compat/target/openai"' internal --glob '*.go'
rg -n 'reasoning_content|tool_calls|ChatResponse|ChatStreamEvents|MessagesClientResponse|WriteMessagesStream|ResponsesInbound|ChatOutbound' internal/integrations --glob '*.go' --glob '!*_test.go'
```

Both scans should produce no output for production integration code.

## Files To Review Before Commit

The worktree is intentionally dirty from the migration. Before committing,
review rename staging carefully. The current focused dirty set includes:

- `docs/compat-proxy-architecture.md`
- `docs/internal-architecture.md`
- `internal/compat/gateway/reasoning_cache.go`
- `internal/integrations/compatproxy/claude_compat_proxy.go`
- `internal/integrations/compatproxy/claude_compat_proxy_test.go`
- `internal/integrations/compatproxy/codex_compat_proxy.go`
- `internal/integrations/compatproxy/codex_compat_proxy_test.go`
- deleted `internal/integrations/compat_chat_extractors.go`
- `internal/integrations/compatproxy/compat_executors.go`
- `internal/integrations/compatproxy/compat_helpers.go`
- `internal/integrations/compatproxy/compat_test_helpers_test.go`
- `internal/integrations/proxyutil/compatio.go`

There are also broader compat migration changes in the worktree from earlier
steps, including client/target package moves and docs updates. Use
`git status --short` and stage renames intentionally.

## Remaining Work

- Review `git status --short` after staging to ensure package moves are shown as
  intentional renames where possible.
- Review docs diffs for consistency:
  - `docs/compat-gateway-architecture.md`
  - `docs/compat-gateway-next-steps.md`
  - `docs/compat-gateway-todo.md`
  - `docs/compat-ir-migration-plan.md`
  - `docs/compat-proxy-architecture.md`
  - `docs/internal-architecture.md`
- Run `go test ./...` again after staging or any conflict resolution.

## Boundary Notes

- `internal/compat/policy` owns decisions about retaining, dropping, or
  degrading reasoning/tool behavior.
- `internal/compat/gateway/reasoning_cache.go` owns reasoning echo/cache logic
  for chat tool-call compatibility.
- `internal/integrations/claude_compat_reasoning.go` is now only a thin bridge
  to `gateway.ReasoningCache`.
- Tests under `internal/integrations` may still assert protocol field names, but
  production integration code should not perform protocol mapping.
