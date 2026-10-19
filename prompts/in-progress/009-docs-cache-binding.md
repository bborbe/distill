---
status: approved
spec: [002-bug-cache-not-bound-to-output]
created: "2026-10-19T20:45:00Z"
queued: "2026-10-19T21:25:38Z"
branch: dark-factory/bug-cache-not-bound-to-output
---

<summary>
- The user-facing specification now explains that the cache is bound to one output file and to that file's content.
- It documents what happens when the run's output is a different file, when the output changed, and when the cache is old or missing.
- The cache schema description matches reality: a version string, an entries map, and the new output binding fields.
- The README tells operators that scratch renders must pass `--no-cache` and that a hand-edited output is treated as authoritative.
- The changelog has an Unreleased entry describing the fix.
</summary>

<objective>
Document the cache output binding and the rebuild-from-output path in `docs/spec.md`, update the README cache note, and add a `## Unreleased` entry to `CHANGELOG.md`. Correct the stale cache schema description (version string, `entries` map, no integer) and split the old "missing / corrupt / version-mismatch" row into the distinct behaviors. This prompt is documentation only; prompts 1 and 2 shipped the implementation.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read `specs/in-progress/002-bug-cache-not-bound-to-output.md` — the Goal, Desired Behaviors 1–7, the Constraints, the Acceptance Criteria for documentation and CHANGELOG, and the operator-side Verification steps (they describe the final behavior you are documenting).

Read these files fully before editing:
- `docs/spec.md` — the Goal, Cache, Output Structure, Error Cases, and Architecture sections.
- `README.md` — the Output shape, Cache, and Edit protection hook sections and the flag table.
- `docs/adopting-distill.md` — the Gotchas list, which still says hand-edits are clobbered.
- `CHANGELOG.md` — the top of the file; add `## Unreleased` immediately after the introductory paragraph and before the latest released version.
- `docs/dod.md` — the documentation and contract-conformance checks.

The implementation from prompts 1 and 2 is the authority for what to write:
- `pkg/distill/cache.go` — `CacheStatus`, `cacheFile` (`version`, `output_path`, `output_hash`, `entries`), `Load` classification and warnings, `Save` binding, `SaveMerged` binding preservation, `normalizeOutputPath`, `hashOutput`.
- `pkg/distill/driver.go` — the normalized output path used in the header and writer, the `CacheForeign` save guard, `tryRebuild`, `parseOutputSections`, `attributeSections`, `rebuildFromOutput`.
Read both files before writing the docs so the prose matches the code, not the old spec.

