---
status: completed
approved: "2026-10-19T20:20:09Z"
generating: "2026-10-19T20:35:14Z"
prompted: "2026-10-19T21:03:25Z"
verifying: "2026-10-19T21:37:42Z"
completed: "2026-10-19T21:51:40Z"
branch: dark-factory/bug-cache-not-bound-to-output
---

## Summary

- `distill`'s content-hash cache lives at `<source-dir>/.distill-cache.json` and is keyed only to the source directory — it records whatever the **last run** compiled, not what is committed at `--output`.
- A cache-enabled run whose `--output` is a scratch path (the runbook's own staleness check does exactly this) overwrites the shared cache with bullets from that scratch compile. The next routine regenerate then serves those bullets and rewrites the committed `CLAUDE.md` wholesale from output nobody reviewed.
- Observed 2026-10-19: a one-rule edit reported `70 cached, 1 compiled` yet produced a 589-line diff (+245/−344) against the committed file.
- The fix binds the cache to the output it was built for: a cache is served only when it describes the committed output, and is otherwise rebuilt from that output without calling the LLM.
- After this work: an unchanged-source regenerate reproduces the committed output byte-for-byte with zero `claude` calls, and a run aimed at a *different* output than the cache's recorded one leaves the shared cache byte-identical. (A cache that has never been bound is bound by the first run that uses it — see Assumptions.)

## Problem

The cache is a second source of truth for bullet text with no link to the file those bullets are supposed to produce: it records a schema version and per-rule bullets, and its path is derived from `--source` alone. Any cache-enabled run therefore writes the same cache regardless of where its output went, so a compile that was reviewed-and-reverted, or one rendered to a scratch file, silently becomes the next run's starting state. The failure is silent because the run summary reports cache hits, and the resulting diff looks like ordinary LLM re-wording rather than a stale-cache replay.

A second, related hazard: the cache on disk is schema `v3` while the current source tree writes `v4`, so the first run of any binary built from master warns "cache schema version mismatch — running cold" and recompiles all 71 rules. Without a rebuild path that costs a full unreviewed rewrite — the same outcome by a different route.

## Reproduction

Binary: built from `origin/master` (v0.9.2) with `go build -o /tmp/distill-repro/bin/distill .`. Fixture: a scratch source dir holding one rule, so the real `~/.claude` cache is never touched.

```bash
mkdir -p /tmp/distill-repro/src
cp ~/.claude/claude-md-rules/worker-does-not-open-sessions.md /tmp/distill-repro/src/

# 1. cache-enabled run to a scratch output
./bin/distill --source /tmp/distill-repro/src --output /tmp/distill-repro/out.md
# → distill: cache file "/tmp/distill-repro/src/.distill-cache.json" missing — running cold
# → distill: 0 cached, 1 compiled (1 chunks), 0 retried
# → .distill-cache.json appears in the SOURCE dir, not beside the output

# 2. cache-enabled run to a DIFFERENT output
./bin/distill --source /tmp/distill-repro/src --output /tmp/distill-repro/other.md
# → distill: 1 cached, 0 compiled (0 chunks), 0 retried
#   the cache built for out.md is served for other.md

# 3. control: --no-cache leaves the cache alone
BEFORE=$(stat -f %m src/.distill-cache.json)
./bin/distill --source /tmp/distill-repro/src --output /tmp/distill-repro/third.md --no-cache
AFTER=$(stat -f %m src/.distill-cache.json)
# → before == after
```

Observed evidence from the production incident this reproduces: a one-rule edit reported `distill: 70 cached, 1 compiled` while `git diff --stat CLAUDE.md` read 589 lines (+245/−344); the committed `cf00b3a` (431 lines) carries "One Session One Goal" where the served bullets carried different wording.

## Expected vs Actual

| | |
|---|---|
| **Expected** | `docs/spec.md` § Goal: "Re-running on unchanged sources spawns zero `claude` processes and writes byte-identical output." The cache is a derived store for the file at `--output`; a run whose output is not that file must not become the next run's input. |
| **Actual** | The cache is written and read per source directory, ignoring `--output` entirely, so a run to any other path re-seeds it. The next routine regenerate serves those bullets and rewrites the committed file from output nobody reviewed. |

## Goal

After this work is done, the following is true of `distill`:

- A cache entry is served only when the cache describes the output file the run is about to write.
- A cache that does not describe the committed output is rebuilt from that output, without any LLM call, so unchanged sources reproduce the committed output byte-for-byte.
- A run whose `--output` is not the output the cache belongs to does not write the shared cache.
- A schema-version bump costs a rebuild from the committed output, not a full LLM recompile.
- `--no-cache` continues to bypass all cache reads and writes.

## Non-goals

- No change to the cache file location (`<source-dir>/.distill-cache.json`).
- No new CLI flag.
- No change to the compression prompt, batching, validation, retry, or anti-injection invocation.
- No change to source-file frontmatter, section grouping, ordering, or the output header shape.
- No change to the exit-code contract.

## Acceptance Criteria

- [ ] A cache-enabled run whose `--output` differs from the cache's recorded output leaves `.distill-cache.json` byte-identical — evidence: `shasum` of the cache file before and after the run are equal, and stderr carries a warning naming the cache's recorded output **and** the remedy (run against that output, or delete the cache).
- [ ] Two spellings of the same output share one binding — evidence: run with `--output <dir>/.claude/../CLAUDE.md`, then with `--output <dir>/CLAUDE.md`; the second reports `N cached, 0 compiled`, writes byte-identical output, and the recorded path in the cache file is the cleaned absolute form.
- [ ] Unchanged sources plus a committed output that differs from the cache's recorded content produce an output file byte-identical to the committed file, with zero `claude` invocations — evidence: driver test asserts `Runner.RunCallCount() == 0` and the bytes at `--output` after the run equal the bytes before it.
- [ ] A cache written under an older schema version is rebuilt from the committed output rather than triggering a recompile — evidence: test seeds a `v3`-shaped cache whose entry hashes are stale for the current rule bodies (as a real v3 cache is) plus a matching output file, runs, asserts `Runner.RunCallCount() == 0` **and** the bytes at `--output` are byte-identical before and after the run.
- [ ] A cache whose recorded output hash does not match the file on disk triggers a rebuild from that file, not a cold compile — evidence: test rewrites one bullet's text in the output file (counts unchanged), runs, asserts `Runner.RunCallCount() == 0` and the bytes at `--output` are byte-identical before and after the run.
- [ ] The rebuilt cache is usable, not merely populated — evidence: chain the steps in one test: (1) hand-edit bullet A's text in the output file; (2) run → rebuild, `RunCallCount() == 0`, output unchanged; (3) edit rule B's body in the sources; (4) run → `RunCallCount() == 1`, stderr `N-1 cached, 1 compiled`, and `diff` shows exactly B's bullet changed while A's hand-edit is still present.
- [ ] When the committed output cannot be attributed to the rules (section or bullet counts disagree), the run falls back to a cold compile and does not serve stale entries — evidence: test with a hand-edited output missing one bullet asserts `Runner.RunCallCount() > 0` and a stderr warning.
- [ ] `--no-cache` leaves the cache file byte-identical — evidence: `shasum` before and after are equal.
- [ ] A warm run on unchanged sources still spawns zero `claude` processes and writes byte-identical output — evidence: `Runner.RunCallCount() == 0` and a clean diff (existing behaviour, must not regress).
- [ ] `make precommit` exits 0 — evidence: exit code.
- [ ] `CHANGELOG.md` has a non-empty `## Unreleased` section — evidence: `awk '/^## Unreleased/{f=1;next} /^## /{f=0} f && NF' CHANGELOG.md | wc -l` returns ≥ 1.
- [ ] `docs/spec.md` documents the output binding and the rebuild path, and no longer contradicts it — evidence: `awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -c output` returns ≥ 1; the same range contains a row matching `cache does not match the output`; and `awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -ci 'version.*rebuild'` returns ≥ 1 (the version-mismatch row now names the rebuild, not "run cold"). The same pass corrects the stale schema description: `awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -ci 'version.*entries'` returns ≥ 1 and `... | grep -ci 'integer'` returns 0.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — build, lint, format, generate, tests all pass.
- `go test ./pkg/distill/...` — the cache and driver suites carry the cases named in the Acceptance Criteria.
- `awk '/^## Unreleased/{f=1;next} /^## /{f=0} f && NF' CHANGELOG.md | wc -l` — returns ≥ 1.
- `awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -c output` — returns ≥ 1.

### Operator-executable (runs on the host after merge, spec verification ladder)

1. Build the fixed binary: `go build -o /tmp/distill-fixed/distill .`
2. Create the fixture (self-contained, one rule):
   ```bash
   mkdir -p /tmp/distill-fixed/src && cat > /tmp/distill-fixed/src/demo.md <<'EOF'
   ---
   distill:
     section: Demo
     id: demo
   ---
   Reply in English at all times.
   EOF
   ```
3. First run binds the cache: `/tmp/distill-fixed/distill --source /tmp/distill-fixed/src --output /tmp/distill-fixed/out.md` → stderr `0 cached, 1 compiled`; `.distill-cache.json` appears in `/tmp/distill-fixed/src` and names `out.md`.
4. Cross-output check: `shasum /tmp/distill-fixed/src/.distill-cache.json`, then `/tmp/distill-fixed/distill --source /tmp/distill-fixed/src --output /tmp/distill-fixed/other.md` → the `shasum` is unchanged and stderr warns the cache belongs to `out.md`.
5. Rebuild check: `/tmp/distill-fixed/distill --source /tmp/distill-fixed/src --output /tmp/distill-fixed/out.md` after rewriting one word in `out.md` → `1 cached, 0 compiled`, and `out.md` is byte-identical to its pre-run content.
6. Real target, with the cache's recorded output unchanged: run the exact `Regenerate:` line from `~/.claude/CLAUDE.md`'s banner → stderr `71 cached, 0 compiled` and `cd ~/.claude && git diff --stat CLAUDE.md` is empty.
7. Real target, one-rule edit: touch one rule body in `~/.claude/claude-md-rules/`, rerun the same command → stderr `70 cached, 1 compiled` and `git diff CLAUDE.md` shows only that rule's bullet.
8. **Replay `## Reproduction` steps 1–3 against the fixed binary** (bug-workflow's mandatory check). Step 2 must now warn that the cache belongs to `out.md` and leave `.distill-cache.json` byte-identical, instead of reporting `1 cached, 0 compiled`.

## Desired Behavior

1. **The cache records its output.** The persisted cache carries the **normalized absolute** output path it was built for (`filepath.Abs` then `filepath.Clean` of the `--output` argument, with symlinks unresolved) and a content hash of that file as written. A cache without these fields is *unbound* — it describes no particular output.
2. **Entries are served only on a full match.** A cached bullet is used only when the recorded output path equals the normalized absolute form of this run's `--output` **and** the recorded content hash equals the hash of the file currently at that path. Normalization is load-bearing: the vault consumer passes `$(CURDIR)/../CLAUDE.md` while its own banner records the cleaned path, and both must resolve to one binding.
3. **The output is authoritative on any mismatch.** When the binding matches the output path but the content hash does not — or the cache is unbound (older schema) — the run rebuilds cache entries **from the file on disk**, never from the cache: sections in emission order, bullets within a section in rule order. Each attributed bullet is stored under its rule id with that rule's current content hash. The run then proceeds with zero LLM calls and writes no change to the output. The cache is a derived store; the committed file wins. The run never rewrites the output to match the cache.
3a. **Absent is not unbound.** Rebuild-from-output applies only when a cache file exists but carries no usable binding (older schema, or a content-hash mismatch). A **missing** cache file has nothing to rebuild into, so the run compiles cold as today and writes the cache on success. Keeping the missing-cache path cold is deliberate: rebuilding into an empty cache would attribute the committed output's bullets to the current sources, silently swallowing any source edit made since that output was committed.
4. **Attribution is all-or-nothing.** Rebuilding succeeds only when every section's bullet count equals its **enabled** rule count and every section heading in the output exists in the rules (an all-disabled section is legitimately 0 bullets / 0 enabled rules). On any mismatch the run warns, discards the partial attribution, and falls back to a cold compile. It never serves a partially attributed cache.
5. **A foreign output is never read and never written.** A run whose `--output` differs from the cache's recorded output reads no entries, writes no cache, warns naming the recorded output, and states the remedy (run against that output, or delete the cache). The cache file stays byte-identical. An *unbound* cache is not foreign: it is rebuilt and bound to the current output on success.
6. **Saves record the binding, including after a rebuild.** Every successful run **whose output is not foreign** persists the cache with the output path and the hash of the bytes now at that path — including a rebuild run, whose output bytes are unchanged but whose rebuilt entries must land on disk so the next run needs no re-parse. A merged save after a failed run preserves the existing binding and does not rebind.
7. **`--no-cache` is untouched.** It bypasses all cache reads and writes exactly as today.

## Constraints

- Frozen behavior that must not regress: source frontmatter contract, section grouping and ordering, duplicate detection, atomic temp-file+rename output write, output header shape, exit codes, the `distill: N cached, M compiled (K chunks), R retried` summary line.
- `--no-cache` bypasses cache load and save only; it never disables validation, batching, or anti-injection.
- Both consumer Makefiles (`~/.claude`, vault `.claude`) stay byte-unchanged — no new flag is introduced.
- `bborbe` Go conventions: `github.com/bborbe/errors` wrapping (never `fmt.Errorf`, never bare `return err`); Ginkgo v2 + Gomega in external `_test` packages; Counterfeiter mocks under `mocks/` regenerated with `go generate ./...` if the `Cache` interface changes; factory stays pure composition.
- The cache path stays `<source-dir>/.distill-cache.json`.
- The rebuild must not require network or LLM access — it is a pure function of the rules and the committed output.
- **Consequence for scratch renders.** A render to a path other than the cache's recorded output is now a foreign-output run: it reads nothing, writes nothing, and therefore recompiles everything. Scratch renders must pass `--no-cache`. The vault runbook's Step 5 staleness check is exactly such a render and is corrected outside this repo; until it is, it costs a cold compile per check rather than poisoning the cache.

## Assumptions

- **The output file is the reviewed artifact.** A file at `--output` is assumed to be what the operator intends to keep; the cache is a derived store. This is what makes "rebuild from the output" the correct resolution of a hash mismatch, and it is why a hand-edited bullet is adopted rather than rejected.
- **Section emission order is deterministic.** `assembleOutput` orders sections by min-rule-order then name, and bullets within a section by `(order, id)`; the rebuild relies on that order being reproducible from the rules alone.
- **Bullet counts identify attribution.** Rebuilding zips a section's bullets to its **enabled** rules positionally. It is valid only when the counts agree; a missing or extra bullet is treated as unattributable, not guessed at.
- **The output is parseable by column-0 markers.** A bullet starts with `- **` at column 0 and its continuation lines are indented; a section starts with `## ` at column 0; fenced code blocks are indented, so no column-0 `- ` or `## ` appears inside one. This holds for the current committed file (71 bullets, 11 headings, no column-0 markers inside fences) and is what the rebuild's boundary rule depends on. If it ever stops holding, attribution fails closed to a cold compile rather than mis-attributing.
- **A source edit racing a rebuild is swallowed.** A rebuilt bullet is stored under its rule's **current** hash. If a rule body is edited before a rebuild, the rebuild records the old bullet under the new hash, so that edit produces no recompile and no diff until the rule changes again. This is a deliberate consequence of output-authoritative semantics: the file on disk is the reviewed artifact, and the operator sees the edit as absent rather than as a silent rewrite. Editing the rule after the rebuild (AC5's chain) is the normal path.
- **A compression-contract bump must still cold-compile.** The persisted schema version and the compression-context salt are currently the same constant. Rebuilding from the output is correct for a schema-only change (the current `v3`→`v4` bump, which does not invalidate existing bullets), but a future bump that changes the compression or output contract must not be rebuilt from the old file — that would adopt old-contract bullets as current and skip the recompile the bump exists to force. Such a bump must split the two constants or take the cold path.
- **One writer per cache file.** Two concurrent runs on the same source directory are unsupported (unchanged from today); the cache uses temp-file + rename so the last writer wins whole.
- **The recorded output path is stable after normalization.** Both sides are made absolute and cleaned before storing and comparing, so `.../.claude/../CLAUDE.md` and `.../CLAUDE.md` share one binding. This is required by the existing consumers: the vault `.claude/Makefile` passes `$(CURDIR)/../CLAUDE.md` while the file's own banner records the cleaned path, and both Makefiles must stay byte-unchanged. A genuinely different path (a distinct file) is still foreign; symlinks are not resolved.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection | Reversibility |
|---------|-------------------|----------|-----------|---------------|
| Cache written for a different `--output` | Warn naming the recorded output; run cold; do not write the cache | Run against the cache's own output, or delete the cache | stderr warning + unchanged cache hash | reversible |
| Cache content hash differs from the output on disk | Rebuild entries from the committed output; zero LLM calls | Automatic | stderr line naming the rebuild | reversible |
| Committed output unparseable / counts disagree | Warn; fall back to cold compile; never serve partial attribution | Fix or restore the output; rerun | stderr warning + `compiled > 0` | reversible |
| Cache schema version older than current | Rebuild from the committed output instead of a full recompile | Automatic | stderr line | reversible |
| Output file missing | Cold compile; cache written on success | Automatic | stderr warning | reversible |
| Cache missing / corrupt JSON | Warn; cold run; write on success (existing behaviour) | Automatic | stderr warning | reversible |
| Cache write fails (ENOSPC, temp-file creation, rename) | Output is still written and the run exits 0; cache-save failure is a stderr warning, never fatal (existing behaviour) | Free space, rerun to persist the cache | stderr warning | reversible |
| Concurrent runs on one source dir | Unsupported; last atomic write wins (existing behaviour) | Rerun | n/a | reversible |

## Security / Abuse Cases

No new surface. The rebuild parses a file the operator already controls and writes only cache entries keyed by rule ids drawn from the source directory. Bullets recovered by attribution are not sent to the model and are not re-validated.

The trust boundary is stated plainly rather than implied: **the output file is authoritative**, so a hand-edit to a bullet is adopted into the cache and survives later warm runs until the run uses `--no-cache`. (A source edit made *before* a rebuild is swallowed by the rebuild — see Assumptions — so the hand-edit can outlive a rule change in that one ordering.) That is a deliberate consequence of the incident model (the committed file is the reviewed artifact; the cache is derived), not an oversight. It is bounded: attribution is all-or-nothing, so a hand-edited file with the wrong section or bullet counts is rejected outright and falls back to a cold compile, and the adopted text is always text already present in a file the operator can see in git. Nothing from the output can reach a prompt — the rebuild only reads.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | `cache.go`: output path + content hash in the persisted schema; `Load` binding checks (path match, hash match, version); `Save` records the binding; `SaveMerged` preserves it; foreign-output read/write guard; `--no-cache` unchanged; Counterfeiter mock regenerated; unit tests | 1, 2, 5, 6, 7 | foreign-output no-write, `--no-cache` no-write, warm no-op | — |
| 2 | `driver.go`: parse the committed output into section→bullets, attribute to enabled rules in emission order, all-or-nothing, populate the cache and run with zero LLM calls; fall back to cold on mismatch; driver tests | 3, 4, 6 | byte-identical rebuild, old-schema rebuild, rebuilt-cache usability (AC5 chain), hand-edited fallback | prompt 1 |
| 3 | Docs: `docs/spec.md` Cache section + lifecycle rows, README cache note, `CHANGELOG.md` `## Unreleased` | — | `make precommit`, CHANGELOG, docs | prompts 1, 2 |

Rationale: prompt 1 establishes the persisted contract and the read/write guards; prompt 2 consumes it to implement the rebuild; prompt 3 is documentation and depends on the final behaviour. Each prompt leaves `make precommit` green.

## Alternatives Considered

- **Cache-authoritative on mismatch** (serve the cached bullets and rewrite the output from them). Rejected: the incident is exactly a cache that disagrees with the committed file, so trusting the cache would re-ship the unreviewed compile. It also cannot be implemented without storing the output bytes, which the cache does not have.
- **Delete the cache on any mismatch.** Rejected: it turns every mismatch into a full LLM recompile — the same unreviewed rewrite, just slower and more expensive — and the already-landed `v3`→`v4` bump makes that the first run of every new binary.
- **Bind by output path only** (no content hash). Rejected: a path match says nothing about whether the file changed since the cache was written, which is the case that produced the incident.
- **Store the whole output in the cache** so a mismatch can be restored. Rejected: duplicates the committed file inside a gitignored store, creating a second place for the reviewed text to drift.

## Do-Nothing Option

If we do nothing, every scratch render keeps poisoning the shared cache and every regenerate keeps shipping a wholesale rewrite that the operator did not review. The cache is already the mechanism by which a reviewed-and-reverted compile becomes the next run's input, so leaving it unbound guarantees the incident recurs. The already-landed `v3`→`v4` bump means the first run of any new binary is a full 71-rule recompile regardless, so the absence of a rebuild path is already costing one unreviewed rewrite.
