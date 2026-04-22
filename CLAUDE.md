# CLAUDE.md

High-signal notes for AI contributors working on `tally`.
For repo layout, commands, and config details, start with `AGENTS.md`.
The repo also includes an LSP server (`internal/lsp/`), IDE integrations (`_integrations/`),
and a WASM-compiled shellcheck (`internal/shellcheck/`).

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
  - `parser.Node.Value` is uppercase (`"ENV"`, `"RUN"`); compare via `strings.ToLower` or `command.*` constants.
  - ENV nodes are parsed as `(key, value, separator)` triples — walking `node.Next` as flat tokens gives wrong keys.
  - Heredoc `RunCommand.Location()` is multi-range: `[0]` = `RUN <<EOF` opener, `[1]` = first body line. `run.Files[0].Data` holds the body.
- Keep `cmd/` as wiring only; put implementation in `internal/`.
- When running `go build`, `go test`, or `go run` directly, pass
  `-tags 'containers_image_openpgp,containers_image_storage_stub,containers_image_docker_daemon_stub'`.
  - `make` targets handle this automatically.
- `internal/shellcheck/wasm/shellcheck.wasm` is `.gitignored` — build it with `make shellcheck-wasm` (requires Docker).
  - `make build`/`make test`/`make lint`/`make deadcode` fail fast with a pointer if the wasm is missing.
  - Bump upstream versions via `_tools/shellcheck-wasm/versions.env` (`SHELLCHECK_VERSION`, `GHC_WASM_META_COMMIT`,
    `AST_GREP_VERSION`) — that file is the single source of truth shared by the Makefile and the CI composite action.

## Snapshots (Maintainer Preference)

- If you change output, update snapshots intentionally:
  - `UPDATE_SNAPS=true go test ./internal/integration/...`
- If you touch fixes/formatting, add/update a snapshot of the fixed Dockerfile too.
  - Fix tests snapshot the final file with `snaps.Ext(".Dockerfile")` under `internal/integration/__snapshots__/`.
- When adding new cases, copy existing patterns in `internal/integration/integration_test.go`.

## Rules & Fixes

- Rule config: `internal/config/`. Rule logic: `internal/lint/`. Flags/env wiring: `cmd/tally/cmd/lint.go`.
- If a rule needs derived stage/run knowledge, check `internal/facts/` first.
  - Use `input.Facts` (`*facts.FileFacts` via type assertion) for shared derived state such as effective `ENV`, active `SHELL`, parsed `RUN`
    commands, install/package heuristics, and cache/registry signals.
  - Extend `internal/facts/` when multiple rules would otherwise re-derive the same heuristic; rules should consume facts, not mutate them.
- New behavior should come with an integration fixture under `internal/integration/testdata/<case>/`.
- Fixes:
  - Use `Violation.WithSuggestedFix()` and pick the narrowest safety level.
  - `FixSafe` is eligible for `--fix`; `FixSuggestion`/`FixUnsafe` must stay behind `--fix-unsafe`.
  - If a fix needs external data, implement a resolver (`fix.FixResolver`) instead of doing IO/network in the rule.
  - PREFER narrow edits over whole-region replacement (e.g. delete one package token, not the whole install line).
  - Async resolvers run AFTER sync fixes; always scan the post-sync content before emitting edits — don't trust the original state.
  - In tests, apply a fix's edits back to source with `fix.ApplyFix(src, v.SuggestedFix)` (or `fix.ApplyEdits(src, edits)`) — don't hand-roll a
    reverse-order `ApplyEdit` loop.
  - tally assumes PowerShell 7+ in Windows containers, so `curl`/`wget` resolve to the binaries (no PS 5.1 alias gotcha).

## Docs & Schema

- If you add/change rule documentation, update `_docs/` (Mintlify docs).
  - Each rule has its own `.mdx` page under `_docs/rules/<namespace>/<rule-name>.mdx`.
  - Run `cd _docs && npx mint validate` to check Mintlify docs build.
  - The docs site is published from the `wharflab/docs` repository via Mintlify.
- If you change config schema, regenerate `schema.json` via `make jsonschema`.
- `make schema-gen` regenerates all schemas and generated models; `make jsonschema` (alias `schema-check`) validates them.

## Hygiene

- PREFER targeted `go test` first; run `make test` before finishing a larger change.
- PREFER `make lint`/`make lint-fix` (custom wrapper) over running `golangci-lint` directly.
  - Custom analyzers live in `_tools/customlint/`; `make lint` builds and runs them via `bin/custom-gcl`.
  - `customlint/cmdliteral` triggers on ANY `"add"`/`"run"`/`"env"`/etc. string literal under `internal/` (except `internal/shell`), even when
    it's a shell subcommand. Work around by moving the helper to `internal/shell` or by using `command.*` constants where applicable.
- Avoid `panic`/`log.Fatal` outside `main`; return errors and keep error context (`%w`).
- Avoid `//nolint` unless necessary; if used, scope it to a specific linter and add a brief reason.
- Do not run `make release`/`make publish*` unless explicitly asked.

## Release Workflow

- Release automation lives in [`.github/workflows/release.yml`](.github/workflows/release.yml).
- The workflow builds signed/release-ready binaries on native GitHub runners, aggregates `dist/`, then publishes GitHub assets plus npm/PyPI/RubyGems
  and IDE marketplace artifacts.
- When release is broken, inspect:
  - [`.github/workflows/release.yml`](.github/workflows/release.yml)
  - [`scripts/release/package_release_artifact.py`](scripts/release/package_release_artifact.py)
  - [`packaging/npm/`](packaging/npm)
  - [`packaging/pypi/`](packaging/pypi)
  - [`packaging/rubygems/`](packaging/rubygems)
