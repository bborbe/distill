---
status: rejected
tags:
    - dark-factory
    - spec
approved: "2026-06-26T16:16:26Z"
generating: "2026-06-26T16:16:26Z"
rejected: "2026-06-26T16:26:54Z"
rejected_reason: Design pivot — switch from verbatim TL;DR extraction to LLM-compile (call claude -p with all source rules per target/section). Idempotency via content-hash cache. Will rewrite spec.
branch: dark-factory/implement-distill-mvp
---

## Summary

- Implement the `distill` CLI end-to-end so it satisfies the user-facing contract already pinned in `docs/spec.md`.
- Cover source parsing, target alias resolution, marker scanning, writer (with stable sort + idempotency), and the CLI driver wiring all of it together.
- Wire `--check` (exit 3 on drift) and exit codes 0 / 1 / 2 per the contract.
- Verify with an end-to-end test replicating the worked example from `docs/spec.md` (three sources, two targets), plus an idempotency test (second run writes zero bytes).
- Leave for follow-up specs: pilot migration of `[[CLAUDE.md Rules]]`, regenerating real CLAUDE.md files, watch / daemon mode, Authoring Guide / runbook updates, the `--verbose` flag.

## Problem

`main.go` is a scaffolded stub that exits 1 with "not yet implemented". The behavioural contract for the tool — frontmatter shape, marker convention, target resolution, exit codes, error catalogue, idempotency rules, worked example — already lives in `docs/spec.md`. Nothing turns that contract into a working binary. Until a real implementation lands, `go install github.com/bborbe/distill@latest` produces a tool that prints an error and exits 1, the `distill` workflow cannot replace the manual "derive a TL;DR, paste into CLAUDE.md, bump date" loop documented in [[Update CLAUDE.md]], and the project cannot dogfood itself.

## Goal

`distill --source <dir>` reads a folder of per-rule markdown files, replaces the derived blocks between `<!-- begin:distill section="X" -->` / `<!-- end:distill section="X" -->` markers in each target file, and exits 0. Re-running on unchanged sources writes zero bytes and `--check` exits 0. On drift, `--check` exits 3 without modifying any file. All nine error cases enumerated in `docs/spec.md` are reachable and exit with the documented codes. The worked example from `docs/spec.md` runs end-to-end against on-disk fixtures and produces byte-exact output.

## Non-goals

- Do NOT migrate the real `[[CLAUDE.md Rules]]` page into per-rule source files — that is a separate pilot spec.
- Do NOT regenerate the real `~/.claude/CLAUDE.md` or vault `CLAUDE.md` against live sources — fixture-based tests only.
- Do NOT add watch / daemon mode — single-shot CLI per `docs/spec.md` "Non-goals".
- Do NOT implement `--verbose` — agent decides at impl time whether to land a stub; behaviour is not under test.
- Do NOT update the Authoring Guide, the `[[Update CLAUDE.md]]` runbook, or any vault-side documentation — follow-up spec.
- Do NOT add configurable aliases beyond `global` and `vault` — invariant per `docs/spec.md`; if a future consumer demands variation, that's a separate spec.
- Do NOT create target files when they are missing — invariant per `docs/spec.md` "Target Resolution"; missing target is exit 1.
- Do NOT validate markdown content quality, lint TL;DR bodies, or rewrite/normalise emitted text — TL;DR is emitted verbatim per `docs/spec.md` "TL;DR extraction".
- Do NOT implement atomic multi-target writes — partial-progress mid-run is acceptable per `docs/spec.md` "Non-goals".

## Desired Behavior

