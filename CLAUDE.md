# CLAUDE.md - Project Guidance

## Project Overview

`tally` is a fast, configurable linter for Dockerfiles and Containerfiles. It checks container build files for best practices, security issues, and
common mistakes.

## Design Philosophy

**Minimize code ownership** - This project exists in Go specifically to maximize reuse from the container ecosystem. We heavily reuse existing,
well-maintained libraries:

### Primary Sources (research these FIRST)

Before implementing ANY data structure, type, or functionality, **actively research** whether something suitable exists in:

1. **`github.com/moby/buildkit`** - The authoritative source for Dockerfile semantics
   - `frontend/dockerfile/parser` - AST, Position, Range, Warning types
   - `frontend/dockerfile/instructions` - Typed instruction parsing, Stage, Command types
   - `frontend/dockerfile/linter` - LinterRule, rule definitions, LintWarnFunc
   - `frontend/subrequests/lint` - LintResults, Warning output format
   - `solver/pb` - Location, SourceInfo protobuf types

2. **`github.com/containers/common`** - Podman/Buildah ecosystem utilities
   - Configuration patterns, image reference handling

3. **`github.com/opencontainers/image-spec`** - OCI image specification types

### The 80% Rule

**If an existing type/pattern covers 80% of our needs, prefer it over creating something new.** Wrap and extend if necessary rather than reinventing.
Examples:

- Use `parser.Range` internally, wrap with `rules.Location` only to add file path
- Use BuildKit's `LintResults.Warning` structure as reference for our `Violation` schema
- Consume parser's semantic data (LineStats, Stages) instead of re-parsing

### Other Dependencies

- `github.com/urfave/cli/v3` - CLI framework
- `github.com/knadh/koanf/v2` - Configuration loading
- `golang.org/x/sync` - Concurrency primitives

Do not re-implement functionality that exists in these libraries.

**Adding dependencies** - Before adding a new dependency, run `go list -m -versions <module>` to check available versions and use the latest stable
release.

## Build & Test Commands

```bash
# Build
go build ./...
make build

# Run all tests
go test ./...
make test

# Run tests with verbose output
go test ./... -v

# Update snapshots for integration tests
UPDATE_SNAPS=true go test ./internal/integration/...

# Run linting
make lint

# Run copy/paste detection (CPD)
make cpd

# Run the CLI
go run . check --help
go run . check Dockerfile
go run . check --max-lines 100 Dockerfile
```

## Code Quality Tools

This project uses multiple code quality tools:

1. **golangci-lint** - Go linting and static analysis
   - Run locally: `make lint`
   - Runs in CI on every PR

2. **PMD CPD (Copy/Paste Detector)** - Detects duplicate code patterns in production code
   - Run locally: `make cpd`
   - Runs in CI on every PR
   - PMD 7.14.0, 100 token threshold
   - Excludes: `*_test.go`, `*.pb.go`, `*_generated.go`, `testdata/`, `__snapshots__/`, `packaging/`
   - Uses file-list approach for reliable exclusion (not pattern-based)

## Coverage Collection

Integration tests are built with coverage instrumentation (`-cover` flag). Coverage data is automatically collected to a temporary directory during
test runs.

```bash
# Run integration tests (coverage data is automatically collected)
go test ./internal/integration/...

# To view coverage reports, manually run with a persistent coverage directory:
# 1. Build the binary with coverage
go build -cover -o tally-cover .

# 2. Run tests with GOCOVERDIR set
mkdir coverage
GOCOVERDIR=coverage go test ./internal/integration/...

# 3. Generate coverage reports
go tool covdata percent -i=coverage
go tool covdata textfmt -i=coverage -o=coverage.txt
go tool cover -html=coverage.txt -o=coverage.html
```

## Commit Messages

- Use semantic commit rules (Conventional Commits), e.g. `feat: ...`, `fix: ...`, `chore: ...` (enforced via `commitlint` in `.lefthook.yml`).

## Project Structure

