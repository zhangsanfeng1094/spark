---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - cmd/spark/main.go
  - internal/app
  - README.md
---

# CLI Summary

Purpose:
`cmd/spark` delegates to `internal/app`, where Cobra commands implement interactive mode and subcommands for launch, config, MCP, skill, profile, debug, and version flows.

Entry points:
- `app.NewRootCmd()`
- `spark launch`
- `spark config`
- `spark mcp`
- `spark skill`
- `spark profile`

Collaborators:
- `internal/config` for persisted profiles and MCP state.
- `internal/integrations` for agent runners/editors.
- `internal/tui` for interactive prompts and managers.
- `internal/skills` for skill commands.
- `internal/usage` for token usage summaries.

Tests:
Check existing `internal/app` tests before changing command behavior; broaden coverage when command parsing, persistence, or launch flow changes.

Unclear or inferred:
Exact command test coverage was not exhaustively mapped in this initialization.
