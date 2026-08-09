# Releasing distill

How to ship a new version of `distill`. Mandatory reading before every `make install`.

`distill` is a **binary-only** tool — one artifact, one version stream. There is no
Claude plugin surface (no `.claude-plugin/` JSONs to align, unlike vault-cli).

## One surface, one version stream

| Surface | Versioned by | Consumed by | Bumped how |
|---------|--------------|-------------|------------|
| **Binary** | git tag `vX.Y.Z` + matching `## vX.Y.Z` section in `CHANGELOG.md` | `~/.claude/Makefile` + vault `.claude/Makefile` via `go install github.com/bborbe/distill@latest` | Auto-tagged by `github-releaser-agent` after `## Unreleased` bullets merge to `master` |

## The release gate (run BEFORE every `make install`)

`make precommit` does NOT exercise the real `claude` subprocess boundary — the exact
place the compile pipeline broke (rule bodies hijacking `claude --print`). Unit tests
pass with a mocked Runner while the live invocation is wrong. So before every install:

**Walk `scenarios/*.md` against a freshly built binary.**

```bash
# 1. Build a fresh binary (NOT the installed one)
go build -C ~/Documents/workspaces/distill -o /tmp/new-distill .

# 2. Walk each scenario against /tmp/new-distill
ls scenarios/*.md   # 001-compile-anti-injection.md (+ any later ones)
```

`scenarios/001-compile-anti-injection.md` is the load-bearing one: it drives real
`claude` with a hijack-bait rule + a planted `CLAUDE.md` and asserts the output has
proper bullets, zero `No task request` junk, and no ambient-CLAUDE.md leak. It is an
LLM-driving scenario (~30 s–2 min, real token cost) — babysit it; do not run unattended.

If any scenario fails: do **not** install. Fix the regression first, then rerun the gate.

### When the diff is empty (the one valid skip)

Nothing on the binary surface changed since the installed binary:

```bash
INSTALLED=$(distill --version 2>/dev/null | awk '{print $NF}')   # if/when --version exists
git diff "$INSTALLED"..HEAD --name-only | grep -E '\.(go|mod|sum)$|^Makefile$|pkg/distill/system.md$'
# empty output → installed binary is behaviorally equivalent → skip the gate
```

Note `pkg/distill/system.md` is in the skip check: it is embedded into the binary and
changes compile behavior, so a `system.md` edit is a binary-surface change.

## Binary release — `github-releaser-agent` (canonical, post-merge)

`.maintainer.yaml: release.autoRelease: true` opts the repo in. After any commit lands
on `master` carrying `## Unreleased` bullets in `CHANGELOG.md`, the watcher emits a task
and the agent:

1. Classifies the semver bump from `## Unreleased` prefixes (`feat:` → minor, `fix:` → patch, `BREAKING:` → major)
2. Rewrites `## Unreleased` → `## vX.Y.Z`
3. Commits `release vX.Y.Z`, tags `vX.Y.Z`, pushes tag + commit

Picks up within ~10 min of the merge. To force an immediate scan: `/github-release-repo-trigger`.

**Operator's job in this flow**: keep `## Unreleased` bullets accurate; merge to master.
**Do NOT** rename `## Unreleased` → `## vX.Y.Z`, **do NOT** create a local tag — the bot
owns the release commit; a local version races it.

**Why dark-factory does NOT tag**: `.dark-factory.yaml: autoRelease: false` — feature-branch
daemon runs push commits without tags. The tag lands once, on master, post-merge, via the
releaser agent. This is the deliberate feature-branch hygiene pattern.

### Manual fallback (only if the releaser stalls / `.maintainer.yaml` absent)

`/coding:commit` on `master` converts `## Unreleased` → `## vX.Y.Z`, tags, and pushes.
Runs `make release-check` first. Use only when the agent-driven flow is unavailable — do
not race the bot.

### Verifying a release shipped

```bash
git fetch --tags
git describe --tags --abbrev=0                                  # latest tag
git log "$(git describe --tags --abbrev=0)"..HEAD --oneline     # unpushed commits beyond it
```

After a successful release, `git status` clean and `git rev-list @{u}..HEAD --count` = 0.

## Install (the moment the new version reaches consumers)

```bash
cd ~/.claude && make install-distill   # go install github.com/bborbe/distill@latest
# then regenerate the CLAUDE.md files with the new binary:
cd ~/.claude && make generate                          # global ~/.claude/CLAUDE.md
cd ~/Documents/Obsidian/Personal/.claude && make generate   # vault CLAUDE.md
```

Both consumers read the source rule folders and rewrite their `CLAUDE.md`. The first run
after a `system.md` change is a full cold recompile (every cache entry invalidated); a
`.distill-cache.json` then appears in each source dir and subsequent no-op runs make zero
`claude` calls.

**After regenerating**: `grep -c 'No task request' <CLAUDE.md>` must be 0 in both files.

## GitHub Release (manual — milestone only)

`autoRelease` creates a `vX.Y.Z` git tag; that is sufficient for `go install …@vX.Y.Z`.
A **GitHub Release** (Releases tab, notes, feed) is a separate deliberate act — create one
only for milestones, not every patch tag.

**Publishing a Release also ships the Homebrew cask.** `.github/workflows/release.yml`
triggers on `release: published` and runs goreleaser, which builds darwin/linux archives,
attaches them to this Release, and pushes the cask to
[`bborbe/homebrew-tap`](https://github.com/bborbe/homebrew-tap). So publishing here is what
makes `brew install bborbe/tap/distill` serve the new version.

**A tag alone never reaches brew.** This is deliberate: `autoRelease` tags every merge, and
publishing a cask per tag would bypass the gate above. The corollary of "milestones only" is
that brew users stay on the last promoted version until you promote again — `go install
@latest` is the fast track, brew is the verified one.

```bash
TAG=$(git describe --tags --abbrev=0)
gh release create "$TAG" --target master --title "$TAG" \
  --notes "$(awk -v tag="## $TAG" '$0 == tag {f=1; next} /^## v/ {f=0} f' CHANGELOG.md)"
```

Do not "simplify" that `awk` into a range like `awk "/^## $TAG/,/^## v/"`. A range evaluates
its END pattern on the same record where the START matched, and the `## vX.Y.Z` heading
matches `^## v` itself — so the range opens and closes on that one line, and the old
`| head -n -1` then stripped it, yielding **empty notes**. The flag form above skips the
heading and stops at the next one.

Then confirm the cask actually shipped — publishing the Release only *starts* the workflow:

```bash
gh run list --workflow=release.yml --limit 1          # workflow ran and succeeded
gh release view "$TAG" --json assets --jq '.assets[].name'   # archives attached
gh api repos/bborbe/homebrew-tap/contents/Casks/distill.rb --jq '.content' \
  | base64 -d | grep -E '^\s+version\s+'              # cask at the new version
brew update && brew install bborbe/tap/distill && distill --version
```

If the workflow never ran, the Release was created as a **draft** — drafts do not fire
`release: published`. Publish it (`gh release edit "$TAG" --draft=false`).

## See also

- [spec.md](spec.md) — the behavior contract (frontmatter, compile pipeline, cache, validation, invocation)
- `CLAUDE.md` — dark-factory flow + key rules
- `scenarios/` — the regression suite the gate walks
