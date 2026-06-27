# Definition of Done

After completing your implementation, review your own changes against each criterion below. These are quality checks you perform by inspecting your work — not commands to run (linting and tests already ran via `validationCommand`). Report any unmet criterion as a blocker.

## Code Quality

- Exported types, functions, and interfaces have doc comments
- Error handling uses `github.com/bborbe/errors` with context wrapping
- No debug output (print statements, fmt.Printf in non-CLI code) — use structured logging
- Factory functions are pure composition — no conditionals, no I/O, no `context.Background()`
- Follow Interface → Constructor → Struct → Method pattern

## Contract Conformance

- Changes match `docs/spec.md` (frontmatter contract, marker convention, target resolution, error cases, Claude invocation). If `docs/spec.md` needs to change, change it first and call it out.
- All exit codes from `docs/spec.md` (`0` / `1` / `2`) are reachable from the implementation.

## Testing

- New code has good test coverage (target >= 80%)
- Changes to existing code have tests covering at least the changed behavior
- Tests use Ginkgo v2 / Gomega with Counterfeiter mocks
- An end-to-end test exercises the worked example from `docs/spec.md` against a stub `ClaudeRunner` and asserts that the stubbed output lands between markers + operator prose outside markers is preserved byte-for-byte

## Install

- `go install github.com/bborbe/distill@latest` works
- No `exclude` or `replace` directives in go.mod (break remote install)

## Documentation

- README.md is updated if the change affects usage, configuration, or setup
- CHANGELOG.md has an entry under `## Unreleased`
- `docs/spec.md` and CLAUDE.md stay in sync with shipped behavior
