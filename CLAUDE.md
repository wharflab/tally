# CLAUDE.md - AI Contributor Notes

## What this repo is

`tally` is a fast, configurable linter for Dockerfiles/Containerfiles.

## Core principles

### 1) Reuse the container ecosystem (minimize ownership)

Before inventing types/logic, check these first:

- `github.com/moby/buildkit` (authoritative Dockerfile semantics)
  - `frontend/dockerfile/parser` and `frontend/dockerfile/instructions`
- `github.com/containers/common` (Podman/Buildah ecosystem utilities)
- `github.com/opencontainers/image-spec` (OCI types)

Rule of thumb: if an existing type/pattern fits ~80%, wrap/extend it instead of re-creating it.

### JSON v2 requirement

- The repo is on JSON v2 experiment; run Go commands with `GOEXPERIMENT=jsonv2`.
- Use `encoding/json/v2` and `encoding/json/jsontext` for JSON work.
- Do not add new `encoding/json` usage unless required for external compatibility.

### 2) Prefer modern idiomatic Go (Go 1.25)

- Use higher-level stdlib helpers instead of manual loops/parsing:
  - `slices`, `maps`, `cmp`
  - `strings.Cut*`, `strings.FieldsFunc`, `strings.Builder`
  - `filepath.WalkDir`, `fs.WalkDir`, `io.ReadAll`, `bytes.Clone`
- Avoid deprecated stdlib APIs (e.g. anything under `io/ioutil`).
- Prefer clear error handling: `errors.Is/As`, `errors.Join`, and `fmt.Errorf("...: %w", err)`.

### 3) Tests: prefer integration snapshots for user-visible behavior

Default testing instrument is an end-to-end integration test with a fixed `Dockerfile` + snapshot output:

- Add/adjust fixtures under `internal/integration/testdata/<case>/`.
- Update snapshots only when output changes intentionally:
  - `UPDATE_SNAPS=true go test ./internal/integration/...`

Use unit tests when you can prove behavior purely via isolated logic (no CLI/config discovery/output formatting involved).

## Commands (local)

- Build: `make build`
- Tests: `make test` (or `go test ./...`)
- Lint: `make lint`
- CPD: `make cpd`
- Try the CLI: `go run . check Dockerfile`

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

## Adding Auto-Fix Support

Rules can provide fixes via `Violation.WithSuggestedFix()`. Two types:

### Sync Fix (immediate, e.g., apt → apt-get)

```go
fix := &rules.SuggestedFix{
    Description: "Replace apt with apt-get",
    Safety:      rules.FixSafe,  // or FixSuggestion, FixUnsafe
    Edits: []rules.TextEdit{{
        Location: rules.NewRangeLocation(file, line, startCol, line, endCol),
        NewText:  "apt-get",
    }},
}
return rules.NewViolation(...).WithSuggestedFix(fix)
```

### Async Fix (needs external data, e.g., image digest)

```go
fix := &rules.SuggestedFix{
    Description:  "Pin image with digest",
    Safety:       rules.FixSafe,
    NeedsResolve: true,
    ResolverID:   "image-digest",  // registered FixResolver
    ResolverData: &ImageDigestData{Image: "alpine", Tag: "3.18", Location: loc},
}
return rules.NewViolation(...).WithSuggestedFix(fix)
```

Resolvers implement `fix.FixResolver` interface and register via `fix.RegisterResolver()`.

### Safety Levels

- `FixSafe` - Always correct, applied by default with `--fix`
- `FixSuggestion` - May change semantics slightly, requires `--fix-unsafe`
- `FixUnsafe` - May change behavior, requires `--fix-unsafe`

## Package Publishing

Published to three package managers:

- **NPM**: `tally-cli` (with platform-specific optional dependencies)
- **PyPI**: `tally-cli`
- **RubyGems**: `tally-cli`

Publishing is handled by the `packaging/pack.rb` script and GitHub Actions.
