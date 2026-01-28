# Code Style and Conventions

## Commit Messages

- **REQUIRED**: Use Conventional Commits format (enforced by commitlint)
- Format: `<type>: <description>`
- Types: `feat`, `fix`, `chore`, `docs`, `test`, `refactor`, `perf`, `ci`
- Example: `feat: add max-lines rule`, `fix: handle empty dockerfiles`

## Go Code Style

- Enforced by golangci-lint (`.golangci.yaml`)
- Named returns disabled (nonamedreturns linter)
- Follow standard Go conventions
- Use existing patterns from the codebase

## Testing Strategy

**Integration tests are preferred** for new features. They provide true end-to-end coverage.

### Integration Tests Pattern

1. Create directory in `internal/integration/testdata/` with `Dockerfile`
2. Add test case to `TestCheck`
3. Run `UPDATE_SNAPS=true go test ./internal/integration/...` to generate snapshots
4. Snapshots verify JSON output

### Unit Tests

- Use for isolated parsing and linting logic
- Test pure functions that don't require CLI interaction

## Configuration Pattern

Cascading priority (highest first):

1. CLI flags
2. Environment variables (`TALLY_*` prefix)
3. Config file (`.tally.toml` or `tally.toml`)
4. Built-in defaults

## Adding New Linting Rules (Exemplary Pattern)

See `max-lines` rule as reference:

1. Add rule config struct in `internal/config/config.go`
2. Add rule logic in `internal/lint/rules.go`
3. Add unit tests in `internal/lint/rules_test.go`
4. Add CLI flags in `cmd/tally/cmd/check.go`
5. Wire config loading in `loadConfigForFile()`
6. Add integration test cases
7. Update documentation
