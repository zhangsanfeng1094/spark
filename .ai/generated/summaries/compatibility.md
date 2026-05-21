---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/compat
  - docs/internal-architecture.md
  - docs/compat-ir-migration-plan.md
  - docs/compat-proxy-architecture.md
---

# Compatibility Summary

Purpose:
`internal/compat` translates caller protocols into a shared IR, applies policies, maps to OpenAI Chat Completions, and writes responses or stream events back in caller-specific formats.

Main areas:
- `client/codex`: OpenAI Responses-facing adapter behavior.
- `client/anthropic_messages`: Anthropic Messages-facing adapter behavior.
- `client/gemini_generate_content`: Gemini GenerateContent inbound behavior.
- `ir`: shared request/message/content/tool/reasoning/usage/stream model.
- `policy`: reasoning and tool compatibility behavior.
- `target/openai_chat`: OpenAI Chat request/response/stream target mapping.
- `gateway`: HTTP route selection, handlers, upstream execution, stream and non-stream forwarding.

Tests:
Compatibility tests are behavior-sensitive. Run targeted tests for stream order, usage, reasoning, tool calls/results, and gateway logs after protocol changes.

Unclear or inferred:
Migration docs include future-facing package names and phases; current code remains authoritative.
