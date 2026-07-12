---
status: completed
spec: [001-harden-compile-pipeline]
summary: 'Rewrote docs/spec.md to v3, scrubbed stale marker references, added UsageError+ranRunE exit-2 fix, added --no-cache to README, extended CHANGELOG ## Unreleased, and added pkg/cli test suite covering UsageError.'
execution_id: distill-harden-exec-006-docs-and-cleanup
dark-factory-version: v0.191.4
created: "2026-07-12T09:10:00Z"
queued: "2026-07-12T15:24:13Z"
started: "2026-07-12T16:02:25Z"
completed: "2026-07-12T16:09:05Z"
branch: dark-factory/harden-compile-pipeline
---

<summary>
- The behavior contract (`docs/spec.md`) is brought up to date: it documents the content-hash cache, the batched id-keyed model call, per-id validation with fail-loud retry, and the anti-injection invocation contract, plus the new error cases.
- Stale "marker" language left over from the abandoned marker-addressing design is scrubbed from `main.go` and `docs/dod.md`.
- The README gains the new cache file, the `--no-cache` flag, and a note that changing the compression instructions forces a cold recompile.
- A missing required flag now exits with the usage code 2 (distinct from a runtime failure exit 1), matching the documented exit-code table.
- The changelog records the hardening under an `## Unreleased` section (a minor bump toward v0.3.0).
- No behavior of the compile pipeline itself changes here — this is documentation plus the small exit-code correction.
</summary>

<objective>
Finish the hardening spec: rewrite `docs/spec.md` to v3 (cache, batching, validation, invocation contract, new error rows), scrub stale marker references from `main.go` and `docs/dod.md`, update the README, make a missing-required-flag exit 2 (usage) distinct from runtime exit 1, and add the `## Unreleased` CHANGELOG entry. Implements the exit-code-2, CHANGELOG, and docs/spec.md-v3 ACs.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions.
Read `/workspace/specs/in-progress/001-harden-compile-pipeline.md` — the whole spec; especially the Failure Modes table (new error rows), Constraints ("Exit 2 becomes reachable for usage errors (currently cobra required-flag errors map to 1)"), and the docs/exit-code/CHANGELOG ACs.

Read these files fully before editing:
- `/workspace/docs/spec.md` — currently "v2"; you rewrite to v3. Preserve the frozen contract sections (frontmatter, section grouping/ordering, atomic write, header/title shape) and ADD cache/batching/validation/invocation.
- `/workspace/main.go` — its package doc comment says "writing the returned bullets between fenced markers" — stale marker reference to fix.
- `/workspace/docs/dod.md` — lines referencing "marker convention" and "lands between markers … operator prose outside markers preserved" — stale marker references to fix.
- `/workspace/pkg/distill/doc.go` — package comment also references "fenced markers" / "between the section's markers" / "operator prose outside markers" — stale; fix to describe the current whole-file-regeneration + per-rule-bullet model.
- `/workspace/pkg/cli/cli.go` — cobra `Run`/`Execute`; `MarkFlagRequired("source")` / `("output")`. Missing-required-flag error currently maps to exit 1 via `distill.ExitCode`.
- `/workspace/pkg/distill/driver.go` — `ExitCode(err) int` (nil→0, else→1).
- `/workspace/README.md` — update for cache + `--no-cache`.
- `/workspace/CHANGELOG.md` — add `## Unreleased`.

