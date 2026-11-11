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

Every scenario is walked by hand today — there is no `scenarios/helper/run-NNN-all.sh`. When
porting them to scripted helpers, follow the dark-factory pattern (`run-NNN-all.sh` builds
the binary, isolates `HOME`, asserts exit codes) rather than inventing a second convention.

`scenarios/001-compile-anti-injection.md` is the load-bearing one: it drives real
`claude` with a hijack-bait rule + a planted `CLAUDE.md` and asserts the output has
proper bullets, zero `No task request` junk, and no ambient-CLAUDE.md leak. It is an
LLM-driving scenario (~30 s–2 min, real token cost) — babysit it; do not run unattended.

If any scenario fails: do **not** install. Fix the regression first, then rerun the gate.

### When the diff is empty (the one valid skip)

Nothing on the binary surface changed since the installed binary:

```bash
INSTALLED=$(distill --version 2>/dev/null | awk '{print $NF}')
# The skip needs a bare release tag to diff against. `dev`, a VCS-stamped
# `v0.10.0+dirty`, a `-N-gSHA` describe string and an empty answer all mean there
# is nothing to compare — walk the gate instead of skipping it. Anchor both ends:
# an unanchored `v[0-9]*.[0-9]*.[0-9]*` glob matches `v0.10.0+dirty` too.
if ! printf '%s' "$INSTALLED" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "installed binary reports '$INSTALLED' — no release tag to compare, walk the gate"
  exit 1
fi
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
4. Creates the GitHub Release for that tag, with the `## vX.Y.Z` section as its notes — which is what fires `release.yml` and moves the cask

Step 4 is the one that is easy to get wrong when reasoning about this repo: it means the
cask follows every tag, and the scenario gate below is **operator-side only** — nothing in
the pipeline blocks a publish on it.

Picks up within ~10 min of the merge. To force an immediate scan: `/github-release-repo-trigger`.

**Operator's job in this flow**: keep `## Unreleased` bullets accurate; merge to master.
**Do NOT** rename `## Unreleased` → `## vX.Y.Z`, **do NOT** create a local tag — the bot
owns the release commit; a local version races it.

**Why dark-factory does NOT tag**: `.dark-factory.yaml: autoRelease: false` — feature-branch
daemon runs push commits without tags. The tag lands once, on master, post-merge, via the
releaser agent. This is the deliberate feature-branch hygiene pattern.

There are two release drivers in a bborbe repo; only one is on here:

| Scenario | dark-factory (`.dark-factory.yaml: autoRelease: false`) | `github-releaser-agent` (`.maintainer.yaml: release.autoRelease: true`) |
|---|---|---|
| Daemon runs a prompt on a feature branch | does not fire — the flag is off | does not fire (commits not on master yet) |
| Direct PR + merge | does not fire (no daemon involvement) | **fires** |
| Daemon runs on master directly | does not fire — the flag is off | fires post-merge |

Turning the daemon's flag on would race the releaser for the same version number, which is
why only one driver is enabled.

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

### Is brew behind? — `make release-status`

The curated policy above has one failure mode: the human gate simply stops being
walked, and nothing says so. That is what happened between `v0.6.0` and `v0.9.7`
— 18 versions of drift with no signal.

`make release-status` is that signal. It prints the latest tag, the cask version
in `bborbe/homebrew-tap`, and how many versions the cask is behind, and exits
non-zero when it is behind:

```bash
make release-status
#   latest tag                       v0.9.7
#   brew cask (bborbe/homebrew-tap)  0.6.0
#   versions behind                  18
# ⚠️  brew is 18 version(s) behind v0.9.7
```

(That output is the 2026-11-11 reading, the state that motivated this check.)

It is **diagnostic, not a gate** — deliberately not wired into `make precommit`,
since it reads the network and the tap's live state. Run it when you want to know
whether the releaser is keeping the tap current; wire it into a cron or a weekly
task if you want the drift to announce itself.

`make check-versions` is the other half: it asserts the top `## vX.Y.Z` section in
`CHANGELOG.md` equals the latest git tag, so a release commit that landed half-way
(a tag without a CHANGELOG section, or the reverse) is caught before you publish.

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

## GitHub Release — created by the releaser, and it ships the cask

`github-releaser-agent` (`ben-s-github-releaser[bot]`) cuts the tag **and** creates the
GitHub Release in the same pass, taking the `## vX.Y.Z` CHANGELOG section as its notes.
`.github/workflows/release.yml` then triggers on `release: published`, runs goreleaser,
attaches the darwin/linux archives, and pushes the cask to
[`bborbe/homebrew-tap`](https://github.com/bborbe/homebrew-tap). So on the current setup the
tap follows every tag and there is no manual publish step.

⚠️ **This section said the opposite until 2026-11-11, and the history is on its side.** Of
the 31 tags up to `v0.9.7`, only `v0.6.0` ever had a Release — the tap sat 18 versions behind
and `brew install` served a binary built 2026-08-09. The releaser's behaviour changed at some
point between `v0.9.7` and `v0.10.0`; nothing in this repo documents when. `make release-status`
is the live check, and it is the only claim here you should trust without re-verifying.

Manual creation is the **fallback** — for a stalled releaser, or a release that has to be
re-cut:

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

Before a **manual** publish, all three should hold:

1. Every `scenarios/*.md` passed against a freshly built binary (the gate above).
2. `make check-versions` reports the CHANGELOG top section and the latest tag agree.
3. The `## vX.Y.Z` notes say what a user should care about, not the raw commit log.

`make release-status` is the canary for all of this: it exits non-zero when the cask has
fallen behind the latest tag, which is the shape a stalled releaser takes.

After any release — automatic or manual — confirm the cask actually shipped; creating the
Release only *starts* the workflow:

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