1. Running the binary against a source folder of well-formed rule files updates the marker blocks in each resolved target file so that the bytes between `<!-- begin:distill section="X" -->` and `<!-- end:distill section="X" -->` exactly match the sorted, verbatim-TL;DR output defined in `docs/spec.md` "Output Format". Content outside marker pairs is byte-identical before and after.
2. Source files without a `distill:` frontmatter block are skipped silently; source files with `disabled: true` are parsed but contribute no output.
3. Target resolution honours the alias table in `docs/spec.md` ("Target Resolution"): `global` → `~/.claude/CLAUDE.md` (with `~` expanded), `vault` → the value of `$DISTILL_VAULT_CLAUDE_MD`, paths starting with `/` or `~` resolved as absolute (with `~` expanded), other strings resolved against the CWD where the CLI was invoked.
4. Within each marker block, rules are sorted by `order` ascending, then by `id` ascending (stable). Default `order` is 100; default `id` is the source filename without the `.md` extension. Each TL;DR is emitted as one bullet with `- ` prefix on the first line; subsequent TL;DR lines are indented two spaces. The marker block ends with exactly one trailing newline before the `end:distill` marker.
5. Re-running the binary against unchanged sources writes zero bytes to every target. Concretely: file mtimes for unchanged targets do not advance, and byte content is identical to the prior run.
6. `--check` mode performs the same computation but never writes. It exits 0 when every target's would-write bytes equal its current bytes, exits 3 when any target's bytes would change, and prints the list of out-of-date target paths to stderr.
7. All nine error cases catalogued in `docs/spec.md` "Error Cases" are reachable and produce the documented exit code (1 for the eight error rows; warning + continue for the "orphan marker block with no source" case which is a warning row). Error messages name the offending source file, target path, or section as called out in each row.
8. The binary, when built and installed via `go install github.com/bborbe/distill@latest`, exposes the `distill --source <dir>` interface with `--check` and exits 2 on missing/invalid flags.

## Constraints

- `docs/spec.md` is the contract authority. The implementation conforms to it; if a behaviour cannot be implemented as written, `docs/spec.md` is updated first and the change is called out in the prompt's report — never silently diverge.
- Project conventions in `CLAUDE.md` and `docs/dod.md` hold: `github.com/bborbe/errors` for error wrapping; Ginkgo v2 + Gomega tests; Counterfeiter mocks; Interface → Constructor → Struct → Method pattern; factory functions are pure composition (no I/O, no `context.Background()`); external test packages (`*_test`); exported items carry GoDoc.
- `go.mod` must not gain `replace` or `exclude` directives — both break `go install`.
- LF line endings on emitted bytes; no trailing whitespace on emitted lines (per `docs/spec.md` "Idempotency Contract").
- The CLI flag surface (`--source`, `--check`, `--verbose`) and exit code table (`0` / `1` / `2` / `3`) are frozen by `docs/spec.md` and are not extended in this spec.
- Marker syntax is frozen: literal `<!-- begin:distill section="<name>" -->` / `<!-- end:distill section="<name>" -->`, exact match on `section=` attribute including quoting.
- `make precommit` must stay green.

## Failure Modes

| Trigger | Detection | Expected behavior | Reversibility | Recovery |
|---------|-----------|-------------------|---------------|----------|
| Source file missing `## TL;DR` | parse-time | exit 1, stderr names the offending source file | n/a (no writes performed before error) | author adds `## TL;DR` to the source file |
| Source file missing `distill:` frontmatter | parse-time | skipped silently, no error, no output | n/a | none — by design |
| Source `target: vault` but `$DISTILL_VAULT_CLAUDE_MD` unset | resolve-time | exit 1, stderr names the env var | n/a (no writes performed before error) | operator exports `DISTILL_VAULT_CLAUDE_MD` |
| Resolved target file does not exist | resolve-time | exit 1, stderr names the resolved path | n/a (no writes performed before error) | operator creates the target file with the required markers |
| Target file contains `begin:distill section="X"` with no matching `end:distill` | scan-time | exit 1, stderr names target + section + "orphan begin marker" | partial: targets processed earlier in the run remain written | operator closes the marker pair |
| Target file contains `end:distill section="X"` with no preceding `begin:distill` | scan-time | exit 1, stderr names target + section + "orphan end marker" | partial: targets processed earlier in the run remain written | operator removes the stray end marker |
| Source's `section:` has no matching marker pair in the resolved target | match-time | exit 1, stderr names source file + target + section | partial: targets processed earlier in the run remain written | operator adds the marker pair to the target |
| Two source files resolve to identical `(target, section, order, id)` | match-time | exit 1, stderr names both source files | n/a (no writes performed before error) | operator changes one source's `order` or `id` |
| Marker pair in target with no source claiming that section | write-time | warning to stderr; marker block emitted empty (not an error) | reversible by re-running after sources or markers are reconciled | operator either adds a source or removes the marker pair |
| IO error mid-run on a target write | write-time | exit 1, stderr names the target + underlying error | partial: targets written before the failing one remain mutated; failing target is not partially written (writer commits whole file or aborts) | operator inspects, fixes underlying cause, re-runs |
| Source folder does not exist or is unreadable | startup | exit 1, stderr names the path | n/a (no writes performed before error) | operator corrects `--source` flag |
| `--source` flag missing | startup | exit 2, stderr prints usage | n/a | operator supplies `--source` |

