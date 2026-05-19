---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - skills/ai-native-project-skeleton/SKILL.md
  - skills/ai-native-project-skeleton/runtime/context-resolver.md
  - skills/ai-native-project-skeleton/runtime/context-assembly.md
---

# AI Behavior Rules

Context priority:
1. Current task and touched files.
2. Nearest `.agent.md`.
3. Matching policy under `.ai/policies/`.
4. Boundary rules under `.ai/boundaries/`.
5. Repository rules under `.ai/rules/`.
6. Generated summaries and selected index slices under `.ai/generated/`.

Behavior:
- Treat code and tests as authoritative.
- If `.ai/` metadata conflicts with code, report the mismatch and follow code.
- Load only the smallest relevant context for the task.
- Do not load raw generated artifacts or full graphs into prompt context.
- Refresh generated summaries or mark them stale after architecture-moving changes.
- Prefer boundary-local guidance over root guidance when they conflict.
