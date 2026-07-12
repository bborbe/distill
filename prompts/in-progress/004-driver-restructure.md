---
status: approved
spec: [001-harden-compile-pipeline]
created: "2026-07-12T09:10:00Z"
queued: "2026-07-12T15:24:13Z"
branch: dark-factory/harden-compile-pipeline
---

<summary>
- The compile pipeline switches from "one opaque blob per section" to "one validated bullet per rule, addressed by id".
- Rules that need compressing are batched (at most a fixed batch size per model call) and sent through the hardened runner; each returned bullet is validated for shape.
- Any missing or malformed bullet triggers exactly one retry scoped to only the failing ids; if anything is still unresolved the run fails loudly, names the ids, and the output file is left byte-for-byte untouched.
- The output file is assembled entirely by Go — header, optional title, section headings, and per-rule bullets ordered deterministically — from the validated bullets; the model never contributes structure.
- A one-line stderr summary reports how many rules were served from cache, how many were compiled, in how many chunks, and how many were retried.
- A hijacked/refusal response can never reach the output file — that invariant is asserted by a test.
</summary>

<objective>
Restructure the Driver from per-section blob compression to per-rule id-addressed compilation: delete `runSection`, add a `compileBullets` step (hash-aware lookup via an injected Cache, chunked runner calls, per-id validation, exactly-once scoped retry, fail-loud) and an `assembleOutput` that joins validated per-rule bullets in `(section order, order, id)` order. Emit the run-summary stderr line. Implements Desired Behaviors 7 and 8 and the fail-loud / chunking / scoped-retry / summary ACs.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions.
Read `/workspace/docs/spec.md` — output structure, ordering, error cases (authority).
Read `/workspace/specs/in-progress/001-harden-compile-pipeline.md` — Desired Behaviors 2 (cache lifecycle on failure), 7, 8; the Failure Modes table (hijack, invalid bullets, unknown ids, claude non-zero); and the fail-loud / chunking / retry / summary ACs.

Read these files fully before editing:
- `/workspace/pkg/distill/driver.go` — current `Driver`, `Run`, `runSection`, `filterEnabled`, `groupBySection`, `assembleOutput`, `checkDuplicates`, `ExitCode`. You delete `runSection` and rework `Run` + `assembleOutput`.
- `/workspace/pkg/distill/prompts.go` — `BuildBatchPrompt` + `RuleBody` (added in prompt 1). Also the old `BuildPrompt` shim (remove it in this prompt).
- `/workspace/pkg/distill/parse.go` — `ParseBatchResponse` (prompt 1).
- `/workspace/pkg/distill/validate.go` — `ValidateBullet` (prompt 1).
- `/workspace/pkg/distill/claude.go` — `Runner.Run(ctx, model, systemPrompt, prompt)` (prompt 2) + `SystemPrompt()`.
- `/workspace/pkg/distill/source.go` — `Rule` fields.
- `/workspace/pkg/distill/driver_test.go` — existing e2e tests using `stubRunnerWith` keyed on `id=<id>` in the prompt; you PORT these to the delimited response shape.
- `/workspace/mocks/distill-runner.go` — 4-arg mock (prompt 2).

