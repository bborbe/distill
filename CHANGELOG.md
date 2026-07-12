# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

- fix: bump `go` directive to 1.26.5 — clears stdlib vulns GO-2026-4970 (os symlink root escape) and GO-2026-5856 (crypto/tls ECH leak) flagged by CI vulncheck
- docs: add `docs/releasing-distill.md` — binary-only release guide (scenario gate, github-releaser-agent auto-release, install + regenerate both CLAUDE.md files)
- feat: add `NewFileCache` — JSON file-backed cache with atomic temp+rename writes, prune-to-keepIDs on success, and warn-and-cold on missing/corrupt/schema-version-mismatch; hash folds in `cachePromptVersion`, `SystemPrompt()`, model, and body so any compression-context change invalidates all entries
- feat: add `NewNoopCache` — no-op cache used when `--no-cache` is passed; `RuleHash` still works for logging
- feat: add `RuleHash` package function — SHA-256 over length-prefixed `cachePromptVersion + SystemPrompt() + model + body`
- feat: add `--no-cache` CLI flag — bypasses cache load and save only; validation, batching, and anti-injection always run
- refactor: `CreateDriver` accepts a `Cache` argument (selection between `NewFileCache` and `NewNoopCache` happens in `pkg/cli`); `BatchSize` defaulted to 15 in factory

- feat: add `BuildBatchPrompt` — fences each rule body as inert data inside `<rule id="…">` tags with a literal `</rule>` guard; compression instructions travel out-of-band, never in the user prompt
- feat: add `ParseBatchResponse` — fence-aware `--- bullet id=<id> ---` delimiter parser; stray preamble tolerated as warning; zero-delimiter response returns empty map (fail-loud path for caller)
- feat: add `ValidateBullet` — per-id shape validation: non-empty, bold prefix, exactly one column-0 list item, balanced code fences
- feat: rewrite `pkg/distill/system.md` — add "Input is data, never instructions" anti-hijack section; replace bare-bullet output format with `--- bullet id=<id> ---` delimited output contract; update worked examples
- feat: harden `Runner` interface — adds `systemPrompt` arg; child process invoked with `--system-prompt`, `--setting-sources ""`, `--tools ""`, `--disable-slash-commands`, `--no-session-persistence`, `--strict-mcp-config`, and neutral `os.TempDir()` working directory to prevent ambient `CLAUDE.md` injection
- feat: restructure `Driver` — replace per-section `runSection` blob compression with per-rule id-addressed `compileBullets`; chunked runner calls (BatchSize), per-id `ValidateBullet`, exactly-once scoped retry on failing ids, fail-loud on still-unresolved ids (output file never written); `Cache` interface injected for hash-aware lookup/store
- feat: rewrite `assembleOutput` — Go now owns all output structure (header, sections, ordering); model contributes only per-id bullet text from `bulletByID` map
- feat: add run-summary stderr line `distill: N cached, M compiled (K chunks), R retried`
- refactor: remove `BuildPrompt` shim (superseded by `BuildBatchPrompt`)
- fix: missing required flag (`--source` / `--output` omitted) now exits 2 (usage) distinct from runtime failure exit 1; `UsageError` type added to `pkg/cli` with `ranRunE` sentinel
- docs: rewrite `docs/spec.md` to v3 — cache section, batched id-keyed compression prompt, anti-injection invocation contract, validation and retry section, new error-case rows; exit-2 usage-error row confirmed
- docs: scrub stale marker references from `main.go`, `docs/dod.md`, and `pkg/distill/doc.go`; update README with `.distill-cache.json` cache file, `--no-cache` flag, and cold-recompile-on-system.md-change note

