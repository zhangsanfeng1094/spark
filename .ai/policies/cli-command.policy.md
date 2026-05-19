---
source: ai
confidence: medium
role: cli-command
status: draft
updated_at: 2026-05-18
derived_from:
  - internal/app/cli.go
  - internal/app/mcp_cmd.go
  - docs/internal-architecture.md
---

# CLI Command Policy

Role:
Files that define `spark` commands and orchestrate user workflows.

Detected from:
- `internal/app` Cobra command files.
- `cmd/spark/main.go` delegating to `app.NewRootCmd()`.

Rules:
- Keep command parsing, prompt orchestration, and handoff to config/integration/TUI packages in `internal/app`.
- Preserve `spark launch ... -- ...` pass-through semantics.
- Prefer existing `tui` helpers for interactive selection and confirmation.

Non-rules:
- Do not infer protocol conversion behavior from CLI code alone.

Matching:
- `internal/app/**`
- `cmd/spark/main.go`
- Tasks mentioning launch/config/profile/MCP/skill/debug/version commands.

Review notes:
- Broaden tests when a command changes persisted config, TUI entry points, or integration launch behavior.
