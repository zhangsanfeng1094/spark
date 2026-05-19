---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - README.md
  - docs/internal-architecture.md
  - go list ./...
---

# Architecture Map

Spark is a Go CLI with a small executable entry point and several internal boundaries:

```text
cmd/spark
  -> internal/app
       -> internal/config
       -> internal/integrations
       -> internal/skills
       -> internal/tui
       -> internal/usage
       -> internal/version

internal/integrations
  -> internal/compat/gateway
       -> internal/compat/client/*
       -> internal/compat/target/openai_chat
            -> internal/compat/ir
            -> internal/compat/policy
```

Main runtime flows:
- Interactive or command-line launch begins in `internal/app`.
- Config/profile/MCP persistence goes through `internal/config`.
- Agent-specific setup and launch goes through `internal/integrations`.
- Protocol compatibility uses `internal/compat` to translate client protocols through IR to OpenAI Chat.
- TUI workflows live under `internal/tui`.
- Skill management lives under `internal/skills`.
- NPM distribution wraps released Go binaries under `npm`.

Primary risks:
- Compatibility streaming behavior and usage accounting.
- Config round-trip behavior across Spark, Codex, and Claude formats.
- Release package naming alignment between GoReleaser and npm wrapper scripts.

Staleness triggers:
- New internal package.
- Moved compatibility pipeline responsibilities.
- Changed release artifact naming.
- Changed config schema or peer sync behavior.
