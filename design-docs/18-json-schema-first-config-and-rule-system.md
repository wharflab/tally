# JSON-Schema-First Config and Rule System

## 1. Decision

Adopt a schema-first architecture across tally with these hard requirements:

1. Rule and config schemas are authored as external JSON files in-repo.
2. Go config types are generated from those schemas via `github.com/omissis/go-jsonschema` tooling.
3. Runtime validation everywhere uses `github.com/google/jsonschema-go` (`Schema.Resolve` + `Resolved.Validate`).
4. Remove legacy schema dependencies:
   - `github.com/invopop/jsonschema`
   - `github.com/santhosh-tekuri/jsonschema/v6`
5. Preserve tally's JSON v2 policy: first-party and generated code in this repository must remain compliant with `encoding/json/v2` standards.

This makes schema documents the single source of truth for validation, generated types, and published schema artifacts.

---

## 2. Background and Current Pain

Current state is split:

- Top-level config schema is generated from Go structs (`gen/jsonschema.go`) through `invopop/jsonschema`.
- Rule schemas are defined in Go as `map[string]any` and validated via `santhosh-tekuri/jsonschema/v6`.
- Rule config structs and schema definitions are maintained separately.

Problems:

1. Drift risk between schema, types, and runtime validation.
2. Two schema engines and two schema authoring styles.
3. Harder onboarding for rule authors.
4. Extra dependency surface (including indirect churn).

---

## 3. Goals / Non-Goals

### Goals

1. External JSON schema files for all configurable rules and root config.
2. Runtime config validation in CLI and LSP through one validator stack.
3. Generated Go types derived from schema docs.
4. Idiomatic use of `google/jsonschema-go` everywhere schema validation is needed.
5. Preserve current config discovery and precedence (defaults -> file -> env -> overrides).

### Non-Goals

1. Rewriting all rule logic in one release.
2. Changing TOML format semantics unless explicitly documented.
3. Replacing koanf.

---

## 4. Target Architecture

### 4.1 Source of Truth

- **Canonical**: JSON schema documents checked into git.
- **Derived**: generated Go types.
- **Runtime**: embedded schema bundle + resolved validators.

### 4.2 Proposed Layout

```text
internal/
  schemas/
    root/
      tally-config.schema.json
    rules/
      tally/
        max_lines.schema.json
        consistent_indentation.schema.json
      hadolint/
        dl3001.schema.json
        dl3026.schema.json
      buildkit/
        ...
    generated/
      config/
        tally_config.gen.go
      rules/
        tally/
          max_lines.gen.go
          consistent_indentation.gen.go
        hadolint/
          dl3001.gen.go
          dl3026.gen.go
    embed/
      schemas.go
    runtime/
      loader.go
      validator.go
```

### 4.3 Runtime Validation Flow

1. koanf loads merged config from defaults/file/env/overrides.
2. Obtain merged raw object (`map[string]any`) before unmarshal.
3. Validate raw object with pre-resolved root schema (`Resolved.Validate`).
4. If valid, unmarshal to typed config.
5. If invalid, return normalized diagnostics and stop.

### 4.4 Rule Validation Flow

Preferred model:

- root schema already validates namespaced rule option trees
- rules consume typed/generated config
- per-rule validation helpers become thin wrappers or are removed

Optional strict mode can keep per-rule subtree validation for more localized diagnostics.

### 4.5 Embedding and Schema Resolution

Use `//go:embed` for all schema files and resolve `$ref` via custom loader:

- deterministic in binaries
- no runtime filesystem dependency
- shared cache of resolved schemas

---

## 5. Tooling Design (`omissis/go-jsonschema`)

### 5.1 Generation Entry Point

Provide one orchestrator command:

```bash
make schema-gen
# or:
cd _tools && go run ./schema-gen
```

This command reads a schema manifest and invokes the generator for all schema files.

### 5.2 Manifest-Driven Generation

Manifest example:

```json
{
  "schemas": [
    {
      "input": "internal/schemas/rules/tally/max_lines.schema.json",
      "output": "internal/schemas/generated/rules/tally/max_lines.gen.go",
      "package": "tallyschema"
    }
  ]
}
```

### 5.3 Version Pinning

Pin generator version and expose it via Makefile/tooling to guarantee reproducible output.

> Repository is `github.com/omissis/go-jsonschema`; module/install paths may still
> use `github.com/atombender/go-jsonschema` at a given release. Pin exact versions
> in tooling and avoid floating latest in CI.

---

## 6. JSON v2 Compliance Plan (Mandatory)

JSON v2 compliance is a non-negotiable migration requirement.

### 6.1 Contract

