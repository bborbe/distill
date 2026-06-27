---
status: approved
tags:
    - dark-factory
    - spec
approved: "2026-06-26T17:18:18Z"
generating: "2026-06-27T09:31:20Z"
branch: dark-factory/implement-distill-llm-compile
---

## Summary

- Turn the `distill` stub binary into a working CLI that satisfies the rewritten `docs/spec.md` (LLM-compile design).
- Per (target, section) group, the binary bundles full source bodies into a single prompt and sends it to `claude --print`; the returned compressed block is written between the section's markers.
- All target alias resolution, marker scanning, error catalogue, and exit codes from `docs/spec.md` are reachable; operator prose outside markers is preserved byte-exact.
- The Claude subprocess is wrapped behind a mockable interface so unit + end-to-end tests run offline and deterministically (no real `claude` calls in tests).
- Out of scope (separate specs): caching / idempotency / `--check`, watch mode, vault pilot migration, real-CLAUDE.md regeneration, subfolder recursion, atomic multi-target writes.

## Problem

`main.go` is a scaffolded stub that exits 1 with "not yet implemented". The behavioural contract for `distill` — frontmatter shape, marker convention, target resolution, exit codes, error catalogue, Claude invocation flags, worked example — already lives in `docs/spec.md`. Nothing turns that contract into a working binary. Until a real implementation lands, `go install github.com/bborbe/distill@latest` produces a tool that prints an error and exits 1, and the project cannot replace the manual "derive a one-liner by hand → paste into CLAUDE.md → bump date" loop that `docs/spec.md` was written to retire.

A prior implementation spec (rejected `001-implement-distill-mvp.md`) targeted a verbatim-TL;DR extraction design; `docs/spec.md` has since been rewritten to call `claude` per group and let the LLM do the compression. This spec replaces that rejected one against the new contract.

## Goal

`distill --source <dir>` walks the source folder, parses every `.md` file's `distill:` frontmatter (silently skipping files without one), groups the parsed rules by `(target, section)`, builds one prompt per group from the embedded system instruction plus the delimited source bodies in sort order, invokes `claude --print` once per group with the prompt on stdin, parses the stream-JSON output for the final `result`, and writes the compressed block between the section's markers in the resolved target file. Operator prose outside markers is preserved byte-exact. Every error row in `docs/spec.md` "Error Cases" is reachable and exits with the documented code (0 / 1 / 2). The worked example in `docs/spec.md` runs end-to-end against on-disk fixtures using a stubbed Claude runner and produces the documented output.

## Non-goals