Read these coding-plugin docs (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-context-cancellation-in-loops.md`

INLINE CONTRACT — the `Cache` interface is introduced fully in prompt 4, but this prompt must compile and inject it. Add the interface HERE (in a new `pkg/distill/cache.go` created in this prompt as interface-only, or in driver.go) so prompt 3 stands alone and green; prompt 4 adds the concrete `NewFileCache` / `NewNoopCache` implementations + hashing + mock. Declare:

```go
//counterfeiter:generate -o ../../mocks/distill-cache.go --fake-name DistillCache . Cache

// Cache stores validated bullets keyed by rule id, guarded by a content hash so
// a changed rule (or changed compression context) is a miss.
type Cache interface {
	// Get returns the cached bullet for id when the stored hash equals hash.
	Get(id, hash string) (bullet string, ok bool)
	// Put records a validated bullet for id under hash (pending write).
	Put(id, hash, bullet string)
	// Save persists the cache, pruning to exactly keepIDs on a successful run.
	Save(ctx context.Context, keepIDs []string) error
	// Load reads the cache from disk; a missing/corrupt file warns and runs cold.
	Load(ctx context.Context) error
	// RuleHash derives the content hash for a rule body under the current
	// compression context (prompt-version constant + system.md + model + body).
	RuleHash(model, body string) string
}
```

For prompt 3, the Driver depends on this `Cache` interface and on a mock in tests. The Driver gets a new field `Cache Cache` and a new field `BatchSize int`. The factory wiring of a real cache is prompt 4; for prompt 3's tests, inject a mock cache configured to always-miss (so all rules compile) unless a test says otherwise.
</context>

<requirements>

## 1. Add Driver fields

In `pkg/distill/driver.go`, add to `Driver`:
```go
// Cache serves previously-validated bullets for unchanged rules.
Cache Cache
// BatchSize is the max number of cache-miss rules per claude invocation.
// Set by the factory (default 15); not a CLI flag.
BatchSize int
// NoCache, when true, bypasses cache load and save (validation/batching/anti-
// injection still run). Set from --no-cache in cli.
NoCache bool
```
(The `NoCache` field lets prompt 4 pass through the `--no-cache` flag; in this prompt the factory sets it false by default. The noop-vs-file cache SELECTION happens in `pkg/cli` in prompt 4; `NoCache` here only gates load/save calls.)

## 2. Rewrite `Driver.Run`

New flow (replace the body of `Run`, keep the signature `Run(ctx, sourceDir, outputPath) error`):

```
rules = Parser.Parse
checkDuplicates(rules)  // unchanged
enabled = filterEnabled(rules), then sort by (section-min-order via groupBySection ordering, then order, then id)  // deterministic global order
if !NoCache { Cache.Load(ctx) }   // Load warns-and-continues; never fatal
bulletByID, err = compileBullets(ctx, enabled)  // hash/lookup/chunk/call/validate/retry/fail-loud
if err != nil {
    if !NoCache { _ = Cache.Save-partial ... }  // see requirement 4 (cache lifecycle on failure)
    return err   // output file NEVER written on failure
}
output = assembleOutput(sourceDir, outputPath, Title, enabled, bulletByID)
Writer.Write(ctx, outputPath, output)   // atomic write is the ONLY output mutation, and it runs ONLY after every enabled id has a validated bullet
if !NoCache { Cache.Save(ctx, allEnabledIDs) }   // prune to current ids on success
print summary line
return nil
```

Hard invariant (assert with a test): `Writer.Write` is called only when `compileBullets` returned no error. A hijacked/zero-delimiter response → `compileBullets` returns an error → `Writer.Write` is NOT called → the pre-existing output file is byte-identical.

## 3. Implement `compileBullets`

```go
// compileBullets returns a validated bullet for every enabled rule id, or an
// error naming the ids that stayed unresolved after one scoped retry. Cache
// hits skip the runner; misses are compressed in chunks of at most BatchSize,
// validated per-id, and any missing/invalid ids are retried exactly once.
func (d *Driver) compileBullets(ctx context.Context, rules []Rule) (map[string]string, error)
```

Steps:
- For each rule compute `hash := d.Cache.RuleHash(d.Model, rule.Body)` (when `NoCache`, skip the cache and treat everything as a miss — you may still call `RuleHash` for the Put on success, but do not Get/Save when NoCache).
- Cache lookup: if `!NoCache` and `bullet, ok := d.Cache.Get(id, hash); ok` → record it, count as cached, skip runner.
- Collect misses in the deterministic global order (already sorted in Run). Chunk into slices of at most `d.BatchSize`.
- For each chunk: build `RuleBody{ID, Body}` list; `prompt, err := BuildBatchPrompt(ctx, bodies)` (propagate the literal-`</rule>` error up — this is exit 1 before/without further calls); `resp, err := d.Runner.Run(ctx, d.Model, SystemPrompt(), prompt)` (wrap runner errors with `errors.Wrapf(ctx, err, "claude run chunk ids=%v", ids)`); `bullets, warnings, _ := ParseBatchResponse(resp, chunkIDs)`; write each warning to `d.Stderr`. For each returned id, `ValidateBullet(ctx, id, bullet)`; on success record + `d.Cache.Put(id, hash, bullet)`; on validation failure, mark id as failing.
- After all chunks: any requested id with no recorded bullet OR a validation failure is a FAILING id.
- Include a non-blocking context-cancellation check between chunks per go-context-cancellation-in-loops.md (`select { case <-ctx.Done(): return nil, errors.Wrapf(ctx, ctx.Err(), "cancelled") default: }`).

Scoped retry (exactly once):
- If there are failing ids after the first pass, re-run ONLY those ids in fresh chunks (same BuildBatchPrompt → Runner → Parse → Validate loop), one retry pass total. Track a `retriedCount` = number of ids retried.
- After the retry pass, if ANY id is still unresolved, return `errors.Errorf(ctx, "unresolved rule ids after retry: %v", stillFailing)`. The output file must never be written in this case.

Return the full `map[string]string` id→bullet only when every enabled id resolved.

## 4. Cache lifecycle on validation failure (Desired Behavior 2, second sentence)

On the failure return path in `Run` (compileBullets errored) and when `!NoCache`: persist the bullets that DID validate WITHOUT pruning, so a rerun re-requests only the still-failing ids. Because `Cache.Put` already recorded every validated bullet during `compileBullets`, call a NON-pruning save. Extend the `Cache` interface with:
```go
// SaveMerged persists all currently-Put bullets without pruning any ids.
SaveMerged(ctx context.Context) error
```
Add `SaveMerged` to the interface declared in requirement Context. On the success path use `Save(ctx, keepIDs)` (prunes); on the failure path use `SaveMerged(ctx)` (no prune). Wrap save errors but do NOT let a save error mask the original compile error — on the failure path, if `SaveMerged` also errors, prefer returning the original compile error (log the save error to `d.Stderr`).

## 5. Rewrite `assembleOutput`

Change signature to build from per-rule bullets:
```go
func assembleOutput(sourceDir, outputPath, title string, rules []Rule, bulletByID map[string]string) string
```
- Keep the exact header comment block (`<!--\n  AUTO-GENERATED by distill …`), the `Source:` and `Regenerate:` lines, and the optional `# <title>` — byte-identical to the current shape (the fixed header/title is frozen behavior).
- Group `rules` by section preserving the section ORDER from `groupBySection` (min order then alphabetical). Within each section, emit rules in `(order, id)` order.
- For each section: `\n## <section>\n\n` then each rule's `bulletByID[rule.ID]` (trimmed right) followed by `\n`.
- Empty sections (all rules disabled — though disabled rules are already filtered before this point) keep their heading with an empty body, per docs/spec.md.
- Delete the old `map[string]string compressed` (section→blob) variant.

## 6. Run-summary stderr line

After a successful write, print EXACTLY:
```
distill: %d cached, %d compiled (%d chunks), %d retried\n
```
to `d.Stderr` where cached = cache hits, compiled = misses that compiled, chunks = number of runner calls in the first pass (not counting retry), retried = ids re-requested. (Match the AC pattern `distill: N cached, M compiled (K chunks), R retried`.)

## 7. Remove dead code

- Delete `runSection`.
- Delete the old `BuildPrompt` shim in `prompts.go` (added transitionally in prompt 1) now that nothing calls it. Confirm `grep -rn BuildPrompt pkg/` returns nothing.

## 8. Port existing driver tests + add new ones

Update `pkg/distill/driver_test.go` (external `distill_test`):

8a. The existing `stubRunnerWith` keys on `id=<id>` and returns a raw body. Rework it to return DELIMITED responses: given a map id→bulletText, the stub inspects the prompt for `<rule id="X">` occurrences and returns concatenated `--- bullet id=X ---\n- **…** …` blocks for the requested ids, so `ParseBatchResponse` can split them. Update the 4-arg `Run` signature (`ctx, model, systemPrompt, prompt`). Ensure returned bullets pass `ValidateBullet` (start with `- **X.**`).

8b. Inject a mock `Cache` (`mocks.DistillCache` — generated in prompt 4; for THIS prompt add a minimal always-miss stub OR set `Driver.NoCache = true` in the ported tests so cache is bypassed). Simplest: set `NoCache: true` and `BatchSize: 100` on the ported existing tests, and provide a mock cache whose `RuleHash` returns a fixed string and `Get` returns miss. Because `mocks.DistillCache` does not exist until prompt 4, either (a) create `mocks/distill-cache.go` via `go generate ./...` in THIS prompt after adding the `//counterfeiter:generate` directive on `Cache` (preferred — the interface lives here), or (b) hand-write a tiny in-test fake. PREFER (a): add the directive, run `go generate ./...`, commit the generated mock.

8c. Add NEW driver tests:
- **Hijack fail-loud + output untouched:** stub Runner returns a zero-delimiter refusal string for a rule; write a pre-existing output file with known bytes; run; assert the error is non-nil, its message names the unresolved id, `Writer` was NOT called (use a mock Writer with `WriteCallCount() == 0`), and the pre-existing output file bytes are unchanged.
- **Chunking:** `BatchSize = 2`, 5 cache-miss rules (in one section, distinct orders/ids); assert `Runner.RunCallCount() == 3` and the id sets per captured call follow global `(order, id)` order. (Use `RunArgsForCall` to inspect each prompt.)
- **Scoped retry:** first Runner response omits one id's bullet; second call is scoped to only that id (assert `RunCallCount() == 2` and the second prompt contains only the missing id's `<rule id=…>`). Return a valid bullet on the retry so the run succeeds.
- **Summary line:** capture `d.Stderr` (a `bytes.Buffer`); assert it contains a line matching `distill: \d+ cached, \d+ compiled \(\d+ chunks\), \d+ retried`.
- **claude non-zero:** stub Runner returns an error; assert `Run` returns an error and `Writer` not called.