```text
.
├── main.go                           # Entry point
├── cmd/tally/cmd/                    # CLI commands (urfave/cli)
│   ├── root.go                       # Root command setup
│   ├── check.go                      # Check subcommand (linting)
│   └── version.go                    # Version subcommand
├── internal/
│   ├── config/                       # Configuration loading (koanf)
│   │   ├── config.go                 # Config struct, loading, discovery
│   │   └── config_test.go
│   ├── dockerfile/                   # Dockerfile parsing (uses buildkit)
│   │   ├── parser.go
│   │   └── parser_test.go
│   ├── lint/                         # Linting rules
│   │   ├── rules.go
│   │   └── rules_test.go
│   ├── version/
│   │   └── version.go
│   ├── integration/                  # Integration tests (go-snaps)
│   │   ├── integration_test.go
│   │   ├── __snapshots__/
│   │   └── testdata/                 # Test fixtures (each in own directory)
│   └── testutil/                     # Test utilities
├── packaging/
│   ├── pack.rb                       # Packaging orchestration script
│   ├── npm/                          # npm package structure (@contino/tally)
│   ├── pypi/                         # Python package (tally-cli)
│   └── rubygems/                     # Ruby gem (tally-cli)
└── README.md
```

## Testing Strategy

**Integration tests are the preferred way to test and develop new features.** They provide true end-to-end coverage, ensuring the entire pipeline
works correctly.

### Integration Tests (`internal/integration/`)

**How it works:**

1. `TestMain` builds the CLI binary with `-cover` flag for coverage instrumentation
2. Tests run the CLI binary against test Dockerfiles
3. Snapshots (`go-snaps`) verify the JSON output

**Adding a new test case:**

1. Create a new directory under `internal/integration/testdata/` with a `Dockerfile`
2. Add a test case to `TestCheck`
3. Run `UPDATE_SNAPS=true go test ./internal/integration/...` to generate snapshots

### Unit Tests

- Standard Go tests for isolated parsing and linting logic
- Use when testing pure functions that don't require CLI interaction

### Test Fixtures

Test fixtures are organized in separate directories under `testdata/` to support future context-aware features (dockerignore, config files, etc.)

## Configuration System

tally uses a cascading configuration system with the following priority (highest first):

1. **CLI flags** - Always override everything
2. **Environment variables** - `TALLY_*` prefix (e.g., `TALLY_RULES_MAX_LINES_MAX`)
3. **Config file** - Closest `.tally.toml` or `tally.toml` found walking up from the target file
4. **Built-in defaults**

### Config File Format

```toml
format = "json"

[rules.max-lines]
max = 500
skip-blank-lines = true
skip-comments = true
```

### Key Files

- `internal/config/config.go` - Config struct, loading logic, cascading discovery
- Config file names: `.tally.toml` (hidden) or `tally.toml`

## Key Flags

- `--config, -c`: Path to config file (overrides auto-discovery)
- `--max-lines, -l`: Maximum number of lines allowed (0 = unlimited)
- `--skip-blank-lines`: Exclude blank lines from line count
- `--skip-comments`: Exclude comment lines from line count
- `--format, -f`: Output format (text, json)

## Adding New Linting Rules

1. Add rule config struct to `internal/config/config.go` (under `RulesConfig`)
2. Add the rule logic to `internal/lint/rules.go` (accepting config struct)
3. Add unit tests to `internal/lint/rules_test.go`
4. Add CLI flags to `cmd/tally/cmd/check.go` (with env var sources)
5. Wire up config loading in `loadConfigForFile()` in `check.go`
6. Add integration test cases to `internal/integration/`
7. Update documentation

See the `max-lines` rule implementation as an exemplary pattern.

## Package Publishing

Published to three package managers:

- **NPM**: `@contino/tally` (with platform-specific optional dependencies)
- **PyPI**: `tally-cli`
- **RubyGems**: `tally-cli`

Publishing is handled by the `packaging/pack.rb` script and GitHub Actions.