Read these coding-plugin docs (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-cli-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/documentation-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/readme-guide.md`
</context>

<requirements>

## 1. Exit-code-2 for usage errors

The current path: a missing `--source`/`--output` makes cobra return an error from `ExecuteContext`; `Execute` maps it via `distill.ExitCode` → 1. Requirement: a USAGE error (flag parsing / required-flag failure — cobra returns before `RunE` runs) must exit 2; a RUNTIME error (from inside `RunE`, i.e. the driver) stays exit 1.

Implement via a `ranRunE` sentinel (per the spec's Suggested Decomposition):
- In `pkg/cli/cli.go`, add a `ranRunE bool` captured by the `RunE` closure; set `ranRunE = true` as the FIRST statement inside `RunE` (so we know cobra reached the command body, meaning flags parsed OK).
- After `rootCmd.ExecuteContext(ctx)` returns an error, decide the code: if `!ranRunE`, the error is a usage/parse error → return it wrapped/tagged so `Execute` exits 2; if `ranRunE`, it is a runtime error → exit 1.
- Preferred mechanism: have `Run(ctx, args)` return `(error, bool)` where the bool is `ranRunE`, OR return a typed error. Choose ONE:
  - **Chosen approach:** change `Run` to return the error AND expose whether RunE ran. Simplest: `Run` returns a `*UsageError` wrapper when `!ranRunE && err != nil`. Define in `pkg/cli`:
    ```go
    // UsageError marks a flag-parsing / required-flag failure that must exit 2.
    type UsageError struct{ Err error }
    func (e *UsageError) Error() string { return e.Err.Error() }
    func (e *UsageError) Unwrap() error { return e.Err }
    ```
  - In `Run`, after `ExecuteContext`, if `err != nil && !ranRunE { return &UsageError{Err: err} }`; else return `err`.
  - In `distill.ExitCode` (driver.go), add: if the error is a `cli.UsageError` this creates an import cycle (cli imports distill). AVOID the cycle: do the exit-code decision in `Execute` (pkg/cli) instead. In `Execute`:
    ```go
    if err := Run(ctx, os.Args[1:]); err != nil {
        fmt.Fprintf(os.Stderr, "distill: %v\n", err)
        var ue *UsageError
        if errors.As(err, &ue) {
            os.Exit(2)
        }
        os.Exit(distill.ExitCode(err))
    }
    ```
    (Use `errors` = the stdlib `errors` package for `errors.As`, aliased distinctly from `github.com/bborbe/errors` if both are needed — in cli.go stdlib `errors` is fine here.) Leave `distill.ExitCode` unchanged (nil→0, else→1); usage→2 is decided in `Execute`.

## 2. Rewrite `docs/spec.md` to v3

Change the header to `# distill — Specification (v3)`. PRESERVE these frozen sections unchanged in substance (frontmatter contract, source file format, output structure + ordering rules, atomic write, header/title shape, silent-skip, duplicate detection). UPDATE/ADD:

2a. **Compression Prompt** section: rewrite to describe the batched id-keyed call — system instructions travel out-of-band via `--system-prompt` (never in the user prompt); rule bodies are wrapped as `<rules><rule id="…">verbatim body</rule>…</rules>` inert data; a literal `</rule>` in a body is a build-time error (exit 1) naming the file; the model returns `--- bullet id=<id> ---` delimited blocks; batches of at most `BatchSize` (default 15) rules per invocation.

2b. **Claude Invocation** section: replace the old flag list with the verified anti-injection flag set: `claude --print --output-format stream-json --verbose --strict-mcp-config --system-prompt <file> --setting-sources "" --tools "" --disable-slash-commands --no-session-persistence [--model m]`, prompt on stdin, working directory = a neutral temp dir. State WHY: prevents the child from reading/obeying the operator's ambient `CLAUDE.md`.

2c. **New "Cache" section:** content-hash cache at `<source-dir>/.distill-cache.json`, schema `version` + per-id `{hash, bullet}`, hash folds prompt-version + `system.md` + model + body; hit = skip LLM; miss → compress; success prunes to current ids; validation-failure merges validated bullets without pruning; missing/corrupt/version-mismatch → warn + cold run; `--no-cache` bypasses load/save only.

2d. **New "Validation & Retry" section:** each returned bullet validated per-id (non-empty; `- **…**` bold prefix; exactly one column-0 `- ` line; balanced fences); zero-delimiter response fails the chunk; missing/invalid ids retried exactly once scoped to those ids; still-unresolved → exit non-zero naming ids, output file untouched; run prints `distill: N cached, M compiled (K chunks), R retried`.

2e. **CLI table:** add the `--no-cache` row.

2f. **Error Cases table:** add rows for: rule body contains literal `</rule>` (exit 1, names file, before any LLM call); model returns zero delimiters / hijack (retry once → still failing → exit non-zero naming ids, output untouched); model returns invalid bullet for some ids (scoped retry → still invalid → exit non-zero); model returns bullets for unknown ids (warn, ignore); cache missing/corrupt/version-mismatch (warn, cold run, exit 0).

2g. **Exit codes table:** confirm `2` = usage/argument error is documented (it already is — ensure it stays and note required-flag omission maps to 2).

## 3. Scrub stale marker references

- `main.go`: change the package doc comment line "writing the returned bullets between fenced markers." to describe current behavior, e.g. "assembling the returned per-rule bullets into one regenerated output file." No `marker` reference should remain.
- `docs/dod.md`: fix the two marker references — the "marker convention" bullet under Contract Conformance and the "lands between markers + operator prose outside markers is preserved byte-for-byte" e2e bullet. Rewrite them to the current contract (whole-file regeneration; per-rule validated bullets assembled by Go under `## section` headings; stub `Runner` returns delimited `--- bullet id=… ---` blocks; a hijacked/zero-delimiter response must fail-loud and leave the output untouched).
- `pkg/distill/doc.go`: rewrite the package comment to the current model (compile a flat folder of per-rule markdown → one regenerated AI-targeted file; system instructions out-of-band; per-rule bullets validated and assembled by Go; content-hash cache skips unchanged rules). Remove "fenced markers" / "between the section's markers" / "operator prose outside markers".

After edits, `grep -rn 'marker' main.go docs/dod.md pkg/distill/doc.go` must return NOTHING.

## 4. README update

Update `/workspace/README.md`:
- Document the `.distill-cache.json` file in the source directory (what it is, that it may be committed or gitignored — operator's choice, behavior identical either way).
- Document the new `--no-cache` flag (bypasses cache only; validation and anti-injection always run).
- Add a note: changing `system.md` (the compression instructions) invalidates the whole cache and forces a cold recompile on the next run.
- Keep existing install/format/output/Makefile-integration sections accurate.

## 5. CHANGELOG `## Unreleased`

Add a `## Unreleased` section at the top of `/workspace/CHANGELOG.md` (above `## v0.2.3`) describing the hardening (minor bump toward v0.3.0). Use the required `<prefix>: <what>` format, one bullet per logical change. Include at minimum:
```
## Unreleased

- feat: content-hash cache at `<source-dir>/.distill-cache.json` — unchanged rules skip the `claude` call; warm re-runs spawn zero processes and write byte-identical output.
- feat: batched id-keyed compression — rule bodies fenced as `<rule id="…">` inert data; system instructions passed out-of-band via `--system-prompt`, never in the user prompt.
- feat: anti-injection `claude` invocation — `--setting-sources ""`, `--tools ""`, `--disable-slash-commands`, `--no-session-persistence`, neutral temp cwd; the child can no longer read or obey the operator's ambient `CLAUDE.md`.
- feat: per-id bullet validation with exactly-once scoped retry and fail-loud — a hijacked/zero-delimiter response never reaches the output file; unresolved ids exit non-zero.
- feat: `--no-cache` flag bypasses cache load/save only (validation and anti-injection always run).
- fix: missing required flag exits 2 (usage) distinct from runtime failure exit 1.
- docs: `docs/spec.md` to v3 (cache, batching, validation, invocation contract); scrub stale marker references from `main.go`, `docs/dod.md`, `pkg/distill/doc.go`.
```
Adjust wording to match what actually shipped, but keep every prefix and the load-bearing facts.
</requirements>

<constraints>
- Do NOT change compile-pipeline behavior in this prompt — this is docs + the small exit-code fix only. (The exit-code-2 change IS a behavior change but is scoped to `pkg/cli` flag-error mapping.)
- Do NOT introduce an import cycle: the usage-vs-runtime exit decision lives in `pkg/cli.Execute`, NOT in `distill.ExitCode`. Leave `distill.ExitCode` as nil→0 / else→1.
- Preserve the frozen `docs/spec.md` sections (frontmatter, ordering, atomic write, header/title) in substance; only ADD cache/batching/validation/invocation and the new error rows.
- Both consumer Makefiles stay byte-unchanged.
- `CHANGELOG.md`: required `<prefix>: <what>` format; do NOT copy verification-comment text as entries; describe what was implemented.
- Error wrapping in any touched Go code: `github.com/bborbe/errors` for pkg/ business errors; stdlib `errors.As` is acceptable in `pkg/cli.Execute` for the `UsageError` type check.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run `make precommit` — must exit 0.

Confirm marker scrub:
```
grep -rn 'marker' main.go docs/dod.md pkg/distill/doc.go   # must return NOTHING
```

Confirm docs/spec.md v3 + new sections:
```
grep -n 'Specification (v3)' docs/spec.md
grep -ni 'cache' docs/spec.md
grep -ni 'validation' docs/spec.md
grep -n -- '--setting-sources' docs/spec.md
```
All must return a line.

Confirm CHANGELOG:
```
grep -n '## Unreleased' CHANGELOG.md   # returns a line, section non-empty
```

Confirm exit-code-2 behavior with a test. Add a `pkg/cli` test (external `cli_test` where possible, or in-package for `UsageError`) asserting:
- Running `Run(ctx, []string{})` (no `--source`/`--output`) returns an error that `errors.As`-matches `*UsageError`.
- Running `Run` with valid flags but a driver runtime failure returns an error that does NOT match `*UsageError`.
Run the cli tests:
```
go test -mod=mod ./pkg/cli/...
```
Must pass.
</verification>

<completion_report_template>
Append the standard DARK-FACTORY-REPORT block with `status`, `verification.command`, `verification.exitCode`. Then an `## Improvements` section (PROMPT / GUIDE / GLOBAL, or `- None`).
</completion_report_template>
