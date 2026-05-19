---
source: ai
confidence: medium
role: skill-system
status: draft
updated_at: 2026-05-18
derived_from:
  - internal/skills
  - skills/tui-state-navigator/SKILL.md
  - skills/ai-native-project-skeleton/SKILL.md
---

# Skill System Policy

Role:
Files that define skill metadata, discovery, installation, registry behavior, and peer skill sync.

Detected from:
- `internal/skills/**`
- `skills/**/SKILL.md`
- `skills/**/agents/*.yaml`

Rules:
- Treat `SKILL.md` as operational instructions with frontmatter metadata.
- Keep registry and manifest parsing deterministic.
- Preserve local root and peer-agent root semantics.
- Avoid assuming every peer agent supports identical skill packaging.

Non-rules:
- Skill prose should not override repository code behavior.

Matching:
- `internal/skills/**`
- `skills/**`
- Tasks mentioning skill install, registry, catalog, manifest, peer sync.

Review notes:
- Add focused tests for parser, registry, install, or peer sync changes.
