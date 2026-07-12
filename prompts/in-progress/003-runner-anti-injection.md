---
status: approved
spec: [001-harden-compile-pipeline]
created: "2026-07-12T09:10:00Z"
queued: "2026-07-12T15:24:13Z"
branch: dark-factory/harden-compile-pipeline
---

<summary>
- The `claude` subprocess is now launched so it cannot discover or obey the operator's ambient `CLAUDE.md` — the exact feedback loop that corrupted both generated files.
- Compression instructions are handed to the child process out-of-band as a system prompt, never mixed into the data the model reads.
- Ambient settings/rule discovery is disabled, tools and slash commands are turned off, session persistence is off, and the child runs in a neutral temp directory so it can't re-read the file being regenerated.
- The subprocess wrapper's signature gains the system-prompt argument; its generated test double is regenerated to match.
- Verified via unit assertions on the exact command arguments and working directory — the real-binary check is an operator post-release step.
- No orchestration/driver logic changes here; only the subprocess boundary and its mock.
</summary>

<objective>
Harden the `claude` subprocess invocation against ambient-`CLAUDE.md` injection: extend the `Runner` interface to accept a system prompt, add the verified anti-injection flag set + neutral working directory, regenerate the Counterfeiter mock, and keep all call sites compiling. Implements Desired Behavior 4 and the Runner-arg ACs.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions.
Read `/workspace/specs/in-progress/001-harden-compile-pipeline.md` — Desired Behavior 4, the Assumptions section (verified flag set), and the Runner-arg Acceptance Criteria.
Read these files fully before editing:
- `/workspace/pkg/distill/claude.go` — current `Runner` interface + `runner.Run` + `scanResult` + `tailLine`.
- `/workspace/mocks/distill-runner.go` — current generated mock (you regenerate this).
- `/workspace/pkg/distill/driver.go` — `runSection` calls `d.Runner.Run(ctx, d.Model, prompt)`; this call site must keep compiling.
- `/workspace/pkg/distill/distill_suite_test.go` — has the `//go:generate` counterfeiter directive.

