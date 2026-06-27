# distill — Specification

`distill` reads a folder of detailed per-rule markdown files, sends them to Claude Code (`claude --print`) with a compression prompt, and writes the returned compact output between fenced markers in one or more target files.

## Goal

Replace the manual "long-form rule → derive a one-liner → paste into CLAUDE.md" workflow with a single CLI call. The author writes long-form rules; `distill` calls Claude to compress; the target file is regenerated.

## Non-goals (v1)

- No caching. Every run calls Claude.
- No idempotency contract. Re-running on unchanged sources may produce different output (LLM is non-deterministic). Accepted.
- No `--check` mode. (Cannot exist without cache.)
- No watch / daemon mode. One-shot CLI.
- No creating target files; they must exist with markers in place.
- No content validation of source files beyond frontmatter parsing.
- No multi-target broadcasting from a single source file.

## CLI

```
distill --source <dir>
```

| Flag | Required | Meaning |
|---|---|---|
| `--source <dir>` | yes | Folder containing source rule files |
| `--model <name>` | no | Claude model name (default: `sonnet`) |
| `--verbose` | no | Print per-group prompts + Claude responses to stderr |

Exit codes:

| Code | Meaning |
|---|---|
| `0` | All targets written successfully |
| `1` | Generic failure (parse error, IO error, Claude error, unresolved target, missing marker) |
| `2` | Usage / argument error |

## Source File Format

Each source file is a markdown file with YAML frontmatter declaring `distill:` metadata and a markdown body containing the rule (long-form: rationale, examples, anti-patterns — whatever the author wants).

```markdown
---
distill:
  target: global              # required: alias or path
  section: Git                # required: marker section name in target
  order: 10                   # optional: sort key within section (lower first; default 100)
  id: no-git-c-flag           # optional: stable id (defaults to filename stem)
  disabled: false             # optional: skip emission
---

# Rule: No `git -C` Flag

Never use `git -C /path ...` or `cd /path && git ...` — both break the Bash auto-approval matcher.
The fix is to issue the `cd` as its own Bash call, then run plain `git status` / `git diff` etc.

Examples
- Wrong: `git -C ~/repo status`
- Right: first call `cd ~/repo`, then second call `git status`
```

The full source body — not just a TL;DR section — is sent to Claude as the input to compress.

### Frontmatter contract

The `distill:` block is the only frontmatter `distill` reads. Anything else is ignored.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `target` | string | yes | Where the compressed line lands. Either alias (`global`, `vault`) or a path. |
| `section` | string | yes | Marker section name in the target file. Must match exactly (modulo quoting). |
| `order` | int | no | Sort key within section. Default `100`. Stable sort on ties (then by `id`). |
| `id` | string | no | Stable identifier. Defaults to filename stem. |
| `disabled` | bool | no | If `true`, source is loaded but not sent to Claude / not emitted. |

A source file with no `distill:` block is silently skipped (treated as docs in the source folder).

## Target Resolution

`target:` may be:

| Form | Resolves to |
|---|---|
| `global` | `~/.claude/CLAUDE.md` |
| `vault` | `$DISTILL_VAULT_CLAUDE_MD` if set, else error (exit 1) |
| Absolute / `~`-prefixed path | That path, with `~` expanded |
| Relative path | Resolved against the CWD where `distill` was invoked |

`distill` does NOT create target files. Missing target → exit 1.

## Marker Convention

Each target section that `distill` owns is delimited by a pair of HTML-comment markers:

```markdown
## Git

Some operator prose. Preserved verbatim.

<!-- begin:distill section="Git" -->
- (compressed lines land here)
<!-- end:distill section="Git" -->

More operator prose. Preserved verbatim.
```

Rules:

1. `section="…"` on `begin:distill` and `end:distill` must match exactly.
2. Content outside marker pairs is preserved verbatim.
3. Content between markers is replaced wholesale on every run.
4. A target file may contain multiple `begin:distill` / `end:distill` pairs (one per section).
5. The order of pairs in the target file is the operator's choice and is preserved.

## Compression Prompt

Per (target, section) group, `distill` builds a prompt by concatenating:

1. An embedded system instruction (from `pkg/prompts/system.md` — owned by the binary): "Compress each input rule into one short imperative sentence. Preserve technical literals (commands, env vars, paths) verbatim. No rationale. Output as a markdown bullet list, one bullet per rule, in the order given. No preamble or postamble — bullets only."
2. The source bodies (in order: `order` ascending, then `id` ascending), each prefixed with a delimiter like `--- rule <id> ---`.

The prompt is sent to Claude via `claude --print --output-format stream-json --verbose`, prompt on stdin. The `result` event's `result` field is the compressed block.

