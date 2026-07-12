---
status: completed
spec: [001-harden-compile-pipeline]
summary: Added NewFileCache/NewNoopCache/RuleHash with atomic JSON persistence, --no-cache CLI flag, and cache+BatchSize wired through factory; all ACs pass with 82.1% pkg/distill coverage
execution_id: distill-harden-exec-005-cache-and-cli
dark-factory-version: v0.191.4
created: "2026-07-12T09:10:00Z"
queued: "2026-07-12T15:24:13Z"
started: "2026-07-12T15:52:45Z"
completed: "2026-07-12T16:02:23Z"
branch: dark-factory/harden-compile-pipeline
---

<summary>
- Unchanged rules are now served from a per-source-directory content-hash cache, so re-running on unchanged sources spawns zero `claude` processes and produces byte-identical output.
- Editing a single rule causes only that rule's id to be re-requested from the model; every other rule is served from cache.
- On a successful run the cache is pruned to exactly the current enabled rule ids — deleted, renamed, or disabled rules drop out of the cache.
- A missing or corrupt cache file produces a stderr warning and a cold run; it never fails the run.
- The cache hash folds in a prompt-version constant, the compression instructions, and the model name — so changing any of those invalidates every entry and forces a cold recompile.
- A new opt-out flag bypasses the cache only; validation, batching, and the anti-injection flags always run.
</summary>

<objective>
Add the concrete content-hash cache (`NewFileCache`, `NewNoopCache`, `RuleHash`, JSON persistence with prune/merge semantics) behind the `Cache` interface introduced in prompt 3, add the additive `--no-cache` CLI flag, and wire cache + `BatchSize` selection through `pkg/cli` into the factory (factory stays pure composition). Implements Desired Behaviors 1 and 2 and the warm-noop / single-edit / prune / corrupt-cache ACs.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions.
Read `/workspace/docs/spec.md` — output contract, exit codes.
Read `/workspace/specs/in-progress/001-harden-compile-pipeline.md` — Desired Behaviors 1 & 2; Failure Modes rows for cache-missing / cache-corrupt / schema-version-mismatch; the warm-noop, single-edit, prune, corrupt-cache ACs; and Constraint: "factory is pure composition … the noop-vs-file cache selection happens in `pkg/cli`, and a `Cache` is passed into the factory."

Read these files fully before editing:
- `/workspace/pkg/distill/cache.go` — the `Cache` interface (declared in prompt 3, including `Get`, `Put`, `Save`, `SaveMerged`, `Load`, `RuleHash`, and the `//counterfeiter:generate` directive). You ADD the concrete implementations here.
- `/workspace/pkg/distill/driver.go` — `Driver` fields `Cache`, `BatchSize`, `NoCache`; how `Run` calls `Load`/`Save`/`SaveMerged`/`Get`/`Put`/`RuleHash`.
- `/workspace/pkg/distill/prompts.go` — `SystemPrompt()` (the hash must fold in `SystemPrompt()`).
- `/workspace/pkg/factory/factory.go` — `CreateDriver(stderr, model, title, verbose)`; you extend its signature to accept a `Cache` and `BatchSize`.
- `/workspace/pkg/factory/factory_test.go` — update for the new signature.
- `/workspace/pkg/cli/cli.go` — cobra flags + `RunE` calling `factory.CreateDriver`; you add `--no-cache` and the cache selection.
- `/workspace/mocks/distill-cache.go` — generated in prompt 3 from the `Cache` interface.

