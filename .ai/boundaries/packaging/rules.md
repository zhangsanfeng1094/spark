---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - npm/package.json
  - npm/bin/spark.js
  - npm/scripts/build-packages.js
  - .goreleaser.yml
  - .github/workflows/release.yml
---

# Packaging Rules

Purpose:
Owns release artifacts, npm wrapper behavior, and platform package generation.

Stable constraints:
- GoReleaser binary naming must match npm wrapper expectations.
- Optional platform dependencies must resolve to packages containing the vendor binary.
- Release Please version updates must stay aligned with npm metadata.

Dependency constraints:
- Packaging scripts should not define Go runtime behavior.

Vocabulary:
- `main package`: `@ngominhbinh708/spark`.
- `platform package`: optional dependency containing one OS/architecture binary.
- `wrapper`: `npm/bin/spark.js`, which locates and executes the platform binary.

Open questions:
- Confirm package naming before changing npm scope or binary layout.