- Do NOT add caching, content-hash idempotency, or a `--check` mode — explicitly future per `docs/spec.md` "Non-goals (v1)".
- Do NOT add watch / daemon mode — invariant per `docs/spec.md`; if a future consumer demands it, that's a separate spec.
- Do NOT create target files when missing — invariant per `docs/spec.md` "Target Resolution"; missing target is exit 1.
- Do NOT extend target aliases beyond `global` and `vault` — invariant per `docs/spec.md`; if a future consumer demands variation, that's a separate spec.
- Do NOT recurse into subfolders of `--source` — flat folder only in v1; explicitly tested and documented as a limitation. (If a consumer asks for recursion later, that's a separate spec.)
- Do NOT migrate the vault `[[CLAUDE.md Rules]]` page into per-rule files — separate pilot spec.
- Do NOT regenerate the real `~/.claude/CLAUDE.md` or vault CLAUDE.md against live sources — fixture-based tests only; the operator runs the binary manually after merge.
- Do NOT update `[[CLAUDE.md Authoring Guide]]` or the `[[Update CLAUDE.md]]` runbook in this spec — follow-up vault work.
- Do NOT implement atomic multi-target writes — partial-progress mid-run is acceptable; failed groups exit 1 with the failure named.
- Do NOT handle `claude` CLI authentication, model availability, or API key surfaces — the `claude` CLI owns those via its own config (`$CLAUDE_CONFIG_DIR`); `distill` just invokes it.
- Do NOT add a `--dry-run` flag, an opt-out for the LLM call, or any alternative compression path — invariant; the binary's entire purpose is calling the LLM. If a future consumer demands variation, that's a separate spec.
- Do NOT add content quality validation, lint, or rewriting of either source bodies or compressed output — both pass through untouched (source goes verbatim into the prompt; LLM output is written verbatim between markers).

## Desired Behavior

1. Running the binary against a source folder of well-formed rule files invokes the Claude runner exactly once per `(target, section)` group, with a prompt built from the embedded system instruction plus the source bodies delimited in `order` ascending then `id` ascending order, and writes the runner's returned text verbatim (modulo one leading and one trailing newline) between the section's `<!-- begin:distill section="…" -->` / `<!-- end:distill section="…" -->` markers in the resolved target file. Bytes outside marker pairs are identical before and after.
2. Source files without a `distill:` frontmatter block are skipped silently. Source files with `disabled: true` are parsed but contribute neither to any prompt nor to any output. Files that are not `.md` are ignored.
3. Target resolution honours the alias table in `docs/spec.md` "Target Resolution": `global` → `~/.claude/CLAUDE.md` (with `~` expanded); `vault` → `$DISTILL_VAULT_CLAUDE_MD` (error exit 1 if unset); paths starting with `~` are expanded; absolute paths are used as-is; other strings are resolved against the CWD where the CLI was invoked.
4. Marker scanning of each resolved target file partitions the file into a sequence of (prose, marker-pair) regions. Multiple marker pairs in one target file are supported and their on-disk order is preserved across the write. Orphan `begin:` or `end:` markers (unmatched) cause exit 1 with the target path, section, and orphan kind named on stderr.
5. The Claude subprocess is invoked exactly as specified in `docs/spec.md` "Claude Invocation": `claude --print --output-format stream-json --verbose --strict-mcp-config --model <model>`, with the prompt on stdin, the model defaulting to `sonnet`, and the runner parses the stream-JSON to extract the final `result` event's `result` field, trims trailing whitespace, and returns the body. Missing `claude` binary on `$PATH` is exit 1 with the binary named. Non-zero exit from `claude` is exit 1 with the group name and a tail of `claude`'s stderr.
6. Every row of `docs/spec.md` "Error Cases" is reachable from the binary and produces the documented exit code: skipped-silently for missing frontmatter; exit 1 for unset `$DISTILL_VAULT_CLAUDE_MD`, missing target file, orphan marker, missing-section-for-source, duplicate `(target, section, order, id)`, `claude` non-zero, `claude` missing, source folder missing/unreadable; warning-to-stderr (no error) for marker-block-with-no-source (block emitted empty); exit 2 for missing `--source` flag.
7. The binary supports `--verbose`: when set, the full prompt and the raw stream-JSON response (or at minimum the final assembled compressed body — agent decides at impl time which fidelity level) for every group is written to stderr, each section prefixed with a clear delimiter naming the `(target, section)` group. When unset, stderr is silent on success.

## Constraints

- `docs/spec.md` is the contract authority. The implementation conforms to it; if a behaviour cannot be implemented as written, `docs/spec.md` is updated first and the change is called out in the implementation prompt's report — never silently diverge.
- Project conventions in `CLAUDE.md` and `docs/dod.md` hold: `github.com/bborbe/errors` for error wrapping; Ginkgo v2 + Gomega with Counterfeiter-generated mocks; Interface → Constructor → Struct → Method ordering; factory functions are pure composition (no I/O, no `context.Background()`); external test packages (`*_test`); exported types/functions/interfaces carry GoDoc.
- `go.mod` must not gain `replace` or `exclude` directives — both break `go install github.com/bborbe/distill@latest`.
- LF line endings on emitted bytes. Prose outside markers is preserved byte-for-byte (including trailing newlines, blank lines, indentation).
- The CLI flag surface (`--source`, `--model`, `--verbose`) and exit code table (`0` / `1` / `2`) are frozen by `docs/spec.md` and are not extended in this spec. There is no exit code `3` (idempotency is not a v1 concern).
- Marker syntax is frozen: literal `<!-- begin:distill section="<name>" -->` / `<!-- end:distill section="<name>" -->`, exact match on `section=` attribute including quoting.
- The Claude subprocess flags and env-layering pattern follow `bborbe/agent/claude.ClaudeRunner` (`~/Documents/workspaces/agent/claude/claude-runner.go`). Embedded prompt fragments follow `bborbe/agent-claude/pkg/prompts` (`//go:embed *.md`). These are reference implementations to mirror, not consumed as libraries.
- The Claude runner is behind a `Runner` interface so unit and end-to-end tests inject a stub; no test invokes the real `claude` binary.
- `docs/dod.md` currently mentions idempotency and exit code `3`; both contradict the rewritten `docs/spec.md`. `docs/dod.md` MUST be updated (in the same change set as this implementation) so the DoD reviewer no longer demands idempotency or exit code `3`. Failure to update `docs/dod.md` is itself a contract violation.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection | Reversibility |
|---|---|---|---|---|
| `claude` binary not on `$PATH` | Exit 1; stderr names the binary | Operator installs `claude` and re-runs | Exit code 1 + stderr message | Reversible (no writes performed) |
| `claude` exits non-zero on a group | Exit 1; stderr names the `(target, section)` group and the tail of `claude` stderr | Operator inspects, fixes auth / quota / source, re-runs | Exit code 1 + stderr message | Partial: groups processed before the failing one have already been written |
| `claude` returns non-JSON or malformed stream-JSON | Exit 1; stderr names the group and a tail of the raw stdout | Operator inspects; likely Claude CLI version mismatch | Exit code 1 + stderr message | Partial as above |
| Target file modified mid-run by operator | The write replaces the on-disk file; concurrent edits are lost | Operator restores from VCS / backup | Per-target temp-file + atomic rename keeps each individual target internally consistent (no half-written file). Cross-process concurrent edits to the same target are last-writer-wins — beyond `distill`'s scope. | Reversible via VCS |
| Two source files with the same `(target, section, order, id)` | Exit 1 before any Claude call; stderr names both source paths | Operator changes one source's `id` or `order` | Exit code 1 + stderr message | Reversible (no writes performed) |
| `$DISTILL_VAULT_CLAUDE_MD` unset but a source declares `target: vault` | Exit 1 before any Claude call; stderr names the env var | Operator exports the env var or removes the source | Exit code 1 + stderr message | Reversible (no writes performed) |
| Source folder missing or unreadable | Exit 1; stderr names the path | Operator fixes `--source` value | Exit code 1 + stderr message | Reversible (no writes performed) |
| Orphan `begin:` or `end:` marker in a target file | Exit 1 before any Claude call for affected target; stderr names target + section + orphan kind | Operator fixes the target file's markers | Exit code 1 + stderr message | Reversible for that target (no writes); other targets may have already been written |
| `claude` hangs indefinitely | Subprocess inherits caller cancellation via `exec.CommandContext`; SIGINT / SIGTERM to `distill` propagates | Operator Ctrl-C; re-runs | Process appears hung; user kills | Partial: groups processed before kill are written |
| Source body with no closing newline | Treated as text; delimiter line after the body still appears on its own line in the assembled prompt | None needed | N/A | N/A |

## Security / Abuse Cases

- `--source` accepts an operator-supplied path; `distill` walks it with the operator's credentials. Out of scope to sandbox.
- `target:` strings come from source files (operator-authored) and can resolve to arbitrary paths the operator has write access to. This is by design; the security boundary is "the operator trusts their own source folder".
- `$DISTILL_VAULT_CLAUDE_MD` is read but never written. No injection surface.
- Source bodies are sent verbatim to `claude --print`. The operator is responsible for not putting secrets in source rule files; `distill` does not redact.
- The `claude` subprocess inherits a filtered env (allowlist + `CLAUDE_CONFIG_DIR` overrides per the `bborbe/agent` reference); secrets in the parent env are not blindly forwarded unless the allowlist explicitly permits them.
- Process exit on cancellation must not leave a partially written target file (use write-to-temp-then-rename within a single target write). Cross-target atomicity is explicitly not provided.

## Acceptance Criteria

- [ ] `make precommit` exits 0 — evidence: exit code 0
- [ ] `go install github.com/bborbe/distill@latest` succeeds against the merged branch — evidence: build exits 0, binary appears on `$GOPATH/bin`
- [ ] `go.mod` contains no `replace` or `exclude` directive — evidence: `grep -E '^(replace|exclude)' go.mod` exits non-zero (no match)
- [ ] An end-to-end test in an external `*_test` package replicates the `docs/spec.md` "Worked Example" against on-disk fixtures with a stubbed `Runner` that returns a fixed compressed bullet list per prompt, and asserts the post-run target file equals the documented expected output byte-for-byte — evidence: Ginkgo test passes; assertion is `Expect(actualBytes).To(Equal(expectedBytes))`
- [ ] In the same end-to-end test, operator prose outside the marker pair is asserted byte-identical to the pre-run target — evidence: substring assertion on the pre-marker and post-marker regions matches the pre-run bytes exactly
- [ ] A unit test on the source parser asserts that a file missing the `distill:` frontmatter block is silently skipped (no rule emitted, no error) — evidence: parser returns zero rules and nil error
- [ ] A unit test on the source parser asserts that `disabled: true` rules parse but are filtered before prompt assembly — evidence: parser returns the rule; downstream group has zero rules to send
- [ ] A unit test on the target resolver asserts each row of the alias table from `docs/spec.md` "Target Resolution" maps to the expected absolute path, and that `target: vault` with `$DISTILL_VAULT_CLAUDE_MD` unset returns an error wrapping the env var name — evidence: table-driven Ginkgo `DescribeTable` passes; error case `errors.Is` or substring match on `DISTILL_VAULT_CLAUDE_MD`
- [ ] A unit test on the marker scanner asserts that a target file with two `(begin, end)` pairs partitions into the expected (prose, pair, prose, pair, prose) sequence and round-trips back to the original bytes when no body is replaced — evidence: assembled output equals input bytes
- [ ] A unit test on the marker scanner asserts that an orphan `begin:` (no matching `end:`) and an orphan `end:` (no matching `begin:`) each produce an error naming the target, section, and orphan kind — evidence: error message substring match on each of (target path, section name, "begin" or "end")
- [ ] A unit test on the prompt builder asserts that rules within a group are concatenated in `order` ascending then `id` ascending order, each delimited by a line containing the rule's `id` — evidence: built prompt string contains delimiters in the expected order; assertion compares against a golden string
- [ ] A unit test on the prompt builder asserts that the embedded system instruction (from `pkg/prompts/system.md`) appears as the prompt's prefix — evidence: `strings.HasPrefix` assertion against the embedded file's contents
- [ ] A unit test on the Claude runner asserts the subprocess is invoked with the exact arg vector `["--print", "--output-format", "stream-json", "--verbose", "--strict-mcp-config", "--model", "<model>"]` — evidence: a fake `exec.Cmd` constructor (or a test-only seam) captures the args and the test asserts on the captured slice
- [ ] A unit test on the Claude runner parses a fixture stream-JSON byte stream (representative of `claude --print --output-format stream-json --verbose` output) and asserts the returned string equals the final `result` event's `result` field with trailing whitespace trimmed — evidence: returned string equals the golden expected body
- [ ] A unit test on the Claude runner asserts that if the `claude` binary is not on `$PATH`, the returned error names the binary — evidence: error substring contains `claude`
- [ ] A unit test on the writer asserts that, given a pre-parsed target and a map of `section → compressed body`, the written bytes have each section's marker contents replaced (leading + trailing newline framing the body) and bytes outside markers byte-identical — evidence: pre/post byte comparison on prose regions equals the input; marker region equals the expected framed body
- [ ] A unit test on the writer asserts atomic per-target write semantics: the target file path either contains the fully assembled new bytes or the original bytes after a simulated mid-write abort, never a truncated mix — evidence: after injecting a write error in the seam, `os.ReadFile(target)` returns the original bytes
- [ ] An integration test on the CLI driver asserts that running the binary with `--source` pointing at a fixture folder where `target: vault` is declared but `$DISTILL_VAULT_CLAUDE_MD` is unset exits with code 1 and stderr contains `DISTILL_VAULT_CLAUDE_MD` — evidence: process exit code 1, stderr substring match
- [ ] An integration test on the CLI driver asserts that missing `--source` exits with code 2 and stderr contains usage text — evidence: process exit code 2, stderr substring match (e.g. `Usage:` or `--source`)
- [ ] An integration test on the CLI driver asserts that a target file with a marker pair whose section is not claimed by any source emits a warning to stderr naming the section and leaves the marker block empty in the written file — evidence: process exit code 0, stderr substring match on the section name, marker body in output file is exactly two newlines
- [ ] An integration test on the CLI driver asserts that two source files sharing identical `(target, section, order, id)` produces exit code 1 and stderr names both source paths — evidence: process exit code 1, stderr substring match on both source filenames
- [ ] A unit / integration test on `--verbose` asserts that with the flag set, the assembled prompt and the runner's returned body for each group appear on stderr; with the flag unset, stderr is empty on success — evidence: stderr substring match on a known prompt fragment when `--verbose` is set; stderr is empty bytes when not set
- [ ] The Claude runner exposes a Counterfeiter-generated fake (e.g. `mocks/runner.go`) and the end-to-end test imports and configures this fake — evidence: fake file exists at the expected path; test code uses `fakeRunner.RunReturns(...)` (or equivalent)
- [ ] `docs/dod.md` is updated in the same change set to remove the idempotency requirement and the exit code `3` reference, so the DoD reviewer's contract matches the rewritten `docs/spec.md` — evidence: `grep -n 'exit code.*3\|idempoten' docs/dod.md` returns no matches; CHANGELOG.md gains an `## Unreleased` entry noting the dod.md alignment

Scenario coverage — default: NO new scenario. The end-to-end test above runs in-process against a stubbed Claude runner and covers the worked example. No real-`claude` scenario is added; the regression risk for the subprocess shape is covered by the Claude runner unit test that asserts the exact arg vector. Adding a real-`claude` scenario would require live network + auth and would be flaky; deferred until a future spec demands it.

## Verification

Run from repo root after merge:

```
make precommit
```

Expected: exit 0, all Ginkgo specs pass, lint clean.

Manual smoke (optional, operator-side; not gating):

```
mkdir -p /tmp/distill-smoke && cat > /tmp/distill-smoke/git-no-c.md <<'EOF'
---
distill:
  target: /tmp/distill-smoke-target.md
  section: Git
  order: 10
---
# Rule: No git -C
git -C breaks the matcher. Use cd then git.
EOF
cat > /tmp/distill-smoke-target.md <<'EOF'
## Git

<!-- begin:distill section="Git" -->
<!-- end:distill section="Git" -->
EOF
distill --source /tmp/distill-smoke
cat /tmp/distill-smoke-target.md   # marker body now populated
```

Expected: exit 0; marker body contains an LLM-produced bullet list; prose outside markers unchanged.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Pure read-side parsing: source folder walk + frontmatter parse, target alias resolution, marker scanner (partition + orphan detection). All in-process, no subprocess, no I/O writes. Counterfeiter fakes only where the package boundary exposes one. | 2, 3, 4 | source parser ACs (2), target resolver AC, marker scanner ACs (2) | — |
| 2 | LLM-call seam: embedded `pkg/prompts/system.md`, prompt builder (sort + delimiters + system-prompt prefix), Claude runner interface + concrete subprocess impl + Counterfeiter fake, stream-JSON parsing, missing-binary error path. Pure unit tests with captured exec.Cmd args; no real `claude` invocation. | 1 (partial — prompt assembly + runner contract), 5 | prompt builder ACs (2), Claude runner ACs (3), fake-exists AC | prompt 1 (uses parsed Rule types) |
| 3 | CLI driver + writer + end-to-end test: writer (replace marker contents, atomic per-target temp+rename), CLI flag parsing + exit code mapping, full pipeline wiring (parser → resolver → scanner → grouper → builder → runner → writer), worked-example E2E test with stubbed Runner, `--verbose` plumbing, error-catalogue integration tests, `docs/dod.md` cleanup. | 1 (full), 6, 7 | writer ACs (2), CLI integration ACs (4), E2E AC, verbose AC, dod.md AC, `make precommit` AC, `go install` AC, `go.mod` AC | prompts 1 and 2 |

Rationale: layer 1 is pure pure-function parsing with no subprocess and no LLM-shaped assumptions, so it lands first and unblocks the others. Layer 2 introduces the embedded prompt + the subprocess seam in isolation so its unit tests don't need the CLI wiring. Layer 3 is integration: it depends on the typed outputs of layers 1 and 2 and the Runner interface, and it owns the end-to-end test, the writer's atomic-rename, and the operator-facing exit-code mapping. Splitting writer + driver into one prompt (rather than two) keeps the end-to-end test and the exit-code integration tests in the same change set, avoiding a cross-prompt cycle where the E2E test would otherwise live in prompt 4.

## Do-Nothing Option

The binary stays a stub. `go install github.com/bborbe/distill@latest` produces a tool that exits 1. The vault `[[Update CLAUDE.md]]` runbook continues to require manual TL;DR derivation and paste — the friction `docs/spec.md` was written to retire stays in place. The rewritten `docs/spec.md` describes an unimplemented contract; documentation drifts from reality the longer this stays open. Not acceptable.
