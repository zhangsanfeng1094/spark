#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'EOF'
Usage:
  scripts/release-npm.sh <patch|minor|major|prerelease|x.y.z> [--push]

Examples:
  scripts/release-npm.sh patch
  scripts/release-npm.sh minor --push
  scripts/release-npm.sh 0.2.0 --push
EOF
}

if [ "${1:-}" = "" ] || [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

BUMP="$1"
PUSH_FLAG="${2:-}"

if [ "$PUSH_FLAG" != "" ] && [ "$PUSH_FLAG" != "--push" ]; then
  echo "Unsupported flag: $PUSH_FLAG"
  usage
  exit 1
fi

CURRENT_BRANCH="$(git branch --show-current)"
if [ "$CURRENT_BRANCH" != "main" ]; then
  echo "Current branch is '$CURRENT_BRANCH'. Switch to 'main' before release."
  exit 1
fi

if [ -n "$(git status --porcelain)" ]; then
  echo "Working tree is not clean. Commit or stash changes before release."
  exit 1
fi

echo "Bumping npm package version with: $BUMP"
npm version "$BUMP" --prefix npm -m "chore(release): v%s"

NEW_VERSION="$(node -p "require('./npm/package.json').version")"
echo "Created commit and tag: v$NEW_VERSION"

if [ "$PUSH_FLAG" = "--push" ]; then
  git push origin main --follow-tags
  echo "Pushed main and tag v$NEW_VERSION."
else
  echo "Run this to trigger GitHub Actions release:"
  echo "  git push origin main --follow-tags"
fi