1. No new `encoding/json` usage in first-party code.
2. Generated code checked into this repo must pass existing depguard JSON v2 policy.
3. Runtime schema validation dependency internals may use `encoding/json`; that is acceptable as third-party boundary.

### 6.2 Generation Strategy

Use generator output mode/template strategy that emits data model types without introducing repository-level legacy JSON imports.

Implementation requirement:

- `_tools/schema-gen` must fail if generated files import `encoding/json`.
- CI gate must enforce this with a targeted check after generation.

If upstream templates are insufficient, keep an in-repo template override/post-process step under `_tools/schema-gen` so generated files remain JSON
v2 compliant.

### 6.3 CI Enforcement

Add to schema pipeline:

```bash
make schema-gen
rg -n '"encoding/json"' internal/schemas/generated && exit 1 || true
make lint
```

---

## 7. Root Config Schema Design

Root schema models all existing config surfaces:

- `rules`
  - `include` / `exclude`
  - namespaces: `tally`, `hadolint`, `buildkit`
  - shared controls: `severity`, `fix`, `exclude.paths`
  - per-rule options via `$ref`
- `output`
- `inline-directives`
- `ai`
- `slow-checks`

Strictness policy:

1. start permissive where compatibility risk exists
2. migrate to strict (`additionalProperties: false`) once diagnostics and migration guides are stable

---

## 8. `google/jsonschema-go` Idiomatic Integration

### 8.1 Runtime Package

Create `internal/schemas/runtime`:

- registry of schema IDs to embedded assets
- loader implementation for referenced schemas
- singleton resolver cache for root and rule schemas
- error normalization (`instance path`, user-friendly messages)

### 8.2 Public API

```go
type Validator interface {
    ValidateRootConfig(raw map[string]any) error
    ValidateRuleOptions(ruleCode string, raw any) error
}
```

### 8.3 Config Pipeline Wiring

Integrate in `internal/config` (`Load`, `LoadFromFile`, `LoadWithOverrides`):

1. load raw koanf state
2. validate raw map with root schema
3. unmarshal into typed config

Same validator package is reused by CLI and LSP paths.

---

## 9. Rule API Migration

Current rule interface uses:

- `Schema() map[string]any`
- `ValidateConfig(config any)`

Migration target:

- replace inline map schema with stable schema reference (`SchemaID()` or schema registry mapping)
- keep `DefaultConfig()` but return generated type
- route validation through shared runtime validator

Transitional compatibility:

- keep old methods as adapters during phased rollout
- remove adapters after all configurable rules are migrated

---

## 10. Implementation Phases

### Phase 0: Foundations

1. add schema directories
2. add schema generator tooling
3. add embed/runtime validator packages
4. add Make targets:
   - `make schema-gen`
   - `make schema-check`

### Phase 1: Rule Schemas Externalized

1. create schema file for each configurable rule
2. generate rule config types
3. switch rules to schema references
4. keep adapters where needed

### Phase 2: Root Schema and Config Validation

1. author root schema with refs to rule schemas
2. wire root validation in all config load paths
3. update LSP config override path to same validator

### Phase 3: Dependency Migration

1. migrate runtime rule validation from `santhosh-tekuri` to `google/jsonschema-go`
2. replace `invopop` schema generation pipeline with schema-first source files
3. remove dependencies from `go.mod`

### Phase 4: Cleanup and Hardening

1. remove obsolete struct-tag schema metadata
2. tighten additionalProperties policies
3. finalize migration docs and compatibility notes

---

## 11. Testing Strategy

1. Unit tests
   - loader and `$ref` resolution
   - root config validation pass/fail
   - per-rule option validation diagnostics
2. Integration tests
   - CLI config validation behavior
   - LSP override validation behavior
3. Golden tests
   - generated type files stable and deterministic
4. Compatibility tests
   - existing fixture TOML files remain valid unless explicitly deprecated

---

## 12. Risks and Mitigations

1. **Generator feature mismatch**
   - Start with pilot rules; expand after output quality validation.
2. **Breaking existing user configs**
   - Phased strictness; actionable error messages.
3. **Validation error UX regressions**
   - Central formatter + snapshot tests.
4. **Performance overhead**
   - Resolve once, cache `*Resolved` objects.
5. **JSON v2 policy violations from generated code**
   - hard CI gate and generator post-processing contract.

---

## 13. Definition of Done

Migration is complete when all conditions hold:

1. External schema files exist for root config and all configurable rules.
2. Runtime validation uses only `google/jsonschema-go` in first-party code.
3. Generated Go config types are derived from schema docs.
4. `invopop/jsonschema` and `santhosh-tekuri/jsonschema/v6` are removed.
5. CLI and LSP enforce runtime schema validation before typed decode.
6. JSON v2 policy remains enforced for generated and handwritten repository code.
