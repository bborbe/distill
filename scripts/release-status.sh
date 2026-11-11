#!/usr/bin/env bash
#
# Reports how far the Homebrew cask has fallen behind the latest tag.
#
# The cask is supposed to follow every tag: `github-releaser-agent` creates the
# GitHub Release and .github/workflows/release.yml pushes the cask from it. That
# did not happen between v0.6.0 and v0.9.7 — 31 tags, one Release, 18 versions of
# tap drift — and nothing announced it.
#
# This check is the canary for that shape: a cask behind the latest tag means the
# releaser stalled or the release workflow failed. It is diagnostic, not a gate:
# deliberately NOT wired into `make precommit`, and it exits non-zero only so it
# can run unattended (cron, a weekly task, CI) and alert on drift.
#
# Usage:
#   make release-status
#   bash scripts/release-status.sh
#
# Exit codes: 0 in sync · 1 cask behind · 2 could not determine (missing tool or network).

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

TAP_REPO=${TAP_REPO:-bborbe/homebrew-tap}
CASK_PATH="Casks/distill.rb"

report() {
  printf "  %-32s %s\n" "$1" "$2"
}

if ! command -v gh >/dev/null 2>&1; then
  echo "distill: release-status needs the 'gh' CLI on PATH" >&2
  exit 2
fi

if ! git fetch --tags --quiet 2>/dev/null; then
  echo "distill: release-status could not fetch tags — the count would be stale" >&2
  exit 2
fi

# Highest version tag, NOT `git describe --tags --abbrev=0`: describe resolves by
# reachability from HEAD, so on a checkout that is behind origin it reports an
# older tag and the check silently reads "0 behind" while a newer tag exists.
LATEST_TAG=$(git tag --sort=-v:refname | head -1)
if [ -z "$LATEST_TAG" ]; then
  echo "distill: release-status could not resolve the latest tag" >&2
  exit 2
fi

CASK_VERSION=$(
  gh api "repos/$TAP_REPO/contents/$CASK_PATH" --jq '.content' 2>/dev/null \
    | base64 -d 2>/dev/null \
    | grep -E '^\s*version' \
    | head -1 \
    | sed -E 's/.*"([^"]+)".*/\1/'
) || true

if [ -z "${CASK_VERSION:-}" ]; then
  echo "distill: release-status could not read $CASK_PATH from $TAP_REPO" >&2
  exit 2
fi

CASK_TAG="v$CASK_VERSION"
if [ "$CASK_TAG" = "$LATEST_TAG" ]; then
  BEHIND=0
elif git rev-parse -q --verify "refs/tags/$CASK_TAG" >/dev/null 2>&1; then
  # Count tags that sort after the cask's tag (version-sorted, so 0.9.7 > 0.10.0
  # is not an issue here — distill has never crossed a digit boundary mid-stream).
  BEHIND=$(git tag --sort=v:refname | awk -v t="$CASK_TAG" '$0 == t { found = 1; next } found { n++ } END { print n + 0 }')
else
  BEHIND="unknown"
fi

echo "distill release status"
report "latest tag" "$LATEST_TAG"
report "brew cask ($TAP_REPO)" "$CASK_VERSION"
report "versions behind" "$BEHIND"

if [ "$BEHIND" = "0" ]; then
  echo "✅ brew serves the latest tag"
  exit 0
fi

if [ "$BEHIND" = "unknown" ]; then
  echo "distill: release-status cannot place cask version $CASK_VERSION among the tags" >&2
  echo "    (cask tag $CASK_TAG is not a tag in this repo — the tap may point at a tag that was never pushed here)" >&2
  exit 2
fi

echo "⚠️  brew is $BEHIND version(s) behind $LATEST_TAG"
echo "    The cask follows every tag via the releaser + release workflow. If it has"
echo "    stopped: walk scenarios/*.md against a fresh build, then check why no"
echo "    Release was created — see docs/releasing-distill.md."
exit 1
