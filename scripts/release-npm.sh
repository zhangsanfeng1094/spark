#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'EOF'
Usage:
  scripts/release-npm.sh [patch|minor|major|prerelease|x.y.z] [--push]
  scripts/release-npm.sh

When no version argument is provided, the script enters interactive mode.

Examples:
  scripts/release-npm.sh patch
  scripts/release-npm.sh minor --push
  scripts/release-npm.sh 0.2.0 --push
  scripts/release-npm.sh
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

BUMP="${1:-}"
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

CURRENT_VERSION="$(node -p "require('./npm/package.json').version")"
echo "Current npm version: $CURRENT_VERSION"

choose_bump_interactive() {
  if [ ! -t 0 ]; then
    echo "No bump type provided and stdin is not interactive."
    echo "Use one of: patch | minor | major | prerelease | x.y.z"
    exit 1
  fi

  cat <<'EOF'
Select release type:
  1) patch
  2) minor
  3) major
  4) prerelease
  5) custom version
EOF
  read -r -p "Choose [1-5]: " choice
  case "$choice" in
    1) BUMP="patch" ;;
    2) BUMP="minor" ;;
    3) BUMP="major" ;;
    4) BUMP="prerelease" ;;
    5)
      read -r -p "Enter version (x.y.z): " custom_version
      custom_version="${custom_version// /}"
      if [ -z "$custom_version" ]; then
        echo "Version cannot be empty."
        exit 1
      fi
      BUMP="$custom_version"
      ;;
    *)
      echo "Invalid choice: $choice"
      exit 1
      ;;
  esac
}

if [ -z "$BUMP" ]; then
  choose_bump_interactive
fi

echo "Bumping npm package version with: $BUMP"
npm version "$BUMP" --prefix npm -m "chore(release): v%s"

NEW_VERSION="$(node -p "require('./npm/package.json').version")"
TAG="v$NEW_VERSION"

echo "Updated npm/package.json to: $NEW_VERSION"
echo "Created release commit and tag: $TAG"

if [ "$PUSH_FLAG" = "--push" ]; then
  git push origin main --follow-tags
  echo "Pushed main and tag $TAG."
else
  if [ -t 0 ]; then
    read -r -p "Push main and tag now? [y/N]: " push_now
    case "$push_now" in
      y|Y|yes|YES)
        git push origin main --follow-tags
        echo "Pushed main and tag $TAG."
        ;;
      *)
        echo "Push skipped."
        echo "Run this when ready:"
        echo "  git push origin main --follow-tags"
        ;;
    esac
  else
    echo "Run this to trigger GitHub Actions release:"
    echo "  git push origin main --follow-tags"
  fi
fi
