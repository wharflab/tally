# JSON v2 Migration Plan (`encoding/json/v2`)

## 1. Decision

Migrate tally from legacy `encoding/json` to Go stdlib JSON v2:

- `encoding/json/v2`
- `encoding/json/jsontext`

Do not depend on `github.com/go-json-experiment/json` in this repository.

Reason:

- The upstream module explicitly warns against depending on it in publicly available modules.
- Go 1.26 still ships the stdlib v2 implementation behind `GOEXPERIMENT=jsonv2`.

## 2. Environment Contract (Mandatory)

`encoding/json/v2` is available only when builds run with:

```bash
GOEXPERIMENT=jsonv2
```

Local verification in this repo environment (`go1.26.0`) showed:

- without `GOEXPERIMENT=jsonv2`: importing `encoding/json/v2` fails
- with `GOEXPERIMENT=jsonv2`: importing `encoding/json/v2` works

This means every build/test/lint/generate path must set `GOEXPERIMENT=jsonv2`.

## 3. Scope and Non-Goals

### In Scope

- All first-party Go packages in this repository (`cmd/`, `internal/`, `gen/`, `scripts/`)
- Test code (`*_test.go`)
- Generated + handwritten LSP protocol code under `internal/lsp/protocol/`
- CI, local tooling, and lint policy updates enforcing stdlib JSON v2

### Out of Scope

- Third-party dependencies internal JSON choices
- Immediate replacement of external libs that hide JSON internally (`sourcegraph/jsonrpc2`, etc.)

## 4. Required Build System Changes

Before changing imports, wire `GOEXPERIMENT=jsonv2` everywhere.

### 4.1 Makefile

Set `GOEXPERIMENT=jsonv2` for targets that compile or load packages:

- `build`
- `test`
- `lint`
- code generation targets

Recommended pattern:

```make
GOEXPERIMENT ?= jsonv2
export GOEXPERIMENT
```

### 4.2 GitHub Actions

Update `.github/workflows/release.yml` and any other Go workflow steps to export:

```yaml
env:
  GOEXPERIMENT: jsonv2
```

### 4.3 Lefthook / Local Hooks

Ensure pre-commit and pre-push commands run with `GOEXPERIMENT=jsonv2`.

### 4.4 Developer Onboarding

Document in `README.md` and contributor docs that Go commands must run with `GOEXPERIMENT=jsonv2`.

## 5. Import Migration

### 5.1 Replace Imports

- `encoding/json` -> `encoding/json/v2`
- `github.com/go-json-experiment/json` -> `encoding/json/v2`
- `github.com/go-json-experiment/json/jsontext` -> `encoding/json/jsontext`

### 5.2 Remove External Dependency

After code migration:

- remove `github.com/go-json-experiment/json` from `go.mod`
- clean `go.sum`

## 6. Helper Pattern (TypeScript-Go Style)

Keep direct v2 calls in most packages. Add only a small formatting helper package (`internal/jsonutil`) if needed.

Example helper:

```go
package jsonutil

import (
	"io"

	jsonv2 "encoding/json/v2"
	"encoding/json/jsontext"
)

func MarshalIndent(in any, prefix, indent string) ([]byte, error) {
	if prefix == "" && indent == "" {
		return jsonv2.Marshal(in)
	}
	return jsonv2.Marshal(in, jsontext.WithIndentPrefix(prefix), jsontext.WithIndent(indent))
}

func MarshalIndentWrite(out io.Writer, in any, prefix, indent string) error {
	if prefix == "" && indent == "" {
		return jsonv2.MarshalWrite(out, in)
	}
	return jsonv2.MarshalWrite(out, in, jsontext.WithIndentPrefix(prefix), jsontext.WithIndent(indent))
}
```

## 7. Behavior Differences to Handle

Compared to legacy `encoding/json`, v2 behavior differs and can break tests/output if migrated mechanically.

1. `omitempty` semantics are different.

