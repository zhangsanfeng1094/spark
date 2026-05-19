---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/usage
  - internal/app/cli.go
---

# Usage Rules

Purpose:
Owns token usage persistence and summaries.

Stable constraints:
- Keep stored usage records compatible unless a migration is deliberate.
- Keep aggregation logic testable outside TUI rendering.

Dependency constraints:
- Should not depend on CLI or TUI packages.

Vocabulary:
- `usage record`: persisted token usage event or entry.
- `summary`: aggregated usage data shown by app/TUI workflows.

Open questions:
- Confirm migration behavior before changing persisted usage format.