## Security / Abuse Cases

- Attacker-controlled input is the source-folder file contents (YAML frontmatter, TL;DR body) and the `$DISTILL_VAULT_CLAUDE_MD` env var. Both are operator-trusted in this tool's threat model (the operator owns both the source repo and the env), but:
- A source `target:` value that resolves outside the operator's intent is constrained by the alias table — only `global`, `vault`, or explicit paths are honoured. The implementation does not interpret arbitrary strings as paths beyond what `docs/spec.md` "Target Resolution" describes.
- The implementation does not follow symlinks in the source folder unless they resolve to readable `.md` files; it does not traverse outside the declared source folder. No recursion into subfolders is in scope for this spec (per `docs/spec.md` "Non-goals" → "Rule discovery beyond the declared source folder"). Agent decides at impl time whether to flatten subfolders or error on them — but the decision must be explicit and tested.
- Path expansion of `~` only occurs at the start of a target value; it is not re-expanded inside the path.
- The writer never executes content from source or target files; it treats both as text.

## Acceptance Criteria

- [ ] `make precommit` exits 0 in the project root — evidence: exit code 0.
- [ ] An end-to-end test in an external `*_test` package replicates the worked example from `docs/spec.md` "Worked Example" (three source files: `git-no-c-flag.md`, `git-no-claude-attribution.md`, `obsidian-no-h1.md`; two target files matching the "Target before run" snippets) and asserts byte-exact match against the "Target after run" snippets — evidence: Ginkgo test passes; the test fixture file contents are committed under a `testdata/` directory.
- [ ] An idempotency test re-runs the same compile against the post-run targets and asserts every target's bytes are unchanged — evidence: `diff` between post-run-1 and post-run-2 target bytes is empty, asserted in test.
- [ ] `--check` exits 0 when targets are up to date — evidence: Ginkgo test invokes the CLI driver in `--check` mode against up-to-date fixtures and asserts the returned exit code is 0.
- [ ] `--check` exits 3 when at least one target would change, and writes nothing — evidence: Ginkgo test asserts exit code 3 AND asserts target file bytes are byte-identical to pre-run.
- [ ] Error row 1 — source file missing `## TL;DR`: unit test asserts exit 1 and stderr names the offending source file — evidence: Ginkgo assertion on exit code + stderr substring.
- [ ] Error row 2 — `target: vault` with `$DISTILL_VAULT_CLAUDE_MD` unset: unit test asserts exit 1 and stderr names the env var — evidence: Ginkgo assertion on exit code + stderr substring.
- [ ] Error row 3 — target file does not exist: unit test asserts exit 1 and stderr names the resolved path — evidence: Ginkgo assertion on exit code + stderr substring.
- [ ] Error row 4 — orphan `begin:distill` (no matching `end:distill`): unit test asserts exit 1 and stderr names target + section + "orphan begin marker" — evidence: Ginkgo assertion on exit code + stderr substring.
- [ ] Error row 5 — orphan `end:distill` (no preceding `begin:distill`): unit test asserts exit 1 and stderr names target + section + "orphan end marker" — evidence: Ginkgo assertion on exit code + stderr substring.
- [ ] Error row 6 — source `section:` has no matching marker pair in resolved target: unit test asserts exit 1 and stderr names source + target + section — evidence: Ginkgo assertion on exit code + stderr substring.
- [ ] Error row 7 — two source files resolve to identical `(target, section, order, id)`: unit test asserts exit 1 and stderr names both source files — evidence: Ginkgo assertion on exit code + stderr substring.
- [ ] Error row 8 — IO error mid-run on a target write: unit test (against a read-only target or simulated write failure) asserts exit 1 and stderr names the target + underlying error — evidence: Ginkgo assertion on exit code + stderr substring.
- [ ] Warning row — marker pair in target with no source claiming that section: unit test asserts exit 0, marker block emitted empty, and a warning appeared on stderr — evidence: Ginkgo assertion on exit code + emitted bytes between markers + stderr substring.
- [ ] Exit code 2 is reachable when `--source` is missing — evidence: Ginkgo test invokes the CLI driver with no flags and asserts exit code 2.
- [ ] Stable sort holds: a test with two rules sharing the same `order` asserts the rule with the lexicographically smaller `id` appears first in the emitted block — evidence: Ginkgo test assertion on emitted bytes.
- [ ] Verbatim TL;DR holds: a source TL;DR containing a multi-line body with embedded backticks, em-dashes, and a code fence is emitted with the first line prefixed `- ` and subsequent lines indented two spaces, with no rewrap or character substitution — evidence: Ginkgo test assertion on emitted bytes.
- [ ] Target resolution: tests cover `global`, `vault` (with `$DISTILL_VAULT_CLAUDE_MD` set), `vault` with the env unset (exit 1), a `~`-prefixed path, an absolute path, and a relative path resolved against a controlled CWD — evidence: Ginkgo tests assert the resolved absolute path for each form.
- [ ] Source files without `distill:` frontmatter are skipped silently — evidence: Ginkgo test places one such file in the source folder, runs the CLI, and asserts exit code 0 with no mention of the file on stderr.
- [ ] Source files with `disabled: true` are parsed but not emitted — evidence: Ginkgo test asserts the disabled rule's TL;DR does not appear in the target's marker block; a non-disabled rule in the same section still appears.
- [ ] Content outside marker pairs is preserved byte-for-byte across a run — evidence: Ginkgo test asserts bytes before the first `begin:distill` and after the last `end:distill` are unchanged, including trailing newlines.
- [ ] Installable via `go install` — `go build ./...` exits 0; `go.mod` contains no `replace` or `exclude` directives — evidence: build exit code 0 + `grep` on `go.mod` returns no matches.

