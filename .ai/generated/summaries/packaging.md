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
  - release-please-config.json
---

# Packaging Summary

Purpose:
Packaging files distribute the Go `spark` binary through GitHub releases and npm packages.

Main areas:
- `.goreleaser.yml`: builds platform binaries.
- `.github/workflows/release.yml`: validates, releases binaries, and publishes packages.
- `release-please-config.json`: coordinates release PR/version updates.
- `npm/package.json`: main npm package metadata and optional platform dependencies.
- `npm/bin/spark.js`: locates and executes the installed platform binary.
- `npm/scripts/build-packages.js`: generates platform packages.

Tests:
Run Go tests for release validation assumptions and package-generation checks when changing npm scripts or binary naming.

Unclear or inferred:
Package publication behavior depends on external release workflow secrets and registry state.
