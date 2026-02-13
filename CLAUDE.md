# CLAUDE.md

High-signal notes for AI contributors working on `tally`.
For repo layout, commands, and config details, start with `AGENTS.md`.

## Defaults

- ALWAYS add/adjust a test when behavior changes.
- PREFER end-to-end integration tests + snapshots for user-visible behavior (CLI output, config discovery, `--fix`).
- PREFER reusing container ecosystem primitives (wrap/extend vs re-implement).
- PREFER modern Go stdlib helpers (`slices`, `maps`, `cmp`, `strings.Cut*`, `errors.Join`); avoid deprecated APIs (`io/ioutil`, `sort.Slice` when
  `slices.Sort` fits).

## Non-Negotiables

- ALWAYS run Go with JSON v2 enabled (`GOEXPERIMENT=jsonv2` is already set in your environment).
  - `make` exports this automatically; set it manually when running `go` directly.
- ALWAYS use `encoding/json/v2` (and `encoding/json/jsontext`) for JSON work.
  - Do not introduce new `encoding/json` usage unless you are crossing an external API boundary.
- DO NOT write a custom Dockerfile parser.
  - Use BuildKit as the source of truth: `github.com/moby/buildkit/frontend/dockerfile/parser` and `.../instructions`.
- Keep `cmd/` as wiring only; put implementation in `internal/`.

## Snapshots (Maintainer Preference)

- If you change output, update snapshots intentionally:
  - `UPDATE_SNAPS=true go test ./internal/integration/...`
- If you touch fixes/formatting, add/update a snapshot of the fixed Dockerfile too.
  - Fix tests snapshot the final file with `snaps.Ext(".Dockerfile")` under `internal/integration/__snapshots__/`.
- When adding new cases, copy existing patterns in `internal/integration/integration_test.go`.

## Rules & Fixes

- Rule config: `internal/config/`. Rule logic: `internal/lint/`. Flags/env wiring: `cmd/tally/cmd/lint.go`.
- New behavior should come with an integration fixture under `internal/integration/testdata/<case>/`.
- Fixes:
  - Use `Violation.WithSuggestedFix()` and pick the narrowest safety level.
  - `FixSafe` is eligible for `--fix`; `FixSuggestion`/`FixUnsafe` must stay behind `--fix-unsafe`.
  - If a fix needs external data, implement a resolver (`fix.FixResolver`) instead of doing IO/network in the rule.

## Docs & Schema

- If you add/change rules or defaults, update `RULES.md`.
- If you change config schema, regenerate `schema.json` via `make jsonschema`.

## Hygiene

- PREFER targeted `go test` first; run `make test` before finishing a larger change.
- PREFER `make lint`/`make lint-fix` (custom wrapper) over running `golangci-lint` directly.
- Avoid `panic`/`log.Fatal` outside `main`; return errors and keep error context (`%w`).
- Avoid `//nolint` unless necessary; if used, scope it to a specific linter and add a brief reason.
- Do not run `make release`/`make publish*` unless explicitly asked.
