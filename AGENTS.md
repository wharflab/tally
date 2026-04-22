# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go CLI for linting Dockerfiles and Containerfiles. It checks container build files for best practices, security issues, and
common mistakes.

- `main.go`: application entrypoint
- `cmd/tally/cmd/`: CLI commands (`root.go`, `lint.go`, `version.go`)
- `internal/`: implementation packages
  - `internal/config/`: configuration loading with cascading discovery (koanf)
  - `internal/dockerfile/`: Dockerfile parsing (buildkit)
  - `internal/lint/`: linting rules
  - `internal/version/`: version info
- `internal/integration/`: end-to-end tests with snapshots and fixtures
  - `internal/integration/testdata/<case>/Dockerfile`: test Dockerfiles
  - `internal/integration/testdata/<case>/.tally.toml`: test config files
  - `internal/integration/__snapshots__/`: `go-snaps` snapshot outputs
- `_docs/`: Mintlify documentation source (published via `wharflab/docs` repo)
  - `_docs/rules/<namespace>/<rule>.mdx`: one page per rule
  - `_docs/guides/`: user guides
  - `_docs/docs.json`: Mintlify navigation config
- `bin/` and `dist/`: local tools / release artifacts (ignored by Git)

## Build, Test, and Development Commands

- `make build`: builds the `tally` binary into the repo root
- `make test`: runs `go test -race -count=1 -timeout=30s ./...`
- `make lint`: runs `golangci-lint` for CI (no auto-fix)
- `make lint-fix`: runs `golangci-lint` with `--fix` for local development
- `make cpd`: runs PMD Copy/Paste Detector to find duplicate code (100 token threshold, excludes tests)
- `make clean`: removes the built binary and deletes `bin/` + `dist/`

## Release Notes

- Releases are orchestrated by [`.github/workflows/release.yml`](.github/workflows/release.yml).
- The release pipeline builds binaries on a native GitHub Actions OS matrix, packages artifacts in `dist/`, then publishes GitHub release assets and
  ecosystem packages from workflow jobs.
- If release is broken, check in this order:
  - [`.github/workflows/release.yml`](.github/workflows/release.yml)
  - [`scripts/release/package_release_artifact.py`](scripts/release/package_release_artifact.py)
  - [`packaging/npm/`](packaging/npm)
  - [`packaging/pypi/`](packaging/pypi)
  - [`packaging/rubygems/`](packaging/rubygems)

## JSON v2 Notice

- This repo uses Go JSON v2 experiment: `GOEXPERIMENT=jsonv2` must be set for Go commands.
- Prefer `encoding/json/v2` (and `encoding/json/jsontext`) for all JSON code.
- Avoid `encoding/json` except explicit compatibility boundaries (for example APIs that require v1 types).

Local usage examples:

- `go run . lint --help`
- `go run . lint Dockerfile`
- `go run . lint --max-lines 100 Dockerfile`
- `go run . lint --config .tally.toml Dockerfile`

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

- Format: `gofmt` + `goimports` (configured via `.golangci.yaml`, with `github.com/wharflab/tally` as the local import prefix).
- Prefer small, focused packages under `internal/`; keep CLI wiring in `cmd/`.
- For rule authoring, prefer the shared facts layer in `internal/facts/` when logic depends on derived stage/run state (effective `ENV`, active
  `SHELL`, parsed `RUN` commands, package/install heuristics, cache/registry signals).
  - Facts are built once per file by the linter and exposed to rules through `input.Facts` (`*facts.FileFacts` via type assertion).
  - Rules should consume facts read-only; if a heuristic will be reused, add it to `internal/facts/` instead of recomputing it inside each rule.
- Tests use standard Go conventions: filenames end in `*_test.go`.

## Testing Guidelines

- Unit tests live alongside packages in `internal/**`.
- Integration tests (`internal/integration`) build the binary once and run it against test fixtures.
- Integration test placement decision tree:
  [`design-docs/16-integration-tests-refactor-and-placement.md` §8](design-docs/16-integration-tests-refactor-and-placement.md#8-decision-tree-where-should-this-test-go)
- Update snapshots when intentional output changes:
  - `UPDATE_SNAPS=true go test ./internal/integration/...`
- In rule/resolver tests, round-trip a fix back to source with `fix.ApplyFix(src, v.SuggestedFix)` or `fix.ApplyEdits(src, edits)` — handles
  edit ordering so you don't need a manual reverse-order `ApplyEdit` loop.

## Commit & Pull Request Guidelines

- Follow semantic commit rules (Conventional Commits), e.g. `feat: ...`, `fix: ...`, `chore: ...` (enforced via `commitlint` in `.lefthook.yml`).
- Run `make lint`, `make cpd`, and `make test` before opening a PR (Lefthook runs these on `pre-commit` and `make build` on `pre-push`).
- PRs should explain *what* changed and *why*, note any snapshot updates, and avoid committing build outputs (the `tally` binary is Git-ignored).
- CI runs: tests, golangci-lint, and CPD (copy/paste detection) automatically on all PRs.