Use a mock Writer (`mocks.DistillWriter`, already generated) so you can assert `WriteCallCount()` and capture written content.
</requirements>

<constraints>
- Go owns ALL output structure — sections, ordering, header, title. The model contributes ONLY per-id bullet text (Desired Behavior 8).
- A hijacked response can fail validation but must NEVER reach the output file — hard invariant; the atomic write runs only after every enabled id has a validated bullet.
- The output file is written by exactly ONE mutation (`Writer.Write`) and only on the success path.
- Preserve frozen behavior: header/title shape, duplicate `(section, order, id)` detection, section grouping + ordering, atomic write, silent-skip of non-`distill:` files (all per docs/spec.md).
- Retry is EXACTLY once, scoped to failing ids only.
- Error wrapping: `github.com/bborbe/errors` only. Never `fmt.Errorf`, never bare `return err`, never `context.Background()` in `pkg/`.
- Do NOT add the concrete FileCache/NoopCache/RuleHash implementation here — interface + Driver consumption + generated mock only. Prompt 4 implements the concrete cache and CLI flag.
- Do NOT add `BatchSize` or anti-injection as CLI flags (spec Non-goals). `BatchSize` is a Driver field.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run `make precommit` — must exit 0.

Confirm dead code is gone and structure exists:
```
grep -rn 'runSection\|BuildPrompt' pkg/distill/    # must return nothing
grep -n 'compileBullets' pkg/distill/driver.go     # must return a line
grep -n 'N cached\|%d cached' pkg/distill/driver.go # summary line present
```

Coverage on the new orchestration:
```
go test -coverprofile=/tmp/cover.out -mod=mod ./pkg/distill/... && go tool cover -func=/tmp/cover.out | grep -E 'compileBullets|assembleOutput|Run'
```
Each ≥80%.
</verification>

<completion_report_template>
Append the standard DARK-FACTORY-REPORT block with `status`, `verification.command`, `verification.exitCode`. Then an `## Improvements` section (PROMPT / GUIDE / GLOBAL, or `- None`).
</completion_report_template>
