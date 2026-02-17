# tally Development Guide for GitHub Copilot

**tally** is a production-grade Dockerfile/Containerfile linter + formatter built in Go, using BuildKit's official parser.

## Quick Start

```bash
make build          # Build tally binary
make test           # Run all tests
make lint           # Run golangci-lint (CI mode, no auto-fix)
make lint-fix       # Run golangci-lint with auto-fix
make cpd            # Copy/paste detection
```

Run the tool locally:

```bash
go run . lint Dockerfile
go run . lint --fix --config .tally.toml Dockerfile
```

## Critical: JSON v2 Requirement

**This project uses Go's JSON v2 experiment.** `GOEXPERIMENT=jsonv2` is already set in the Makefile.

- **Always use** `encoding/json/v2` and `encoding/json/jsontext`
- **Never use** `encoding/json` (v1) except at external API boundaries
- The `depguard` linter enforces this rule

When running `go` commands directly (outside `make`), ensure `GOEXPERIMENT=jsonv2` is set.

## Architecture

```text
main.go                       # Entrypoint
cmd/tally/cmd/               # CLI commands (root, lint, version, lsp)
internal/
  ├── config/                # Config loading (koanf) with cascading discovery
  ├── dockerfile/            # Dockerfile parsing (BuildKit wrapper)
  ├── lint/                  # Core linting engine
  ├── rules/                 # Rule implementations (buildkit/, tally/, hadolint/)
  ├── fix/                   # Auto-fix engine and resolvers
  ├── reporter/              # Output formatters (text, json, sarif, markdown, github-actions)
  ├── lsp/ & lspserver/      # Language Server Protocol implementation
  ├── registry/              # Container registry client (no Docker daemon)
  ├── integration/           # End-to-end tests with snapshots
  │   ├── testdata/<case>/   # Test Dockerfiles and configs
  │   └── __snapshots__/     # go-snaps snapshot outputs
  └── [other packages]       # Utilities and helpers
```

### Key Principles

- **Use BuildKit as source of truth**: Always use `github.com/moby/buildkit/frontend/dockerfile/parser` and `.../instructions`. Never write custom
  Dockerfile parsers.
- **CLI wiring in `cmd/`, logic in `internal/`**: Keep command definitions thin; implement behavior in internal packages.
- **Prefer container ecosystem primitives**: Wrap/extend existing tools rather than reimplementing (BuildKit, go-containerregistry, patternmatcher).

## Configuration Discovery

tally uses **cascading config discovery** like Ruff:

- Config files: `.tally.toml` or `tally.toml`
- Discovery: walks up from target file, uses closest config
- Priority: CLI flags > env vars (`TALLY_*`) > config file > defaults

Example `.tally.toml`:

```toml
[output]
format = "json"
fail-level = "warning"

[rules]
include = ["buildkit/*", "tally/*"]
exclude = ["buildkit/MaintainerDeprecated"]

[rules.tally.max-lines]
max = 100
skip-blank-lines = true
```

## Testing Strategy

### Unit Tests

- Live alongside packages: `internal/*/‌*_test.go`
- Use standard Go test conventions

### Integration Tests

- Location: `internal/integration/integration_test.go`
- Use snapshot testing with `go-snaps`
- Fixtures: `internal/integration/testdata/<case>/Dockerfile`
- Test configs: `internal/integration/testdata/<case>/.tally.toml`

**Update snapshots after intentional output changes:**

```bash
UPDATE_SNAPS=true go test ./internal/integration/...
```

### Running Tests

```bash
# All tests (with race detector)
make test

# Verbose output
make test-verbose

# Single package
go test ./internal/config

# Single test
go test -run TestLintCommand ./internal/integration
```

## Rules and Fixes

### Adding Rules

1. **Rule config**: Add schema to `internal/config/`
2. **Rule logic**: Implement in `internal/rules/<source>/`
3. **CLI wiring**: Update `cmd/tally/cmd/lint.go` if new flags needed
4. **Integration test**: Add fixture in `internal/integration/testdata/<case>/`
5. **Documentation**: Update `RULES.md`

### Fix Safety Levels

Use `Violation.WithSuggestedFix()` and pick the narrowest safety level:

- `FixSafe`: Eligible for `--fix` (safe, mechanical rewrites)
- `FixSuggestion`: Requires `--fix-unsafe` (heuristic fixes)
- `FixUnsafe`: Requires `--fix-unsafe` (risky transformations)

If a fix needs external data (registry, AI), implement a `fix.FixResolver` instead of doing IO/network in the rule.

## Code Quality

### Linting

The project uses **golangci-lint v2** with a custom wrapper and custom rules:

```bash
make lint           # CI mode (no auto-fix)
make lint-fix       # Local mode (with --fix)
```

Custom linter (`customlint`) enforces tally-specific rules beyond standard Go linters.

### Copy/Paste Detection

PMD CPD runs automatically in CI (minimum 100 tokens):

```bash
make cpd
```

Excludes tests, generated code, and packaging.

### Prefer Modern Go

- Use `slices`, `maps`, `cmp`, `strings.Cut*`, `errors.Join`
- Avoid deprecated APIs: `io/ioutil`, `sort.Slice` (use `slices.Sort`)

## Build Tags

The project uses **pure-Go** build tags to avoid CGO dependencies:

```text
BUILDTAGS := containers_image_openpgp,containers_image_storage_stub,containers_image_docker_daemon_stub
```

These are set automatically in the Makefile.

## Commit Conventions

- **Semantic commits** (Conventional Commits): `feat:`, `fix:`, `chore:`, etc.
- Enforced by `commitlint` via Lefthook (`.lefthook.yml`)
- Pre-commit hooks run: `make lint`, `make cpd`, `make test`
- Pre-push hooks run: `make build`

## LSP (Language Server Protocol)

tally includes a built-in LSP server for editor integration:

```bash
tally lsp --stdio
```

The VS Code extension (`wharflab.tally`) uses this for real-time linting.

### LSP Protocol Generation

Protocol definitions are generated from the official LSP spec:

```bash
make lsp-protocol   # Requires bun
```

Generated files: `internal/lsp/protocol/`

## JSON Schema

Configuration schema is auto-generated:

```bash
make jsonschema     # Generates schema.json
```

Update after changing config structures in `internal/config/`.

## Additional Resources

- **AGENTS.md**: Complete repository guidelines (structure, commands, config)
- **CLAUDE.md**: AI contributor notes (defaults, non-negotiables, preferences)
- **RULES.md**: Full rules reference
- **design-docs/**: Architecture decision records and design discussions
