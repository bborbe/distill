# distill — Specification

`distill` reads a folder of detailed per-rule markdown files and writes short imperative one-liners between fenced markers in one or more target files. Source files are the authority. Targets are derived, never hand-edited.

## Goal

Replace the manual "long-form Rules section → derive a one-liner → paste into CLAUDE.md → bump date" workflow with an idempotent compiler.

## CLI

```
distill --source <dir>
```

| Flag | Required | Meaning |
|---|---|---|
| `--source <dir>` | yes | Folder containing source rule files (one rule per file) |
| `--check` | no | Exit non-zero if any target file would change. Used in CI / pre-commit. |
| `--verbose` | no | Print per-rule decisions to stderr. |

Exit codes:

| Code | Meaning |
|---|---|
| `0` | All targets up to date (no change), or all changes applied successfully |
| `1` | Generic failure (parse error, IO error, unresolved target) |
| `2` | Usage / argument error |
| `3` | `--check` mode: targets out of date (no files modified) |

## Source File Format

Each source file is a markdown file with YAML frontmatter and a `## TL;DR` section. The TL;DR is what `distill` emits into the target.

```markdown
---
distill:
  target: global              # required: alias or absolute path
  section: Git                # required: heading text in target
  order: 10                   # optional: sort key within section (lower first; default 100)
  id: no-git-c-flag           # optional: stable id (defaults to filename without .md)
---

# Rule: No `git -C` Flag

## TL;DR
Never `git -C /path` or `cd /path && git ...`; first `cd /path`, then run git commands in subsequent Bash calls.

## Why
…long-form rationale…

## Examples
…wrong / right…

## Anti-patterns
…seductive almost-right shapes…

## When to remove
Permanent; no removal trigger.
```

### Frontmatter contract

The `distill:` block is the only frontmatter `distill` reads. Anything else (Obsidian metadata, page_type, etc.) is ignored.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `target` | string | yes | Where the derived bullet lands. Either a registered alias (see "Target Resolution") or a path. |
| `section` | string | yes | Heading text in the target file. Must match exactly (modulo leading `##`). |
| `order` | int | no | Sort key within section. Default `100`. Stable sort on ties (then by `id`). |
| `id` | string | no | Stable identifier. Used in tie-breaking and for the optional `<!-- rule:<id> -->` anchor. Defaults to filename stem. |
| `disabled` | bool | no | If `true`, source is parsed but not emitted. For staging removals before deletion. |

A source file with no `distill:` block is silently skipped (treated as documentation in the source folder, not a rule).

### TL;DR extraction

`distill` reads the content under the `## TL;DR` heading until the next `##` heading or EOF. Leading/trailing whitespace and trailing newlines are stripped. The block is emitted **verbatim** into the target (no rewrap, no rewrite).

A source file missing `## TL;DR` is an error (exit 1).

### One rule per file

Each source file emits exactly one bullet into exactly one target/section. Multi-target broadcasting is non-goal.

## Target Resolution

`target:` may be:

| Form | Resolves to |
|---|---|
| `global` | `~/.claude/CLAUDE.md` |
| `vault` | `$DISTILL_VAULT_CLAUDE_MD` if set, else error |
| Absolute path (starts with `/` or `~`) | That path, with `~` expanded |
| Relative path | Resolved against the CWD where `distill` was invoked |

Aliases are not configurable in v1. `global` and `vault` are the only built-ins; everything else is an explicit path.

A target file that does not exist is an error (exit 1) — `distill` does not create targets.

## Marker Convention

Each target section that `distill` owns is delimited by a matching pair of HTML-comment markers:

```markdown
## Git

Some free-form prose. Stays as-is.

<!-- begin:distill section="Git" -->
- Rule one one-liner.
- Rule two one-liner.
<!-- end:distill section="Git" -->

More free-form prose. Stays as-is.
```

Rules:

1. The `section="…"` attribute on `begin:distill` and `end:distill` must match exactly.
2. The section name in the marker must match `section:` from at least one source file with `target:` matching this file.
3. Content outside marker pairs is **preserved verbatim** — `distill` never edits it.
4. Content between markers is **replaced wholesale** on every run; never edit it by hand.
5. A target file may contain multiple `begin:distill` / `end:distill` pairs (one per section).
6. The order of pairs in the target file is the operator's choice and is preserved across runs.

## Section Addressing

`section:` from source must equal the `section="…"` attribute on a matching marker pair in the resolved target file. `distill` does NOT scan for or create `##` headings — the marker is the addressing primitive, not the heading. (The operator places markers under whichever heading they like; `distill` only writes between markers.)

## Output Format

Inside each marker pair, `distill` emits, in order:

1. Source rules with matching `target:` + `section:`, sorted by `order` ascending then `id` ascending.
2. Each rule is rendered as one bullet: the TL;DR block prefixed with `- ` on the first line. Subsequent lines of the TL;DR are indented two spaces.
3. A trailing newline terminates the marker block.

No rule = empty marker block (single newline between begin and end). No header. No surrounding blank lines.

## Idempotency Contract

For any source folder S and target T:

```
distill --source S                   # first run, may write
distill --source S                   # second run, MUST write zero bytes
diff before-run-1 after-run-1        # may be non-empty
diff after-run-1 after-run-2         # MUST be empty
```

Achieved by:

- Stable sort (`order` then `id`)
- Verbatim TL;DR (no reformatting)
- LF line endings; no trailing whitespace on emitted lines
- One trailing `\n` at end of marker block; no extras
- `--check` mode compares would-write bytes to current file bytes; exit 3 on mismatch.

## Error Cases

| Case | Behaviour |
|---|---|
| Source file missing `## TL;DR` | Error, exit 1, name file in stderr. |
| Source file missing required `distill:` frontmatter | Skipped silently (treated as docs). |
| Source `target:` is alias `vault` but `$DISTILL_VAULT_CLAUDE_MD` unset | Error, exit 1. |
| Target file does not exist | Error, exit 1 (distill never creates targets). |
| Target file contains `begin:distill section="X"` with no matching `end:distill section="X"` | Error, exit 1 ("orphan begin marker"). |
| Target file contains `end:distill section="X"` with no preceding `begin:distill section="X"` | Error, exit 1 ("orphan end marker"). |
| Target file has no marker matching a source's `section:` | Error, exit 1, name source file + target + section in stderr. |
| Two source files resolve to same `(target, section, order, id)` | Error, exit 1, name both source files. |
| Marker exists in target but no source file targets it | Warning to stderr; marker block emptied. Not an error. |
| Source file has `disabled: true` | Parsed, not emitted. No error. |

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

## TL;DR
Never `git -C /path` or `cd /path && git ...` — both break auto-approval. First `cd /path`, then separate Bash calls for `git status`, `git diff`, etc.

## Why
The Bash auto-approval rule keys on the literal command shape; `git -C` and `cd && git` both fall through to the prompt. Splitting the cd into its own call keeps subsequent git commands auto-approved.

## When to remove
Remove once Bash auto-approval matches on resolved commands, not literal strings.
```

### `git-no-claude-attribution.md`

```markdown
---
distill:
  target: global
  section: Git
  order: 20
---

# Rule: No Claude Attribution in Commits

## TL;DR
No Claude attribution in commits — no "Generated with Claude Code", no "Co-Authored-By".

## When to remove
Permanent; no removal trigger.
```

### `obsidian-no-h1.md`

```markdown
---
distill:
  target: vault
  section: Critical Rules
  order: 30
---

# Rule: No H1 Headers in Vault Pages

## TL;DR
No H1 headers — filename = title.

## When to remove
Permanent; no removal trigger.
```

### Target before run — `~/.claude/CLAUDE.md`

```markdown
# Global Preferences

…free-form prose…

## Git

Some operator prose about git.

<!-- begin:distill section="Git" -->
<!-- end:distill section="Git" -->

…more prose…
```

### Target before run — vault `CLAUDE.md` (resolved via `$DISTILL_VAULT_CLAUDE_MD`)

```markdown
# CLAUDE.md

Obsidian vault assistant.

## Critical Rules

<!-- begin:distill section="Critical Rules" -->
<!-- end:distill section="Critical Rules" -->
```

### Run

```bash
export DISTILL_VAULT_CLAUDE_MD=~/Documents/Obsidian/Personal/CLAUDE.md
distill --source "~/Documents/Obsidian/Personal/50 Knowledge Base/CLAUDE Rules"
```

### Target after run — `~/.claude/CLAUDE.md`

```markdown
# Global Preferences

…free-form prose…

## Git

Some operator prose about git.

<!-- begin:distill section="Git" -->
- Never `git -C /path` or `cd /path && git ...` — both break auto-approval. First `cd /path`, then separate Bash calls for `git status`, `git diff`, etc.
- No Claude attribution in commits — no "Generated with Claude Code", no "Co-Authored-By".
<!-- end:distill section="Git" -->

…more prose…
```

### Target after run — vault `CLAUDE.md`

```markdown
# CLAUDE.md

Obsidian vault assistant.

## Critical Rules

<!-- begin:distill section="Critical Rules" -->
- No H1 headers — filename = title.
<!-- end:distill section="Critical Rules" -->
```

### Second run

```bash
distill --source "~/Documents/Obsidian/Personal/50 Knowledge Base/CLAUDE Rules" --check
echo $?  # 0
```

Zero diff. `--check` exits 0.

## Non-goals

- **Watch / daemon mode.** One-shot CLI; re-run as needed.
- **Multi-target broadcasting from one source.** One rule = one target/section.
- **Configurable aliases beyond `global` + `vault`.** Use absolute paths for anything else.
- **Creating target files.** `distill` only writes between existing markers in existing files.
- **Markdown linting / content validation.** Source authoring quality is not `distill`'s concern.
- **Header / heading management.** `distill` does not insert `##` headings or rewrite operator prose outside markers.
- **Rule discovery beyond the declared source folder.** No recursion into linked repos, no Obsidian dataview emulation.
- **Atomic multi-target writes.** Each target is rewritten in-place; on partial failure mid-run, earlier targets remain mutated.

## Relationship to existing manual workflow

Replaces these manual steps from [[Update CLAUDE.md]] runbook:

- Step 4 (derive TL;DR by hand) — `distill` reads `## TL;DR` directly.
- Step 5 (paste into matching section) — `distill` writes between markers.
- Step 7 (bump date) — N/A; markers replace the dating signal.
- Drift check — every line between markers maps to a source file; orphan rules (marker but no source) emit a warning.

The long-form authoring rules (Why / Examples / Anti-patterns / When-to-remove) stay in source files exactly as they live in [[CLAUDE.md Rules]] today, just split one per file.