The compressed block is written verbatim between the section's markers, preceded and followed by exactly one newline.

## Claude Invocation

`distill` uses the same subprocess pattern as `bborbe/agent/claude.ClaudeRunner`:

- `exec.CommandContext(ctx, "claude", "--print", "--output-format", "stream-json", "--verbose", "--strict-mcp-config", "--model", <model>)`
- Prompt on stdin
- Parse stream-JSON output for the final `result` event
- Trim trailing whitespace; otherwise verbatim

If the `claude` binary is missing on `$PATH`, exit 1 with a clear message.

## Error Cases

| Case | Behaviour |
|---|---|
| Source file missing required `distill:` frontmatter | Skipped silently. |
| Source `target: vault` but `$DISTILL_VAULT_CLAUDE_MD` unset | Exit 1; stderr names the env var. |
| Target file does not exist | Exit 1; stderr names the resolved path. |
| Target file has orphan `begin:distill` / `end:distill` (unmatched pair) | Exit 1; stderr names target + section + orphan kind. |
| Target file has no marker matching a source's `section:` | Exit 1; stderr names source + target + section. |
| Two source files share identical `(target, section, order, id)` | Exit 1; stderr names both source files. |
| Marker pair in target with no source claiming that section | Warning to stderr; marker block emitted empty. Not an error. |
| `claude` CLI exits non-zero | Exit 1; stderr names the group + tail of Claude's stderr. |
| `claude` CLI not on `$PATH` | Exit 1; stderr names the binary. |
| Source folder missing / unreadable | Exit 1. |
| `--source` flag missing | Exit 2; stderr prints usage. |

## Architecture

- `main.go` — flag parsing, exit codes
- `pkg/cli/` — driver: orchestrate parser → resolver → grouper → claude runner → writer
- `pkg/source/` — parse source folder; extract frontmatter + body
- `pkg/target/` — alias / path resolution
- `pkg/marker/` — scan target into (prose, marker-pair) regions
- `pkg/prompts/` — embedded system prompt (`//go:embed system.md`)
- `pkg/claude/` — thin wrapper around `exec.CommandContext("claude", ...)`, stream-JSON parsing; mockable for tests
- `pkg/writer/` — replace marker contents in target file

Tests mock `pkg/claude` so unit / E2E tests are deterministic and offline. The real Claude call only happens when the operator runs the binary.

## Worked Example

### Source folder

```
~/Documents/Obsidian/Personal/50 Knowledge Base/CLAUDE Rules/
├── git-no-c-flag.md
├── git-no-claude-attribution.md
└── obsidian-no-h1.md
```

### `git-no-c-flag.md`

```markdown
---
distill:
  target: global
  section: Git
  order: 10
---

# Rule: No `git -C` Flag

`git -C /path …` and `cd /path && git …` both break the Bash auto-approval matcher
because the matcher keys on the literal command shape. Splitting `cd` into its own
Bash call keeps subsequent `git status` / `git diff` calls auto-approved.

Examples:
- Wrong: `git -C ~/repo status`
- Right: call `cd ~/repo` first, then `git status` in a separate Bash call.
```

### Target before run — `~/.claude/CLAUDE.md`

```markdown
# Global Preferences

## Git

Some operator prose.

<!-- begin:distill section="Git" -->
<!-- end:distill section="Git" -->

More prose.
```

### Run

```bash
distill --source "~/Documents/Obsidian/Personal/50 Knowledge Base/CLAUDE Rules"
```

### Target after run — `~/.claude/CLAUDE.md` (illustrative; exact wording is Claude's call)

```markdown
# Global Preferences

## Git

Some operator prose.

<!-- begin:distill section="Git" -->
- Never `git -C /path` or `cd /path && git ...`; first `cd /path`, then run git in a separate Bash call.
- No Claude attribution in commits — no "Generated with Claude Code", no "Co-Authored-By".
<!-- end:distill section="Git" -->

More prose.
```

The exact bullet wording is the LLM's choice within the system-prompt constraints. Re-running may produce slightly different phrasing; that is accepted in v1.

## Relationship to existing manual workflow

Replaces the manual "derive a TL;DR by hand → paste into CLAUDE.md → bump date" loop from [[Update CLAUDE.md]]. The author writes the long-form rule once; `distill` and Claude do the compression and the paste.

## Future (out of scope for v1)

- Content-hash cache so unchanged sources skip the Claude call (idempotency on demand).
- `--check` mode for CI gating.
- Author-assist subcommand (draft a TL;DR via Claude, write it back to source).
- Pluggable LLM provider (currently `claude` only).
