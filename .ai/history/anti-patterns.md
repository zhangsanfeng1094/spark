---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - skills/ai-native-project-skeleton/SKILL.md
  - docs/internal-architecture.md
---

# Anti-Patterns

Avoid:
- Creating `.agent.md` files in every directory.
- Treating `.ai/` summaries as more authoritative than code and tests.
- Copying protocol conversion rules into each integration runner.
- Letting config packages depend on CLI or TUI packages.
- Hiding reasoning or tool-call policy inside generic stream helpers.
- Changing release package names without checking GoReleaser, npm wrapper, and optional dependency metadata together.

Known risk:
- Compatibility migration docs may mix current and intended architecture. Confirm current code before applying future-facing guidance.
