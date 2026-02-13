# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go CLI for linting Dockerfiles and Containerfiles. It checks container build files for best practices, security issues, and
common mistakes.

- `main.go`: application entrypoint
- `cmd/tally/cmd/`: CLI commands (`root.go`, `check.go`, `version.go`)
- `internal/`: implementation packages
  - `internal/config/`: configuration loading with cascading discovery (koanf)
  - `internal/dockerfile/`: Dockerfile parsing (buildkit)
  - `internal/lint/`: linting rules
  - `internal/version/`: version info
- `internal/integration/`: end-to-end tests with snapshots and fixtures
  - `internal/integration/testdata/<case>/Dockerfile`: test Dockerfiles
  - `internal/integration/testdata/<case>/.tally.toml`: test config files
  - `internal/integration/__snapshots__/`: `go-snaps` snapshot outputs
- `bin/` and `dist/`: local tools / release artifacts (ignored by Git)

## Build, Test, and Development Commands

- `make build`: builds the `tally` binary into the repo root
- `make test`: runs `go test -race -count=1 -timeout=30s ./...`
- `make lint`: runs `golangci-lint` for CI (no auto-fix)
- `make lint-fix`: runs `golangci-lint` with `--fix` for local development
- `make cpd`: runs PMD Copy/Paste Detector to find duplicate code (100 token threshold, excludes tests)
- `make clean`: removes the built binary and deletes `bin/` + `dist/`

## JSON v2 Notice

- This repo uses Go JSON v2 experiment: `GOEXPERIMENT=jsonv2` must be set for Go commands.
- Prefer `encoding/json/v2` (and `encoding/json/jsontext`) for all JSON code.
- Avoid `encoding/json` except explicit compatibility boundaries (for example APIs that require v1 types).

Local usage examples:

- `go run . check --help`
- `go run . check Dockerfile`
- `go run . check --max-lines 100 Dockerfile`
- `go run . check --config .tally.toml Dockerfile`

## Configuration

tally uses cascading config discovery (like Ruff):

- Config files: `.tally.toml` or `tally.toml`
- Discovery: walks up from target file, uses closest config
- Priority: CLI flags > env vars (`TALLY_*`) > config file > defaults

Example config:

```toml
format = "json"
[rules.max-lines]
max = 500
skip-blank-lines = true
```

## Coding Style & Naming Conventions

- Format: `gofmt` + `goimports` (configured via `.golangci.yaml`, with `github.com/tinovyatkin/tally` as the local import prefix).
- Prefer small, focused packages under `internal/`; keep CLI wiring in `cmd/`.
- Tests use standard Go conventions: filenames end in `*_test.go`.

## Testing Guidelines

- Unit tests live alongside packages in `internal/**`.
- Integration tests (`internal/integration`) build the binary once and run it against test fixtures.
- Integration test placement decision tree: [`design-docs/16-integration-tests-refactor-and-placement.md` §8](design-docs/16-integration-tests-refactor-and-placement.md#8-decision-tree-where-should-this-test-go)
- Update snapshots when intentional output changes:
  - `UPDATE_SNAPS=true go test ./internal/integration/...`

## Commit & Pull Request Guidelines

- Follow semantic commit rules (Conventional Commits), e.g. `feat: ...`, `fix: ...`, `chore: ...` (enforced via `commitlint` in `.lefthook.yml`).
- Run `make lint`, `make cpd`, and `make test` before opening a PR (Lefthook runs these on `pre-commit` and `make build` on `pre-push`).
- PRs should explain *what* changed and *why*, note any snapshot updates, and avoid committing build outputs (the `tally` binary is Git-ignored).
- CI runs: tests, golangci-lint, and CPD (copy/paste detection) automatically on all PRs.
