# CLAUDE.md

`distill` — compile a folder of detailed per-rule markdown files into one short, AI-targeted markdown file (e.g. `CLAUDE.md`). Source files own content; the derived target is regenerated, never hand-edited.

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

- **Source is the authority** — per-rule `.md` files declare `target:` + `section:`; the CLI compiles them into derived blocks
- **Markers delimit derived content** — `<!-- begin:rules section=<name> -->...<!-- end:rules section=<name> -->`; free-form prose around markers is preserved
- **Idempotent** — re-running on unchanged sources yields zero diff
- **No watch / daemon mode** — one-shot CLI; re-run as needed