- For scalar fields that must preserve legacy omission behavior, add `omitzero`.

2. Map ordering is non-deterministic by default.

- Use `jsonv2.Deterministic(true)` when output text stability matters.

3. Duplicate names are rejected by default.

- Use `jsontext.AllowDuplicateNames(true)` only where intentionally required.

4. Nil map/slice marshal behavior can differ.

- Set explicit options where wire format compatibility matters.

## 8. Linter Enforcement

Enable `depguard` and forbid legacy/old imports:

- `encoding/json$` (legacy package)
- `github.com/go-json-experiment/json$`
- `github.com/go-json-experiment/json/v1$`

Example policy:

```yaml
linters:
  settings:
    depguard:
      rules:
        main:
          deny:
            - pkg: 'encoding/json$'
              desc: 'Use stdlib v2 package "encoding/json/v2" instead.'
            - pkg: 'github.com/go-json-experiment/json$'
              desc: 'Use stdlib v2 package "encoding/json/v2" instead.'
            - pkg: 'github.com/go-json-experiment/json/v1$'
              desc: 'Use stdlib v2 package "encoding/json/v2" instead.'
```

Optional hardening:

- add `forbidigo` backup rule for `encoding/json`

## 9. Phased Migration Plan

### Phase 0: Tooling First

1. Wire `GOEXPERIMENT=jsonv2` in Makefile, CI, hooks, docs.
2. Verify baseline:

- `make build`
- `make test`
- `make lint`

### Phase 1: Low-Risk Import Swaps

Start with low-risk areas:

- `internal/rules/buildkit/fixes/json_args_recommended.go`
- `internal/rules/severity.go`
- tooling scripts

### Phase 2: Output-Sensitive Paths

Then migrate:

- `cmd/tally/cmd/version.go`
- `internal/reporter/json.go`

Add deterministic and formatting options as needed to preserve stable output.

### Phase 3: Config/Schema/LSP Paths

Migrate:

- `internal/rules/configutil/resolve.go`
- `gen/jsonschema.go`
- `internal/lspserver/server.go`

### Phase 4: Tag Audit

Audit all `omitempty` tags and add `omitzero` where compatibility requires it.

## 10. Testing Plan

Run after each phase:

```bash
make lint
make test
```

Focused checks:

- reporter/snapshot stability
- `tally --version --json` output shape
- LSP black-box tests (`internal/lsptest`)
- config schema error output stability

Add explicit tests for:

- scalar omission behavior (`omitempty` vs `omitzero`)
- deterministic serialized map output where text comparison is used

## 11. Risks and Mitigations

Risk: missing `GOEXPERIMENT=jsonv2` in any execution path causes compile failures.

Mitigation:

- centralize env in Makefile/workflows
- add CI sanity check:

```bash
go env GOEXPERIMENT | rg -q '(^|,)jsonv2(,|$)'
```

Risk: Go toolchain behavior changes across upgrades.

Mitigation:

- treat Go version bumps as explicit migration tasks
- run full test and snapshot review on each Go upgrade

## 12. Definition of Done

Migration is complete when:

1. First-party code no longer imports legacy `encoding/json`.
2. First-party code no longer imports `github.com/go-json-experiment/json` or `/v1`.
3. `depguard` enforces these constraints in CI and hooks.
4. All build/test/lint/generate paths run with `GOEXPERIMENT=jsonv2`.
5. `make lint` and `make test` are green.
6. JSON outputs are unchanged unless intentionally documented.

## 13. Useful Commands

Find legacy imports:

```bash
rg -n '"encoding/json"' --glob '*.go'
```

Find old experimental module imports:

```bash
rg -n 'go-json-experiment/json' --glob '*.go'
```

Find stdlib v2 imports:

```bash
rg -n 'encoding/json/v2|encoding/json/jsontext' --glob '*.go'
```

Find risky omission tags:

```bash
rg -n 'json:"[^"]*omitempty[^"]*"' --glob '*.go'
```