Read these coding-plugin docs (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`

Verified Runner flag set (from the spec Assumptions — empirically confirmed on the target machine 2026-07-12, do NOT deviate):
`claude --print --output-format stream-json --verbose --strict-mcp-config --system-prompt <system.md file> --setting-sources "" --tools "" --disable-slash-commands --no-session-persistence [--model m]`
User prompt on stdin. Working directory = `os.TempDir()`.

Load-bearing facts from Assumptions (do NOT re-litigate):
- `--system-prompt` ALONE does not stop ambient `CLAUDE.md` injection; `--setting-sources ""` is what suppresses discovery and is REQUIRED in the set.
- `--bare` was rejected (breaks OAuth). Do NOT add `--bare`.
- `--tools ""` is accepted in `--print` mode.
</context>

<requirements>

## 1. Extend the `Runner` interface in `pkg/distill/claude.go`

Change the interface method to pass the system prompt out-of-band:

```go
//counterfeiter:generate -o ../../mocks/distill-runner.go --fake-name DistillRunner . Runner

// Runner invokes `claude --print` with the compression instructions passed
// out-of-band via --system-prompt and the batch prompt on stdin. It returns the
// final `result` event's text. The child process is invoked so it cannot read
// or obey the operator's ambient CLAUDE.md.
type Runner interface {
	Run(ctx context.Context, model string, systemPrompt string, prompt string) (string, error)
}
```

(Argument order: `ctx, model, systemPrompt, prompt`.)

## 2. Implement the anti-injection invocation in `runner.Run`

Rewrite `runner.Run` so that:

2a. `claude --system-prompt <prompt>` takes the system-prompt STRING directly as the argv value (verified against `claude --help`: `--system-prompt <prompt>  System prompt to use for the session`). Pass `systemPrompt` inline — do NOT write it to a temp file and do NOT pass a file path (that would feed claude the path string as its system prompt and silently break anti-injection; the file-based variant is a different flag, `--system-prompt-file`, which we do NOT use). No temp file, no extra IO, no gosec surface. `system.md` is a few KB — well within argv limits.

2b. Build args EXACTLY as:
```go
args := []string{
	"--print",
	"--output-format", "stream-json",
	"--verbose",
	"--strict-mcp-config",
	"--system-prompt", systemPrompt,
	"--setting-sources", "",
	"--tools", "",
	"--disable-slash-commands",
	"--no-session-persistence",
}
if model != "" {
	args = append(args, "--model", model)
}
```
Note the empty-string values for `--setting-sources` and `--tools` are separate argv elements (`"--setting-sources", ""`), NOT `--setting-sources=""`. The `--system-prompt` value is the full `systemPrompt` string, passed inline (no file).

2c. `cmd := exec.CommandContext(ctx, "claude", args...)`; set `cmd.Dir = os.TempDir()` (neutral cwd — belt-and-braces against the child re-reading the regenerated file, since `--setting-sources ""` suppresses discovery but a neutral cwd removes any project-root ambiguity); `cmd.Stdin = bytes.NewBufferString(prompt)`.

2d. Keep the existing stdout-pipe + `scanResult` + `cmd.Wait()` + stderr-tail error handling and the `strings.TrimRight` on the result. Keep `scanResult` and `tailLine` unchanged. Keep the "produced no result event" and "start claude CLI (is `claude` on $PATH?)" error messages.

## 3. Keep the driver call site compiling

In `pkg/distill/driver.go`, `runSection` calls `d.Runner.Run(ctx, d.Model, prompt)`. Update this ONE call to `d.Runner.Run(ctx, d.Model, SystemPrompt(), prompt)` so the package compiles. (Prompt 3 replaces `runSection` entirely with the batched compile path; this is a minimal keep-it-green change, not the final wiring.)

## 4. Regenerate the Counterfeiter mock

Regenerate `mocks/distill-runner.go` so `RunStub` / `RunArgsForCall` / etc. carry the new 4-arg signature (`context.Context, string, string, string`). Run:
```
cd /workspace && go generate ./...
```
Do NOT hand-edit the generated file beyond what `go generate` produces. Verify the file header still says `// Code generated by counterfeiter. DO NOT EDIT.` and `RunArgsForCall(i int)` now returns `(context.Context, string, string, string)`.

## 5. Runner-argument unit test

Add a test in `pkg/distill/` (external `distill_test` package, Ginkgo v2 + Gomega) that verifies the constructed command WITHOUT spawning `claude`. Since `runner` builds an `exec.Cmd` internally, extract the arg/dir construction into a small unexported pure helper so it is testable, e.g.:

```go
// buildClaudeArgs returns the argv (after "claude") for the given model and
// system-prompt string.
func buildClaudeArgs(model, systemPrompt string) []string
```

Have `runner.Run` call `buildClaudeArgs`. Then unit-test `buildClaudeArgs` (via an exported test seam — either export it as `BuildClaudeArgs` if the package convention allows, OR add an in-package `claude_internal_test.go` with `package distill`). Assert the returned argv:
- Contains `--setting-sources` immediately followed by `""` (empty string element).
- Contains `--tools` immediately followed by `""` (empty string element).
- Contains `--disable-slash-commands`, `--no-session-persistence`, `--strict-mcp-config`.
- Contains `--system-prompt` immediately followed by the given systemPrompt string (the instructions inline, NOT a file path).
- Includes `--model m` when model is `m`; omits `--model` when model is `""`.

Also assert (in-package test) that `runner` sets `cmd.Dir = os.TempDir()` — extract a `neutralDir() string { return os.TempDir() }` helper if that makes the assertion clean, or assert on a constructed `*exec.Cmd` from a small `buildCmd` helper. Choose ONE seam and use it consistently; do not spawn the real binary.

(Note: the "system prompt not in user prompt" AC is satisfied by prompt 1's `BuildBatchPrompt` test — do not duplicate it here. The neutral-cwd + flag-presence assertions are this prompt's responsibility.)
</requirements>

<constraints>
- Use EXACTLY the verified flag set from the spec Assumptions. Do NOT add `--bare`. Do NOT drop `--setting-sources ""` — it is the load-bearing anti-injection flag.
- The compression instructions travel via `--system-prompt` file, NEVER inside the stdin user prompt.
- `--setting-sources` and `--tools` values are empty-string argv elements, not `=""` suffixes.
- Do NOT add a flag to disable the anti-injection behavior — the whole point is that it always runs (spec Non-goal).
- Error wrapping: `github.com/bborbe/errors` only. Never `fmt.Errorf`, never bare `return err`, never `context.Background()` in `pkg/`.
- gosec: temp file perms stay at `CreateTemp` default (0600); if the linter flags anything, fix with a documented `#nosec` reason per go-security-linting.md, do not widen perms.
- Do NOT touch cache/driver-restructure/CLI concerns — those are prompts 3 and 4.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run `make precommit` — must exit 0.

Confirm the mock regenerated to the 4-arg shape:
```
grep -n 'RunArgsForCall(i int) (context.Context, string, string, string)' mocks/distill-runner.go
```
Must return a line.

Confirm the flag set is present in the runner:
```
grep -n -- '--setting-sources' pkg/distill/claude.go
grep -n -- '--tools' pkg/distill/claude.go
grep -n 'os.TempDir' pkg/distill/claude.go
```
All three must return a line.
</verification>

<completion_report_template>
Append the standard DARK-FACTORY-REPORT block with `status`, `verification.command`, `verification.exitCode`. Then an `## Improvements` section (PROMPT / GUIDE / GLOBAL, or `- None`).
</completion_report_template>