## Verification

```
cd ~/Documents/workspaces/distill
make precommit
```

Plus a manual smoke against `testdata/`:

```
cd ~/Documents/workspaces/distill
go run . --source ./testdata/worked-example/sources --check
# expect exit 0 against the committed post-run fixtures
```

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Source parser + target resolver (read-only halves: parse `.md` files with `distill:` frontmatter, extract TL;DR, resolve `target:` to absolute path including `global` / `vault` / `~` / relative). Unit tests only; no CLI wiring yet. | 2, 3 | "Source files without `distill:`", "Source files with `disabled:`", "Target resolution" | — |
| 2 | Marker scanner + writer (parse target files into prose / marker-block regions, replace marker contents with sorted-verbatim bullets, idempotent byte emission). Unit tests against in-memory inputs. | 1, 4, 5 | "Stable sort", "Verbatim TL;DR", "Content outside marker pairs is preserved", idempotency test | prompt 1 (consumes parsed-rule and resolved-target types) |
| 3 | CLI driver + end-to-end test + error catalogue (wire parser → resolver → scanner → writer; `--check` mode; exit-code mapping for all nine error rows; worked-example E2E with `testdata/`). | 6, 7, 8 | "End-to-end test", `--check` exits 0 / 3, error-row coverage, exit code 2, `go install`, `make precommit` | prompts 1 + 2 |

Rationale: prompt 1 ships the pure-read layer (no target file mutation) so its tests are fast and its types stabilise before any writer touches them. Prompt 2 builds the byte-emission layer in isolation against fixed inputs, which is where the idempotency contract is enforced and where the highest churn risk lives. Prompt 3 wires CLI + error mapping + worked-example E2E once the two halves below it are green. Splitting this way avoids the writer being rewritten when the parser's exported types shift, and avoids the CLI being rewritten when the marker scanner's region shape shifts.

## Do-Nothing Option

Today the binary exits 1 with "not yet implemented". The fallback is the manual workflow in [[Update CLAUDE.md]] — derive a TL;DR by hand, paste into the matching `CLAUDE.md` section, bump the date. That workflow stays viable but: it does not catch drift between source long-form rules and pasted one-liners, it has no `--check` for CI, and every new rule costs a manual derivation step. Not building this spec means the dark-factory pilot for compiled CLAUDE.md files stalls and the [[Update CLAUDE.md]] runbook stays as the only mechanism. Acceptable only if the rule corpus stops growing.
