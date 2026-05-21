---
source: ai
confidence: medium
role: compat-translation
status: draft
updated_at: 2026-05-18
derived_from:
  - internal/compat
  - docs/internal-architecture.md
  - docs/compat-ir-migration-plan.md
  - docs/compat-proxy-architecture.md
---

# Compatibility Translation Policy

Role:
Files that translate between client protocols, shared IR, policy rules, gateway routing, and OpenAI Chat target protocol.

Detected from:
- `internal/compat/client/*`
- `internal/compat/ir`
- `internal/compat/policy`
- `internal/compat/target/openai_chat`
- `internal/compat/gateway`

Rules:
- Preserve tool call IDs and tool result relationships.
- Keep reasoning behavior explicit and policy-controlled.
- Keep stream event ordering and usage aggregation covered by tests.
- Use IR as the shared model between client-specific and target-specific adapters.

Non-rules:
- Migration docs can describe intended future shape; verify current code before applying them.

Matching:
- `internal/compat/**`
- Tasks mentioning Responses, Anthropic Messages, Gemini GenerateContent, OpenAI Chat, IR, reasoning, streaming, tools, gateway.

Review notes:
- Run targeted compatibility tests and `go test ./...` after changes with protocol impact.
