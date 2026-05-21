---
source: ai
confidence: medium
role: release-packaging
status: draft
updated_at: 2026-05-18
derived_from:
  - npm/package.json
  - npm/bin/spark.js
  - npm/scripts/build-packages.js
  - .goreleaser.yml
  - .github/workflows/release.yml
  - release-please-config.json
---

# Release Packaging Policy

Role:
Files that package Spark binaries, publish npm packages, and coordinate release automation.

Detected from:
- GoReleaser config.
- Release Please config and workflows.
- npm package wrapper and platform package generator.

Rules:
- Keep Go binary name, npm wrapper expectations, and platform package names aligned.
- Keep version updates compatible with Release Please and generated package metadata.
- Preserve platform-specific optional dependency behavior.

Non-rules:
- Packaging metadata does not define runtime CLI behavior.

Matching:
- `npm/**`
- `.goreleaser.yml`
- `.github/workflows/release*.yml`
- `release-please-config.json`
- `.release-please-manifest.json`

Review notes:
- Verify package generation separately when changing platform naming or binary lookup.
