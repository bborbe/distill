---
status: approved
spec: [002-bug-cache-not-bound-to-output]
created: "2026-10-19T20:45:00Z"
queued: "2026-10-19T21:25:38Z"
branch: dark-factory/bug-cache-not-bound-to-output
---

<summary>
- When the cache's recorded output path matches but the file has changed, distill rebuilds the cache from that file instead of calling the model.
- The committed output wins: its sections and bullets are mapped back onto the enabled rules in the same order distill would emit them.
- A rebuilt run makes zero model calls and leaves the output file byte-for-byte untouched.
- The rebuilt entries are saved so the next run is warm and only genuinely edited rules are recompiled.
- If the output cannot be mapped back exactly (a section or bullet count disagrees, or a heading is unknown), distill warns and falls back to a full cold compile.
- A missing cache is still a cold compile, not a rebuild, so an edit made since the output was committed is never silently swallowed.
- Old-schema caches are rebuilt from the output rather than triggering a full recompile.
- A hand-edited bullet is adopted into the cache and survives later warm runs.
- Editing one rule after a rebuild recompiles only that rule.
</summary>

<objective>
Rebuild cache entries from the committed output when the cache is unbound or its recorded output hash is stale: parse the output into section-to-bullets, attribute bullets positionally to enabled rules in emission order, require an exact all-or-nothing match, populate the cache with each rule's current content hash, skip both the model and the writer, and fall back to a cold compile on any mismatch. Implements Desired Behaviors 3, 4, and the save-after-rebuild part of 6, plus Acceptance Criteria 3 through 7.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read `specs/in-progress/002-bug-cache-not-bound-to-output.md` — Desired Behaviors 3, 3a, 4, and 6; the Failure Modes rows for a stale content hash and an unparseable output; the Assumptions about section emission order, bullet-count attribution, and column-0 markers; the Security section; and Acceptance Criteria 3–7.

Read `docs/spec.md` — the Cache section and the Output Structure section. The output shape is the contract the parser must read.

Read these files fully before editing:
- `pkg/distill/driver.go` — `Run`, `compileBullets`, `filterEnabled`, `sortRulesGlobal`, `groupBySection`, `assembleOutput`, `ruleIDs` (added by the previous prompt), and the `loadCache` / `saveUnlessForeign` / `saveMergedUnlessForeign` helpers.
- `pkg/distill/cache.go` — `CacheStatus` (`CacheCold`, `CacheWarm`, `CacheRebuild`, `CacheForeign`), `Load`, `Save`, `Put`, `RuleHash`, and the binding fields. The previous prompt added these; do not change their semantics.
- `pkg/distill/source.go` — the `Rule` struct (`ID`, `Section`, `Order`, `Disabled`, `Body`) and `Parser`.
- `pkg/distill/writer.go` — the atomic writer; a successful rebuild must not call it.
- `pkg/distill/validate.go` — `ValidateBullet`; the rebuild does **not** re-validate attributed bullets.
- `pkg/distill/driver_test.go` — the existing `stubRunnerWith` helper, the cache integration tests, and the `readOutput` helper.
- `pkg/distill/cache_test.go` — the bound-cache helpers added by the previous prompt.

