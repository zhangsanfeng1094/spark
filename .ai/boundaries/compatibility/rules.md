---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/compat
  - docs/internal-architecture.md
  - docs/compat-ir-migration-plan.md
---

# Compatibility Rules

Purpose:
Owns protocol conversion and compatibility gateway semantics.

Stable constraints:
- Client-specific packages convert to/from `internal/compat/ir`.
- Target packages convert between IR and upstream provider protocols.
- Policy packages own reasoning and tool compatibility behavior.
- Gateway code owns HTTP routing, handlers, upstream execution, and stream/non-stream forwarding.

Dependency constraints:
- `ir` must stay independent of clients, targets, gateway, integrations, and app.
- Policy should depend on IR, not concrete HTTP gateway types.
- Gateway may coordinate clients and targets but should not hide provider-specific rules inside generic helpers.

Vocabulary:
- `IR`: shared representation for requests, messages, content blocks, tool calls/results, responses, stream events, and usage.
- `client`: adapter for a caller protocol such as Codex Responses or Anthropic Messages.
- `target`: adapter for an upstream protocol such as OpenAI Chat Completions.

Open questions:
- Migration docs mention future target packages beyond OpenAI Chat; verify current code before adding new packages.