Read these coding-plugin docs (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md`
</context>

<requirements>

## 1. Update the `## Cache` section of `docs/spec.md`

Keep the existing section structure and the operator note about committing vs gitignoring the cache file. Replace the stale schema sentence and add the binding/rebuild/foreign paragraphs.

### 1a. Schema

Replace the current "**Schema:** JSON object with a `version` integer and a `rules` map from id to `{hash, bullet}`." with a description that names a `version` string and an `entries` map, plus the binding fields:

```
**Schema:** JSON object with a `version` string, an `entries` map from id to `{hash, bullet}`, and the optional `output_path` / `output_hash` binding fields.
```

The words `integer` and `rules` map must no longer appear in the Cache section. The line must contain both `version` and `entries` (the acceptance check greps for `version.*entries`).

### 1b. Binding paragraph

Add:

```
**Output binding.** The cache records the output file it was built for: `output_path` is the normalized absolute `--output` path (`filepath.Abs` then `filepath.Clean`, symlinks unresolved) and `output_hash` is the hex SHA-256 of that file's bytes. A cached bullet is served only when the run's normalized output path equals `output_path` AND the file currently at that path hashes to `output_hash`. Normalization is load-bearing: `.../.claude/../CLAUDE.md` and `.../CLAUDE.md` share one binding. A cache without these fields is unbound and describes no particular output.
```

### 1c. Rebuild paragraph

Add:

```
**Rebuild from the committed output.** When the cache is unbound (an older schema, or a cache written before the binding existed) or its recorded output hash is stale, distill rebuilds entries from the file at `--output` instead of calling Claude. Sections are read in the order they appear, bullets within a section in rule order, and each bullet is stored under its rule id with that rule's current content hash. The rebuild is a pure function of the rules and the committed output — no network or LLM access. Attribution is all-or-nothing: every section heading in the output must exist in the rules, and every section's bullet count must equal its enabled rule count (an all-disabled section is legitimately 0 bullets / 0 enabled rules). On any mismatch distill warns and falls back to a cold compile; it never serves a partially attributed cache. A rebuilt run makes zero model calls and does not rewrite the output file. A hand-edited bullet is adopted into the cache and survives later warm runs until the run uses `--no-cache`. A **missing** cache is not rebuilt: it runs cold, because rebuilding into an empty cache would attribute the committed output's bullets to the current sources and silently swallow an edit made since that output was committed.
```

### 1d. Foreign-output paragraph

Add:

```
**Foreign output.** A run whose `--output` is not the cache's recorded output reads no entries, writes no cache, and warns naming the recorded output and the remedy (run against that output, or delete the cache). The cache file stays byte-identical. An unbound cache is not foreign: it is rebuilt and bound to the current output on success. Scratch renders to a different path are foreign-output runs and must pass `--no-cache`.
```

### 1e. Output path normalization

Add:

```
**Output path normalization.** The recorded and compared output path is `filepath.Abs` followed by `filepath.Clean`, with symlinks unresolved. The output header's `Regenerate:` line uses the same normalized absolute path.
```

### 1f. Lifecycle table

Replace the existing lifecycle table with rows that cover the new states. It must include these rows (wording may be adjusted, but keep the required substrings):

```
| Scenario | Behaviour |
|---|---|
| Cache-hit rule | `hash` matches the stored entry AND the cache is bound to this run's output with a matching content hash → serve the bullet; skip the LLM call. |
| Cache-miss rule | No entry or hash mismatch → include in the next LLM batch. |
| Fully successful run | Cache rewritten to contain entries for exactly the current enabled rule ids (removed/renamed/disabled rules are pruned) and bound to the normalized absolute output path plus the SHA-256 of the output bytes. |
| Validation failure (exit non-zero) | Bullets that DID validate are merged into the cache without pruning; the existing binding is preserved and is not rebound. |
| Cache missing / corrupt | Warn to stderr, run cold (all rules treated as misses), exit 0 on success; the cache is bound and written on successful completion. |
| Cache schema version is older than the current version | Rebuild entries from the committed output file (zero LLM calls); if the output file is missing, run cold. |
| The cache does not match the output (recorded path differs) | Foreign output: read no entries, write no cache, warn naming the recorded output and the remedy (run against that output, or delete the cache). The cache file stays byte-identical. |
| The cache does not match the output (content hash differs) | Rebuild entries from the committed output file (zero LLM calls); the output file is not rewritten. |
| Output file missing | Cold compile; the cache is bound and written on success. |
| `--no-cache` | Cache load and save are bypassed entirely; batching, validation, and anti-injection always run. |
```

The table must contain the exact lowercase substring `cache does not match the output` in at least one row (the acceptance check looks for it). The version row must contain both `version` and `rebuild` on one line.

## 2. Update the rest of `docs/spec.md`

2a. **Goal.** Add one sentence after the existing goal: the cache is bound to the output file it was built for, so a run against a different output cannot re-seed it.

2b. **Output Structure.** In the rule "The warning comment is always emitted, always at the top, with the resolved source dir + the exact command to regenerate", note that the `Regenerate:` line uses the normalized absolute output path.

2c. **Error Cases table.** Replace the single row `| Cache missing / corrupt / version-mismatch | Warn to stderr; cold run; exit 0 on success. |` with distinct rows:
- `| Cache missing / corrupt | Warn to stderr; cold run; exit 0 on success. |`
- `| Cache schema version older than current | Rebuild entries from the committed output (zero LLM calls); cold run if the output is missing. |`
- `| Cache belongs to a different output | Warn naming the recorded output and the remedy; read no entries; write no cache; exit 0 on success. |`
- `| Cache output hash differs from the output file | Rebuild entries from the committed output (zero LLM calls); the output is not rewritten. |`
- `| Output file missing | Cold run; cache bound on success. |`

2d. **Architecture.** Update the cache and driver bullets:
- `cache.go` — `Cache` interface; `CacheStatus`; `NewFileCache`; `NewNoopCache`; `RuleHash`; output binding (normalized path + content hash).
- `driver.go` — orchestrate; `compileBullets`; `assembleOutput`; `rebuildFromOutput`; `ExitCode`.

Do not rewrite unrelated sections and do not contradict the existing behavior. The spec remains the authority; do not restate the implementation line by line.

## 3. Update the README cache note

In `README.md`, update the `## Cache` section (the paragraph starting "`distill` keeps a content-hash cache at ..." and the paragraphs after it). Keep the existing notes about committing vs gitignoring the cache and about changing `system.md` or `--model`. Add or replace so the section states:

- The cache is bound to the output file it was built for: it records the normalized absolute `--output` path and a SHA-256 of the output bytes; a cached bullet is served only when both match.
- A run whose `--output` is a different file reads nothing, writes nothing, and warns naming the recorded output; that run recompiles everything. Scratch renders must pass `--no-cache`.
- When the recorded path matches but the output changed (for example a hand-edited bullet), or the cache predates the binding, `distill` rebuilds the cache from the committed output with zero `claude` calls and leaves the output untouched. Attribution is all-or-nothing; a heading or bullet-count mismatch falls back to a cold compile.
- A missing cache runs cold rather than rebuilding, so an edit made since the output was committed is not swallowed.
- The committed output is the reviewed artifact; a hand-edited bullet is adopted into the cache.
- Keep the existing `Incremental recompile` paragraph and the `--no-cache` note.
- Reconcile the two README sentences the new behavior contradicts. In `## Output shape`, replace "The output file is owned end-to-end by `distill`. Don't hand-edit it — your edits will be lost on the next regeneration. Edit the source rule files instead, then re-run." with "The output file is regenerated by `distill` on every run. Editing the source rule files and regenerating is the supported path; a hand-edited bullet is adopted into the cache on the next rebuild, but a rule edit or `--no-cache` replaces it." In `## Edit protection hook`, replace "Generated output is overwritten on every run, so hand-edits are silently lost. [`hooks/deny-generated-file-edits.sh`](hooks/deny-generated-file-edits.sh) is a Claude Code `PreToolUse` hook that prevents this:" with "A hand-edited bullet is adopted into the cache on the next rebuild rather than silently discarded. [`hooks/deny-generated-file-edits.sh`](hooks/deny-generated-file-edits.sh) is a Claude Code `PreToolUse` hook that keeps agents from editing generated files at all:"

Update `docs/adopting-distill.md` § Gotchas: replace "- **Never hand-edit the output.** Edits are clobbered on the next run. Edit the rule source, then regenerate." with "- **Prefer editing the rule source.** A hand-edited bullet is adopted into the cache on the next rebuild, but a rule edit or `--no-cache` replaces it; edit the rule source, then regenerate." In § 7 Enable edit protection, also replace "the single most common way to lose work with distill is editing the output, and the hook is what makes that mistake impossible rather than merely documented" with "a hand-edit to the output survives only until the next rule edit or --no-cache run, and the hook is what keeps that mistake from happening at all rather than merely documenting the convention".

Also correct the residual "from scratch" claims: in `## Output shape`, replace "`distill` writes the file from scratch every run:" with "`distill` writes the file every run, except a rebuild-from-output run, which leaves it byte-identical:".

Do not change the flag table or claim a new flag exists.

## 4. Update `CHANGELOG.md`

Add a `## Unreleased` section immediately after the introductory paragraph (`All notable changes ...`) and before the latest released version. Add one `fix:` bullet that describes the shipped behavior:

```
- fix: bind the content-hash cache to the output file it was built for. The cache now records the normalized absolute `--output` path and a SHA-256 of the output bytes, and serves entries only when both match. A run whose output differs from the recorded one reads no entries, writes no cache, and warns instead of re-seeding the shared cache from a scratch render. When the recorded path matches but the content hash is stale (or the cache predates the binding), distill rebuilds entries from the committed output with zero `claude` calls; attribution is all-or-nothing and falls back to a cold compile on a section/bullet count mismatch. The output header's `Regenerate:` line now records the normalized absolute path. `--no-cache` is unchanged.
```

Follow the changelog guide: the prefix is required, the bullet names the behavior, and no prompt filename or verification text is used as the entry.

</requirements>

<constraints>
- Documentation only. Do not change Go code, the cache schema, the CLI, the factory, or the cache path.
- `docs/spec.md` must not contradict the shipped behavior; where the old text is stale (the schema description, the missing/corrupt/version-mismatch row), correct it.
- Do not add a `--cache` flag or any other new flag to the docs.
- Keep the existing `## Cache` section heading exactly `## Cache` (the acceptance checks use it as an awk range delimiter).
- Do not write "integer" anywhere in the `## Cache` section.
- Do not restate the implementation in the README; describe operator-visible behavior.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run `make precommit` — it must exit 0.

CHANGELOG has a non-empty Unreleased section:
```
awk '/^## Unreleased/{f=1;next} /^## /{f=0} f && NF' CHANGELOG.md | wc -l
```
Returns ≥ 1.

The Cache section mentions the output binding:
```
awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -c output
```
Returns ≥ 1.

The Cache section contains the required rows and the corrected schema:
```
awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -n 'cache does not match the output'
awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -ci 'version.*rebuild'
awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -ci '^\*\*Schema:\*\*.*version.*entries'
! awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -qi 'integer'
! awk '/^## Cache$/{f=1;next} /^## /{f=0} f' docs/spec.md | grep -qi 'rules.*map'
```
The first must return a line, the second and third must return ≥ 1, and the fourth must produce no match (the leading `!` makes the absence assertion fail loudly if the word appears — `grep -c` prints 0 but exits 1, so its exit code is inverted for an absence check).

The schema fields are documented:
```
grep -n 'output_path\|output_hash' docs/spec.md
```
Returns lines.

The README covers scratch renders:
```
grep -in 'scratch renders' README.md
```
Returns a line.
</verification>

<completion_report_template>
Append the standard DARK-FACTORY-REPORT block with `status`, `verification.command`, and `verification.exitCode`. Then an `## Improvements` section (PROMPT / GUIDE / GLOBAL, or `- None`).
</completion_report_template>
