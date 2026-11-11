#!/usr/bin/env bash
#
# Verifies distill's single version surface is aligned. Exits non-zero on a
# mismatch with a clear report.
#
# distill is binary-only, so it has ONE version surface, unlike vault-cli's four:
#   1. CHANGELOG.md   — top "## vX.Y.Z" entry
#   2. the latest git tag
#
# The two must agree at release time. This is NOT wired into `make precommit`:
# every commit advances `## Unreleased`, so a precommit-time check would force a
# release number onto ordinary refactors. Run it via `make check-versions` or
# `make release-check` before tagging.
#
# Run from repo root (Makefile target `check-versions`).

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

CHANGELOG_VERSION=$(grep -m1 '^## v' CHANGELOG.md 2>/dev/null | sed 's/^## v//' || true)
LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "none")
LATEST_TAG_VERSION=${LATEST_TAG#v}

if [ -z "$CHANGELOG_VERSION" ]; then
  echo "❌ CHANGELOG.md has no '## vX.Y.Z' section — nothing to align against" >&2
  echo "    Every released version needs its section; the releaser rewrites ## Unreleased in place." >&2
  exit 1
fi

report() {
  printf "  %-44s %s\n" "$1" "$2"
}

echo "Version alignment check"
report "CHANGELOG.md (top ## vX.Y.Z)" "$CHANGELOG_VERSION"
report "latest git tag" "$LATEST_TAG"

if [ "$LATEST_TAG" = "none" ]; then
  echo "❌ no git tag found — nothing to compare against"
  exit 1
fi

if [ "$CHANGELOG_VERSION" = "$LATEST_TAG_VERSION" ]; then
  echo "✅ CHANGELOG top section matches the latest tag: v$CHANGELOG_VERSION"
  exit 0
else
  echo "❌ version mismatch: CHANGELOG says v$CHANGELOG_VERSION, latest tag is $LATEST_TAG"
  echo "    fix: the releaser rewrites ## Unreleased → ## vX.Y.Z and tags in the same commit."
  echo "         A mismatch means one half landed without the other — investigate before releasing."
  exit 1
fi
