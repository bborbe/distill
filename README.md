# distill

Compile a folder of detailed per-rule markdown files into one short, AI-targeted markdown file.

Source authoring stays human-friendly (one concept per file, full Why / Examples / Anti-patterns / When-to-remove). The output is dense, scannable, token-cheap — loaded into every AI session, derived, never edited by hand.

Primary use case: deriving `~/.claude/CLAUDE.md` and project-vault `CLAUDE.md` files from a folder of rule sources.

## Status

Scaffolded. Contract in [`docs/spec.md`](docs/spec.md); implementation not started.

## Development

```bash
make precommit   # lint + format + generate + test + checks
make test        # tests only
```
