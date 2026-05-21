---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/skills
  - skills/tui-state-navigator/SKILL.md
  - skills/ai-native-project-skeleton/SKILL.md
---

# Skills Summary

Purpose:
`internal/skills` manages skill roots, registry, manifests, catalog/install behavior, files, and peer synchronization. Repository skills live under `skills/`.

Main areas:
- Skill discovery and manifest parsing.
- Root resolution and local file handling.
- Catalog and install workflows.
- Peer import/export/sync behavior.

Tests:
Existing skill tests cover registry, catalog, and peer behavior. Add focused tests for parser or install behavior changes.

Unclear or inferred:
Agent-specific skill compatibility should be confirmed per peer before broadening sync support.
