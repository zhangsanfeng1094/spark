---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - docs/internal-architecture.md
  - existing repository layout
---

# Recurring Patterns

- Thin executable entry point delegates to an internal package.
- Cobra commands orchestrate workflows and call lower-level packages.
- Config format handling is centralized under `internal/config`.
- Agent-specific launch/config code is separated from reusable compatibility translation.
- Compatibility adapters translate through a shared IR where current code supports that boundary.
- TUI views use focused model/view/update files and tests.
- Release packaging keeps Go binary production and npm wrapper behavior coupled through metadata.

Review triggers:
- A new feature crosses more than one boundary.
- A package begins to own both UI and persistence.
- A protocol change bypasses IR or policy.
