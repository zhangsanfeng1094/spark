---
source: ai
confidence: medium
role: usage-store
status: draft
updated_at: 2026-05-18
derived_from:
  - internal/usage
  - internal/app/cli.go
---

# Usage Store Policy

Role:
Files that persist and summarize token usage data.

Detected from:
- `internal/usage/**`
- App/TUI flow that displays token usage summaries.

Rules:
- Keep usage storage and aggregation independent from TUI presentation.
- Preserve stored record compatibility unless the task explicitly changes the format.
- Add tests for record format, aggregation, or summary behavior changes.

Non-rules:
- Display layout belongs in `internal/tui`.

Matching:
- `internal/usage/**`
- Tasks mentioning token usage storage, summaries, usage records, or aggregation.

Review notes:
- Watch for changes that alter historic usage totals.
