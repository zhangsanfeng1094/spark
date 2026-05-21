---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/skills
  - skills/tui-state-navigator/SKILL.md
  - skills/ai-native-project-skeleton/SKILL.md
---

# Skills Rules

Purpose:
Owns local skill discovery, parsing, installation, registry, and peer synchronization.

Stable constraints:
- Skill metadata starts at `SKILL.md`.
- Agent-specific metadata can live under `agents/`.
- Registry behavior should remain deterministic and testable.

Dependency constraints:
- Skill system should not depend on TUI view internals.
- Skill prose should not override code behavior.

Vocabulary:
- `skill root`: a directory searched for installed skills.
- `manifest`: parsed skill metadata.
- `peer`: another agent's skill storage or export/import format.

Open questions:
- Peer-specific behavior should be documented before broadening sync support.