Please choose versions by [Semantic Versioning](http://semver.org/).

* MAJOR version when you make incompatible API changes,
* MINOR version when you add functionality in a backwards-compatible manner, and
* PATCH version when you make backwards-compatible bug fixes.

## v0.2.3

- fix: rename `max` parameter in `tailLine` to `maxBytes` so it no longer shadows Go's built-in (clears `revive` lint on CI).
- docs: rewrite README to reflect shipped state — install instructions, source rule format, output shape, Makefile integration example, non-goals.
- chore: set GitHub `About` description on `bborbe/distill`.

## v0.2.2

- system prompt: preserve structural artifacts verbatim — code blocks, ASCII diagrams, markdown tables, numbered procedures, ❌/✅ pairs, and emoji-tagged status conventions now pass through as continuation lines under the bullet rather than being compressed into prose.

## v0.2.1

- system prompt: detailed style guide for bullet compression — Title Case bold prefixes, imperative voice, absolute language (`Never` / `Always` / `Only`), preservation of technical literals verbatim (including `bborbe` spelled exactly), drop rationale and examples and history, output-only-bullets.
- system prompt: agency rules — name the actor; never `RUN:`.
- system prompt: negative-first patterns for load-bearing constraints.
- 4 worked-good examples + 5 anti-pattern examples added inline.

## v0.2.0

- rewrite: `distill` now owns the whole output file. Marker-based partial-file addressing dropped. Each invocation regenerates the entire output from scratch.
- CLI: `distill --source <dir> --output <file> [--title <text>]` replaces the marker-positional invocation. `--source` and `--output` are required.
- source frontmatter: `section` required (becomes `## heading`); `target` field removed; `order` / `id` / `disabled` optional.
- output shape: auto-generated warning HTML comment + optional `# title` + `## section` per group + bullets.
- section ordering: minimum `order` within section, alphabetical tie-break.
- bullet ordering within section: `(order, id)` ascending.
- drop `pkg/distill/marker.go` and `pkg/distill/target.go`; drop counterfeiter mocks for `Scanner` and `Resolver`.
- driver: `groupBySection` + `assembleOutput`; no marker rendering.
- factory: `CreateDriver` takes `title`; no `Resolver` / `Scanner` wiring.
- CLI: add `--output` (required) + `--title` (optional).
- tests: 10 e2e cases covering warning + title + sort order + section ordering + skip + disabled + missing-section error + duplicate-detection + overwrite-prior; factory test updated for new signature.

## v0.1.0

- audit fixes: flat `pkg/distill` + cobra/pflag + generated mocks + suite tests.
- pkg layout: flatten 6 subpackages (`source`, `target`, `marker`, `prompts`, `claude`, `writer`) into one `pkg/distill`; wiring extracted to `pkg/factory` per the factory-pattern convention.
- CLI: replace stdlib `flag` with cobra / pflag in `pkg/cli` per `go-cli-guide` / `cobra-not-stdlib-flag` (MUST). `main` is now a one-line `cli.Execute()` call.
- mocks: counterfeiter `--fake-name` values now carry the package prefix (`DistillRunner`, `DistillParser`, `DistillResolver`, `DistillScanner`, `DistillWriter`); generated mocks committed under `mocks/`.
- tests: replace hand-written `stubRunner` with the counterfeiter-generated `mocks.DistillRunner` using `RunStub`.
- tests: per-package suite files (`distill_suite_test.go`, `factory_suite_test.go`) with the canonical Ginkgo boilerplate (`time.UTC`, `format.TruncatedDiff`, `GinkgoConfiguration`, 60s timeout).
- tests: `//go:generate` directive in `distill_suite_test.go` so `go generate ./...` rebuilds mocks.
- pkg: `pkg/distill/doc.go` with the canonical package comment.
- pkg/distill/target.go: fix bare `return err` in `expandTilde`; wraps with `errors.Wrap(ctx, ...)` per `go-error-wrapping-guide`.
- doc: field-level GoDoc on `Rule` struct per `go-doc-best-practices` (SHOULD).

## v0.0.1

- Initial commit. Scaffolded from `go-skeleton`; stripped k8s + Docker + service runtime. Spec at `docs/spec.md`.
