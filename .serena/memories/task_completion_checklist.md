# Task Completion Checklist

When completing a task (especially new features or bug fixes), ensure:

## Testing

- [ ] Unit tests pass: `go test ./...`
- [ ] Integration tests pass: `go test ./internal/integration/...`
- [ ] Update snapshots if needed: `UPDATE_SNAPS=true go test ./internal/integration/...`
- [ ] Consider adding new integration test for the feature

## Code Quality

- [ ] Code builds: `go build ./...` or `make build`
- [ ] Linting passes (auto-run on pre-commit via Lefthook)
- [ ] No spelling errors (typos)
- [ ] Follow existing code patterns

## Documentation

- [ ] Update CLAUDE.md if architectural changes
- [ ] Update README.md if user-facing changes
- [ ] Add/update inline comments for complex logic

## Commit

- [ ] Use Conventional Commits format: `feat:`, `fix:`, `chore:`, etc.
- [ ] Commit message describes "why" not just "what"
- [ ] Pre-commit hooks pass automatically

## Before PR/Push

- [ ] Pre-push hooks pass (builds successfully)
- [ ] Consider if config file format needs updates
- [ ] Consider if documentation needs updates

## For New Linting Rules

- [ ] Config struct in `internal/config/config.go`
- [ ] Rule logic in `internal/lint/rules.go`
- [ ] Unit tests in `internal/lint/rules_test.go`
- [ ] CLI flags in `cmd/tally/cmd/lint.go`
- [ ] Config loading wired in `loadConfigForFile()`
- [ ] Integration tests added
- [ ] Documentation updated
