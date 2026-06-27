# CLAUDE.md

`distill` — compile a folder of detailed per-rule markdown files into one short, AI-targeted markdown file (e.g. `CLAUDE.md`). Source files own content; the derived target is regenerated, never hand-edited.

## Dark Factory Workflow

**Never code directly.** All code changes go through the dark-factory pipeline.

### Complete Flow

**Spec-based (multi-prompt features):**

1. Create spec → `/dark-factory:create-spec`
2. Audit spec → `/dark-factory:audit-spec`
3. User confirms → `dark-factory spec approve <name>`
4. dark-factory auto-generates prompts from spec (`autoGeneratePrompts: true` in `.dark-factory.yaml`)
5. Audit prompts → `/dark-factory:audit-prompt`
6. User confirms → `dark-factory prompt approve <name>`
7. Start daemon → `dark-factory daemon` (use Bash `run_in_background: true`)
8. dark-factory executes prompts automatically

**Standalone prompts (simple changes):**

1. Create prompt → `/dark-factory:create-prompt`
2. Audit prompt → `/dark-factory:audit-prompt`
3. User confirms → `dark-factory prompt approve <name>`
4. Start daemon → `dark-factory daemon` (use Bash `run_in_background: true`)

### Choosing a Flow

**Canonical guide: `~/Documents/workspaces/dark-factory/docs/choosing-a-flow.md`** — read it, don't second-guess from memory.

30-second decision:

1. Code that runs in build / production / CI? No → **Direct** edit (markdown, config, yaml).
2. Yes — carries a business-level "why" worth a permanent in-repo document? No → **Prompt**. Yes → **Spec → prompts**.

### Key Rules

- Prompts go to **`prompts/`** (inbox) — never `prompts/in-progress/` or `prompts/completed/`
- Specs go to **`specs/`** (inbox)
- Never number filenames — dark-factory assigns numbers on approve
- Never manually edit frontmatter status — use CLI commands
- Always audit before approving
- **BLOCKING: Never run `dark-factory spec approve`, `dark-factory prompt approve`, or `dark-factory daemon` without explicit user confirmation.**
- **Before starting daemon** — run `dark-factory status` first to check if one is already running.
- **Start daemon in background** — Bash `run_in_background: true` (not detached with `&`)

### Contract Source

`docs/spec.md` is the user-facing contract for distill's behavior — frontmatter, marker syntax, target resolution, idempotency, error cases. Implementation prompts and dark-factory specs refer back to `docs/spec.md` as the authority; they don't redefine it.

## Development Standards

This project follows the [coding-guidelines](https://github.com/bborbe/coding-guidelines).

### Build and test

- `make precommit` — lint + format + generate + test + checks
- `make test` — tests only

### Test conventions

- Ginkgo v2 / Gomega test framework
- External test packages (`*_test`)

## Architecture

- `main.go` — CLI entry point (flag parsing, exit codes)
- `docs/spec.md` — contract: source frontmatter, marker syntax, target resolution, idempotency, error cases

(Source parser, target writer, and end-to-end runner packages will be added in `pkg/` as implementation progresses.)

## Key Design Decisions

- **Source is the authority** — per-rule `.md` files declare `target:` + `section:` in frontmatter; full body is the rule
- **LLM compresses at compile time** — `distill` bundles sources per group, calls `claude --print`, writes the returned bullets between markers. No cache, no idempotency in v1; every run hits Claude.
- **Markers delimit derived content** — `<!-- begin:distill section="X" -->...<!-- end:distill section="X" -->`; operator prose outside markers is preserved byte-for-byte
- **`ClaudeRunner` is an interface** — mockable for unit / E2E tests so the test suite never spawns the real `claude` CLI
- **No watch / daemon mode** — one-shot CLI; re-run as needed
