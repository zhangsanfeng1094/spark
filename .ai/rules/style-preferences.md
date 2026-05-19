---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - existing Go source
  - README.md
  - docs/internal-architecture.md
---

# Style Preferences

Go:
- Follow existing package-level helper style before adding new abstractions.
- Keep exported names small and purposeful; most packages expose only what adjacent boundaries need.
- Prefer table tests where existing tests already use them.
- Keep compatibility tests focused on protocol behavior, stream order, usage, reasoning, and tool-call details.

Docs and metadata:
- Keep `.agent.md` anchors short and stable.
- Label generated `.ai/` material with source, confidence, update date, and derivation.
- Do not duplicate full code behavior in prose.

CLI/TUI:
- Preserve direct command examples and pass-through argument behavior.
- Keep user-facing terminal copy concise.