Read these coding-plugin docs (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-factory-pattern.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-cli-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-security-linting.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
</context>

<requirements>

## 1. Prompt-version constant + `RuleHash`

In `pkg/distill/cache.go` add:
```go
// cachePromptVersion bumps whenever the compression contract changes in a way
// that must invalidate every cache entry. Increment on any system.md or output-
// contract change.
const cachePromptVersion = "v3"
```
Implement `RuleHash` (satisfies the interface method on the concrete file cache AND is usable standalone). The hash MUST fold in: `cachePromptVersion`, `SystemPrompt()`, the `model` string, and the raw rule `body`. Use `crypto/sha256` over the concatenation with unambiguous separators, return the hex digest:
```go
func RuleHash(model, body string) string {
	h := sha256.New()
	// write each component with a length-prefixed or NUL-separated framing so
	// distinct inputs cannot collide via concatenation
	...
	return hex.EncodeToString(h.Sum(nil))
}
```
The file-cache's `RuleHash` method delegates to this package function. (Folding in `SystemPrompt()` means editing `system.md` invalidates every entry → cold recompile, which is required.)

## 2. `NewFileCache(path string, warn io.Writer) Cache`

Concrete file-backed cache. JSON schema on disk at `path` (default `<source-dir>/.distill-cache.json`):
```go
type cacheFile struct {
	Version string            `json:"version"`
	Entries map[string]cacheEntry `json:"entries"`
}
type cacheEntry struct {
	Hash   string `json:"hash"`
	Bullet string `json:"bullet"`
}
```
where the top-level `version` equals `cachePromptVersion`.

Behavior:
- `Load(ctx)`: read `path`. If the file is MISSING → write a warning line to `warn` (`distill: cache file %q missing — running cold`) and start empty; return nil (never fatal). If UNPARSEABLE JSON → warn (`distill: cache file %q corrupt — ignoring, running cold`) and start empty; return nil. If `Version != cachePromptVersion` → warn (`distill: cache schema version mismatch — running cold`) and start empty; return nil. On success populate an in-memory `map[string]cacheEntry`.
- `Get(id, hash)`: return `(entry.Bullet, true)` only when an entry exists AND `entry.Hash == hash`; else `("", false)`.
- `Put(id, hash, bullet)`: record into an in-memory pending map (guard with a mutex if you like, but the Driver is single-goroutine — a plain map is acceptable; document it).
- `Save(ctx, keepIDs)`: write a `cacheFile{Version: cachePromptVersion, Entries: …}` containing ONLY entries whose id ∈ `keepIDs` (prune removed/renamed/disabled ids), then atomically persist (temp-file+rename, mirror `writer.go`). Entries are the union of loaded + Put, filtered to `keepIDs`. gosec: temp file perms 0600, final file 0644 (documented `#nosec` only if the linter requires it — prefer explicit perms).
- `SaveMerged(ctx)`: write ALL entries (loaded + Put) with NO pruning, atomically. Same version stamp.

## 3. `NewNoopCache() Cache`

A no-op cache: `Get` always returns `("", false)`; `Put` is a no-op; `Load`/`Save`/`SaveMerged` return nil; `RuleHash` delegates to the package `RuleHash` function (so the Driver can still compute hashes even under `--no-cache`, though it won't store them).

## 4. `--no-cache` CLI flag + cache selection in `pkg/cli`

In `pkg/cli/cli.go`:
- Add a `noCache bool` var and register `rootCmd.Flags().BoolVar(&noCache, "no-cache", false, "bypass the content-hash cache (validation and anti-injection still run)")`. Additive, default false.
- In `RunE`, SELECT the cache (this is the "selection happens in pkg/cli" constraint — NOT in the factory):
  ```go
  var cache distill.Cache
  if noCache {
      cache = distill.NewNoopCache()
  } else {
      cache = distill.NewFileCache(filepath.Join(sourceDir, ".distill-cache.json"), cmd.ErrOrStderr())
  }
  driver := factory.CreateDriver(cmd.ErrOrStderr(), cache, model, title, verbose)
  driver.BatchSize = ...  // see requirement 5 — set via factory, not here
  driver.NoCache = noCache
  return driver.Run(cmd.Context(), sourceDir, outputPath)
  ```
  (`--no-cache` gates cache load/save only; it never disables validation, batching, or anti-injection — spec Constraint.)

## 5. Factory signature — stay pure composition

Extend `CreateDriver` to accept the `Cache` and set `BatchSize` to the default 15 (a factory-set field, NOT a flag):
```go
func CreateDriver(stderr io.Writer, cache distill.Cache, model, title string, verbose bool) *distill.Driver {
	return &distill.Driver{
		Parser:    distill.NewParser(),
		Runner:    distill.NewRunner(),
		Writer:    distill.NewWriter(),
		Cache:     cache,
		BatchSize: 15,
		Stderr:    stderr,
		Verbose:   verbose,
		Model:     model,
		Title:     title,
	}
}
```
Factory has ZERO conditionals/IO — the noop-vs-file decision was already made in `pkg/cli` and passed in. Set `BatchSize: 15` as a literal here (this IS pure composition — a constant default, no logic). Do NOT set `NoCache` in the factory; `pkg/cli` sets it on the returned driver (as shown in requirement 4). Update `factory_test.go` for the new signature (pass a `distill.NewNoopCache()` and assert `d.Cache != nil`, `d.BatchSize == 15`).

## 6. Tests

Add `pkg/distill/cache_test.go` (external `distill_test`, Ginkgo v2 + Gomega):
- `RuleHash` is stable for identical inputs and differs when model, body, or (simulate by) system prompt differs. Since `SystemPrompt()` is fixed at build time, assert model-varies and body-varies produce different hashes, and identical inputs produce identical hashes.
- FileCache `Load` on a missing file → warning written to the warn writer + no error + cold (all `Get` return miss).
- FileCache `Load` on corrupt JSON (write `"{not json"`) → warning + no error + cold.
- FileCache `Load` on a version-mismatch file (`{"version":"v0","entries":{...}}`) → warning + cold (entries ignored).
- Round-trip: `Put` two ids → `Save(ctx, [both ids])` → new FileCache `Load` → both `Get(id, hash)` hit; a `Get` with the wrong hash misses.
- Prune: `Load` a file with ids `a,b` → `Put` nothing → `Save(ctx, ["a"])` → reload → `a` present, `b` absent.
- `SaveMerged`: `Load` ids `a,b` → `Put` `c` → `SaveMerged` → reload → `a,b,c` all present (no prune).
- NoopCache: `Get` always miss; `Save`/`SaveMerged`/`Load` return nil.

Add driver-level integration tests (extend `driver_test.go`) using the REAL `NewFileCache` (temp path) + mock Runner:
- **Warm no-op:** run once (cache miss → Runner called, output written), then run AGAIN with the same sources and a FRESH mock Runner; assert second run's `Runner.RunCallCount() == 0` and the output file is byte-identical to the first run's output.
- **Single-edited-rule scoped request:** two rules; run once; edit exactly one rule's body; run again; assert the second run's Runner was called and its captured prompt contains the edited rule's `<rule id=…>` and EXCLUDES the unchanged rule's id.
- **Prune on delete/disable:** run with rules `a,b`; delete (or set `disabled: true` on) `b`'s source file; run again; parse the on-disk `.distill-cache.json` and assert it no longer contains `b`'s id.
- **Corrupt cache → cold run + exit 0:** pre-write a corrupt `.distill-cache.json`; run; assert stderr contains the corrupt-cache warning, `Runner.RunCallCount()` equals the number of chunks (all rules re-requested), and `Run` returns nil (no error for that reason alone).

Use the delimited `stubRunnerWith` from prompt 3 so validation passes.
</requirements>

<constraints>
- `--no-cache` bypasses cache load AND save ONLY; it NEVER disables validation, batching, or the anti-injection flags (spec Constraint + Non-goal).
- Do NOT add a `--cache <path>` flag; the default `<source-dir>/.distill-cache.json` is fixed (spec Non-goal).
- Do NOT add a `--batch-size` flag; `BatchSize` is a factory-set Driver field default 15 (spec Non-goal).
- Factory stays pure composition: no conditionals, no IO, no `context.Background()`. The noop-vs-file selection lives in `pkg/cli`; a constant `BatchSize: 15` literal is allowed.
- A missing/corrupt/version-mismatched cache file → warn + cold run, NEVER a non-zero exit for that reason.
- Cache hash MUST fold in prompt-version constant + `SystemPrompt()` + model + body, so any compression-context change invalidates every entry.
- Both consumer Makefiles stay byte-unchanged — `--no-cache` is additive with a safe default (do NOT touch any Makefile).
- Atomic writes for the cache file (temp-file+rename), mirroring `writer.go`.
- Error wrapping: `github.com/bborbe/errors` only. Never `fmt.Errorf`, never bare `return err`, never `context.Background()` in `pkg/`.
- gosec: cache file perms explicit (0600 temp, 0644 final); document any `#nosec`.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run `make precommit` — must exit 0.

Confirm the wiring:
```
grep -n 'no-cache' pkg/cli/cli.go            # flag registered
grep -n 'NewFileCache\|NewNoopCache' pkg/cli/cli.go   # selection in cli
grep -n 'cache distill.Cache' pkg/factory/factory.go  # cache passed into factory
grep -n 'BatchSize: 15' pkg/factory/factory.go        # default set in factory
grep -n 'cachePromptVersion' pkg/distill/cache.go     # version constant
```
All must return a line.

Coverage:
```
go test -coverprofile=/tmp/cover.out -mod=mod ./pkg/... && go tool cover -func=/tmp/cover.out | grep -E 'RuleHash|NewFileCache|Load|Save'
```
New cache code ≥80%.
</verification>

<completion_report_template>
Append the standard DARK-FACTORY-REPORT block with `status`, `verification.command`, `verification.exitCode`. Then an `## Improvements` section (PROMPT / GUIDE / GLOBAL, or `- None`).
</completion_report_template>
