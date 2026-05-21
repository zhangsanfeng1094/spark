---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/usage
  - internal/app/cli.go
---

# Usage Summary

Purpose:
`internal/usage` persists token usage data and produces summaries consumed by CLI/TUI workflows.

Collaborators:
- `internal/app` loads summaries for interactive token usage display.
- `internal/tui` renders usage views.

Tests:
Existing usage tests cover store behavior. Add focused tests when changing record shape or aggregation semantics.

Unclear or inferred:
Exact retention or migration policy was not exhaustively reviewed during initialization.