Read these coding-plugin docs (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md`
</context>

<requirements>

## 1. Add the output parser and attribution helpers

Add these types and functions to `pkg/distill/driver.go`. Add `os` to the imports (it is not currently imported); `strings` and `errors` are already imported. Keep every function under the `funlen` limit (80 lines / 50 statements), `nestif` below 4, and lines at most 100 characters.

```go
// outputSection is one `## <name>` heading and its bullets in file order.
type outputSection struct {
	Name    string
	Bullets []string
}

// parseOutputSections parses an assembled output file into sections and their
// bullets. It reads only column-0 markers outside fenced code blocks.
func parseOutputSections(ctx context.Context, output string) ([]outputSection, error)

// attributeSections zips each section's bullets positionally onto its enabled
// rules in emission order. It is all-or-nothing.
func attributeSections(
	ctx context.Context,
	allRules, enabled []Rule,
	sections []outputSection,
) (map[string]string, error)

// rebuildFromOutput reads outputPath and returns a bullet for every enabled
// rule id, or an error when the output cannot be attributed exactly.
func rebuildFromOutput(
	ctx context.Context,
	outputPath string,
	allRules, enabled []Rule,
) (map[string]string, error)
```

### 1a. `parseOutputSections` rules

Split the output on `"\n"`. For each line, first toggle a fence flag when `strings.TrimSpace(line)` starts with ```` ``` ````, then evaluate the marker rules against the **post-toggle** state (this mirrors `ParseBatchResponse`). Outside a fence:

- A line starting with `"## "` at column 0 starts a new section. The name is `strings.TrimSpace(strings.TrimPrefix(line, "## "))`. Finalize the current bullet and the current section first. If the same section name was already seen, return an error (`duplicate section heading %q`).
- A line starting with `"- **"` at column 0 starts a new bullet. If no section is open, return an error (`bullet before first section heading`). Finalize the current bullet first.
- A line starting with `"- "` at column 0 that does not start with `"- **"` is a second top-level list item in one bullet. Return an error (`column-0 list item is not a bold bullet`); do not guess.
- Any other line, and any line inside a fence, is a continuation of the current bullet when a bullet is open; otherwise it is ignored (the header, the optional `# title`, and blank lines before the first bullet).

A bullet's text is the first line plus its continuation lines joined with `"\n"`, with trailing `"\n"`, spaces, and tabs trimmed via `strings.TrimRight(bullet, "\n \t")` (the same trim `assembleOutput` applies). Finalize the last bullet and the last section after the loop. A header-only output returns an empty section slice and no error.

The file read and its error wrapping belong to `rebuildFromOutput` (see 1c): read `outputPath` with `os.ReadFile` and a `#nosec G304` comment naming the `--output` argument as the source; wrap a read error with `errors.Wrapf(ctx, err, "read committed output %q", outputPath)`.

### 1b. `attributeSections` rules

Build `known` from **all** parsed rules (`allRules`, including disabled rules) and `groups` from `groupBySection(enabled)`.

1. Build `bulletsBySection` from the parsed sections. If a section name is not in `known`, return an error (`output section %q does not exist in the rules`).
2. For every section in `groups`: it must be present in `bulletsBySection` (otherwise `output is missing section %q`), and `len(bullets) == len(groups[name])` (otherwise `section %q has %d bullets but %d enabled rules`). Zip bullets to rules positionally: `bulletByID[rules[i].ID] = bullets[i]`.
3. For every output section not in `groups`: its bullet count must be 0 (an all-disabled section is legitimately 0 bullets / 0 enabled rules). If it has bullets, return an error (`section %q has bullets but no enabled rules`).

`groups[section]` is already in `(order, id)` order because `Run` calls `sortRulesGlobal(enabled)` before this point and `groupBySection` preserves input order. Do not re-sort.

Do **not** call `ValidateBullet` on attributed bullets. The spec states that bullets recovered by attribution are not re-validated; the parser's column-0 rules are the only check.

## 2. Integrate the rebuild into `Run`

Replace the existing cache-load block (added by prompt 1) with the following block. It must appear exactly once, after `sortRulesGlobal(enabled)` and before `compileBullets`:

```go
status := CacheCold
if !d.NoCache {
	status = d.loadCache(ctx, normalizedOutput)
	if status == CacheRebuild {
		if d.tryRebuild(ctx, normalizedOutput, rules, enabled) {
			return nil
		}
		status = CacheCold
	}
}
```

`tryRebuild` must:

```go
// tryRebuild rebuilds cache entries from the committed output. It returns true
// when the run is complete: the cache is saved and the output is untouched.
func (d *Driver) tryRebuild(
	ctx context.Context,
	outputPath string,
	allRules, enabled []Rule,
) bool
```

- Call `rebuildFromOutput(ctx, outputPath, allRules, enabled)`.
- On error, write a stderr warning that names the output and the failure and says it is compiling cold, for example:
  `distill: warning: committed output %q cannot be attributed to the rules: %v — compiling cold\n`
  Return `false`.
- On success, for every enabled rule call `d.Cache.Put(r.ID, d.Cache.RuleHash(d.Model, r.Body), bulletByID[r.ID])`. This stores each attributed bullet under that rule's **current** content hash, so a later edit to a rule is a miss and an unchanged rule is a hit.
- Call `d.Cache.Save(ctx, outputPath, ruleIDs(enabled))`. A save error is a warning (`distill: cache save warning: %v`), never fatal; the run still succeeds.
- Print the standard summary line with every enabled rule counted as cached:
  `distill: %d cached, 0 compiled (0 chunks), 0 retried\n` using `len(enabled)`.
- Return `true`.

A successful rebuild MUST NOT call `d.Writer.Write` and MUST NOT call `d.Runner.Run`. The output file is the authoritative artifact and is left byte-for-byte unchanged.

## 3. Preserve the cold paths

- A **missing** cache (`CacheCold`) must not enter `tryRebuild`. It compiles cold as today and `saveUnlessForeign` binds the cache on success. This is Desired Behavior 3a: rebuilding into an empty cache would attribute the committed output to the current sources and swallow an edit made since that output was committed.
- A **foreign** cache (`CacheForeign`) must not enter `tryRebuild`; it compiles cold and writes no cache (the previous prompt's guard).
- A `CacheWarm` run proceeds to `compileBullets` unchanged.
- A rebuild failure sets the local status to `CacheCold` and falls through to the normal compile path. The loaded entries are empty for `CacheRebuild`, so every rule is a miss and the run compiles cold; it never serves a partially attributed cache.

## 4. Tests

### 4a. Parser and attribution unit tests

Add `pkg/distill/rebuild_internal_test.go` in `package distill` (mirroring `claude_internal_test.go`; the suite is registered in `distill_suite_test.go`, so no new `TestXxx` is needed). Include the standard BSD license header. Use Ginkgo v2 + Gomega.

`parseOutputSections` cases:
- Parses multiple sections and their bullets in file order, including a multi-line bullet whose continuation is indented.
- Ignores the auto-generated header, the optional `# title`, and blank lines before the first bullet.
- Treats `## ` and `- ` lines inside a fenced block as content, not markers (include a bullet with a fenced block).
- Returns an error for a bullet before the first section heading.
- Returns an error for a column-0 `- ` line that is not `- **`.
- Returns an error for a duplicate section heading.
- Returns no sections and no error for a header-only output.

`attributeSections` cases:
- Zips bullets to enabled rules in emission order across two sections.
- Returns an error when an enabled section is missing from the output.
- Returns an error when the output has an unknown section heading.
- Returns an error when a section's bullet count disagrees with its enabled rule count.
- Accepts an all-disabled section heading with zero bullets (the section exists in `allRules` but not in `enabled`).
- Returns an error when an all-disabled section heading has bullets.

### 4b. Driver integration tests

Add these to `pkg/distill/driver_test.go` (external `distill_test`). Use a real `distill.NewFileCache(cachePath, stderr)` and a shared `*bytes.Buffer` for both the cache's warn writer and `Driver.Stderr`. Use `stubRunnerWith` for the first run so the output has known bullets, for example `{"a": "- **A.** alpha", "b": "- **B.** beta"}`.

- **Rebuild from a hand-edited output (Acceptance Criteria 3 and 6).**
  1. First run writes the output and binds the cache.
  2. Rewrite one bullet's text in the output file (keep the section and bullet counts unchanged).
  3. Second run with a fresh `mocks.DistillRunner` and a `mocks.DistillWriter`.
  4. Assert `RunCallCount() == 0`, `WriteCallCount() == 0`, and the output bytes are identical to the hand-edited bytes.
- **Rebuild from an old-schema cache (Acceptance Criterion 4).**
  1. First run writes the output and binds the cache.
  2. Read the cache JSON, set `"version"` to `"v3"` (keep the entries), and write it back. The entry hashes are now stale, as a real v3 cache would be.
  3. Second run with a fresh runner.
  4. Assert `RunCallCount() == 0` and the output bytes are identical before and after.
- **Rebuilt cache is usable (Acceptance Criterion 5 chain).**
  1. First run with rules `a` and `b`.
  2. Hand-edit bullet `a` in the output.
  3. Second run: assert `RunCallCount() == 0`, no writer call, output unchanged.
  4. Edit rule `b`'s source body.
  5. Third run with `stubRunnerWith(map[string]string{"b": "- **B.** revised"})`.
  6. Assert `RunCallCount() == 1`, stderr contains `1 cached, 1 compiled`, the output contains `- **A.** hand-edited` (the hand-edit survived), and the output contains `- **B.** revised`.
- **Attribution failure falls back to cold (Acceptance Criterion 7).**
  1. First run writes the output.
  2. Remove one bullet line from the output (counts now disagree).
  3. Second run with a fresh runner.
  4. Assert `RunCallCount() > 0` and stderr contains a warning naming the output and the attribution failure.
- **Missing cache is not rebuilt (Desired Behavior 3a).**
  1. First run with `NoCache: true` so a valid output exists but no cache file.
  2. Ensure the cache file is absent.
  3. Second run with a cache-enabled driver and a fresh runner.
  4. Assert `RunCallCount() > 0` (cold compile, not a rebuild).

</requirements>

<constraints>
- The rebuild is a pure function of the rules and the committed output. No network, no LLM, no `claude` invocation.
- Attribution is all-or-nothing. Never serve a partially attributed cache; on any mismatch, warn and fall back to a cold compile.
- A successful rebuild writes no output and calls no runner. The committed output is the reviewed artifact.
- A missing cache must NOT be rebuilt (Desired Behavior 3a). Only `CacheRebuild` enters the rebuild path.
- Do not re-validate attributed bullets and never send output text into a prompt. The rebuild only reads the output.
- Preserve the summary line format `distill: N cached, M compiled (K chunks), R retried`, the output header shape, section ordering, and the exit-code contract.
- Do not change the cache schema, the cache path, the CLI, or the factory.
- Error wrapping: `github.com/bborbe/errors` only. Never `fmt.Errorf`; never `context.Background()` in `pkg/`.
- Add `#nosec G304` with a reason for reads of the operator-supplied `--output` path.
- Ginkgo v2 + Gomega. New and changed code needs ≥80% statement coverage, including the parser's error paths and the attribution mismatches.
- Do NOT commit — dark-factory handles git.
- Do not update `docs/spec.md`, `README.md`, or `CHANGELOG.md`; prompt 3 of this spec covers documentation. This prevents a duplicate `## Unreleased` entry from the global CHANGELOG rule.
- Prerequisite: prompt 1 (`1-cache-output-binding.md`) must already be complete; `CacheStatus`, `loadCache`, `ruleIDs`, and the cache binding fields must exist. If they are absent, report `status: failed` instead of re-implementing them. Approve/execute in order 1 → 2 → 3.
</constraints>

<verification>
Run `make precommit` — it must exit 0.

Confirm the rebuild path exists:
```
grep -n 'rebuildFromOutput\|parseOutputSections\|attributeSections\|type outputSection' pkg/distill/driver.go
grep -n 'func (d \*Driver) tryRebuild' pkg/distill/driver.go
grep -n 'CacheRebuild' pkg/distill/driver.go
grep -n 'func (d \*Driver) Run' pkg/distill/driver.go
```
All must return lines.

Run the suite:
```
go test -mod=mod ./pkg/distill/...
```

Check coverage of the new helpers:
```
go test -coverprofile=/tmp/cover.out -mod=mod ./pkg/distill/... && go tool cover -func=/tmp/cover.out | grep -E 'rebuildFromOutput|parseOutputSections|attributeSections|tryRebuild'
```
Each new helper must be ≥80% statement coverage.

Confirm the rebuild does not write and does not run the model by inspecting the tests:
```
grep -n 'hand-edited' pkg/distill/driver_test.go
grep -n 'old-schema' pkg/distill/driver_test.go
```
Both must return lines (these strings are unique to the new rebuild cases; the generic `WriteCallCount`/`RunCallCount` greps already match pre-existing tests and would pass even without the new ones).
</verification>

<completion_report_template>
Append the standard DARK-FACTORY-REPORT block with `status`, `verification.command`, and `verification.exitCode`. Then an `## Improvements` section (PROMPT / GUIDE / GLOBAL, or `- None`).
</completion_report_template>
