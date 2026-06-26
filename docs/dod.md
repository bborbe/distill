# Definition of Done

After completing your implementation, review your own changes against each criterion below. These are quality checks you perform by inspecting your work — not commands to run (linting and tests already ran via `validationCommand`). Report any unmet criterion as a blocker.

## Code Quality

- Exported types, functions, and interfaces have doc comments
- Error handling uses `github.com/bborbe/errors` with context wrapping
- No debug output (print statements, fmt.Printf) — use structured logging
- No `os.Stdout` or `fmt.Print*` in `pkg/ops/` — operations return structured results, CLI layer formats output
- Factory functions are pure composition — no conditionals, no I/O, no `context.Background()`
- Follow Interface → Constructor → Struct → Method pattern

## Testing

- New code has good test coverage (target >= 80%)
- Changes to existing code have tests covering at least the changed behavior
- Tests use Ginkgo v2 / Gomega with Counterfeiter mocks
- New CLI commands/subcommands have an entry in the integration test command registration table (`integration/cli_test.go`)

## Install

- `go install github.com/bborbe/vault-cli@latest` works
- No `exclude` or `replace` directives in go.mod (break remote install)

## Documentation

- README.md is updated if the change affects usage, configuration, or setup
- CHANGELOG.md has an entry under `## Unreleased`
