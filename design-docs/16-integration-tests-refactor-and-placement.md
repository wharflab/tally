# Integration Tests Refactor and Placement Strategy

## 1. Decision

Refactor `internal/integration/integration_test.go` into focused files in the **same package** (`package integration`) with shared harness helpers.

This keeps runtime/perf characteristics effectively unchanged while making the suite maintainable for future `tally/*` rules.

## 2. Why This Refactor

Current state:

- `internal/integration/integration_test.go` is ~1.7k LOC and mixes bootstrap, runners, data tables, and one-off scenarios.
- `TestCheck` and `TestFix` each contain large inline case tables plus execution/snapshot logic.
- Command execution and assertions are repeated across tests.

Risks:

- Low discoverability and high merge-conflict surface.
- Hard to decide where new rule tests belong.
- Higher chance of accidental inconsistency in command/env/snapshot handling.

## 3. Constraints and Invariants

Keep these invariant during refactor:

1. **Same package**: all files remain under `internal/integration` and `package integration`.
2. **Single `TestMain`**: one binary build and one mock registry setup for the package.
3. **Stable test names**: preserve `t.Run` names to keep snapshot file names stable.
4. **No fixture path breakage**: existing `testdata` and `__snapshots__` paths continue to work.
5. **Parallelism preserved**: keep `t.Parallel()` usage where already present.

This split is Go-idiomatic and does not introduce a meaningful performance penalty because `go test` still builds one test binary per package.

## 4. Target File Layout

```text
internal/integration/
├── main_test.go                         # TestMain + binary/mock-registry bootstrap
├── harness_test.go                      # shared runners, env wiring, snapshot helpers
├── check_test.go                        # TestCheck entrypoint + runner invocation
├── check_cases_core_test.go             # config/discovery/format/fail-level + generic checks
├── check_cases_buildkit_compat_test.go  # BuildKit compatibility fixtures (mostly stable)
├── check_cases_hadolint_compat_test.go  # Hadolint compatibility fixtures (mostly stable)
├── check_cases_tally_static_test.go     # new tally/* static checks (no external I/O)
├── check_cases_tally_context_test.go    # tally/* rules needing --context / filesystem
├── check_cases_tally_async_test.go      # tally/* rules needing --slow-checks / registry/network
├── check_cases_tally_crossrule_test.go  # multi-rule interactions and suppression/supersession
├── version_test.go                      # TestVersion
├── fix_test.go                          # TestFix entrypoint + runner invocation
├── fix_cases_buildkit_compat_test.go    # existing BuildKit fix compatibility cases
├── fix_cases_hadolint_compat_test.go    # existing Hadolint fix compatibility cases
├── fix_cases_tally_test.go              # new tally/* fix cases
├── fix_crossrule_test.go                # multi-rule fix ordering/conflict tests
├── fix_realworld_test.go                # larger scenario regression tests
└── benchmark_test.go                    # benchmarks
```

Notes:

- BuildKit/Hadolint suites are now mostly compatibility/regression coverage.
- Most future changes should land in `check_cases_tally_*` and `fix_cases_tally*.go`.

## 5. Shared Harness Contract

Centralize these in `harness_test.go`:

1. Common case types (`checkCase`, `fixCase`).
2. Command helpers:
   - `runCheckCase(t, tc checkCase)`
   - `runFixCase(t, tc fixCase)`
3. Common assertions:
   - exit code handling
   - normalized snapshot matching
   - optional `afterCheck` callbacks
4. Shared utility:
   - `selectRules(...)`
   - helpers for temporary Dockerfile/config setup

Outcome: case files become data-only (or near data-only), runners stay consistent.

## 6. Refactoring Plan (Step-by-Step)

### Step 0: Baseline and Safety

1. Run current integration suite and keep a baseline.
2. Ensure snapshots are clean before moving tests.

Suggested command:

```bash
GOEXPERIMENT=jsonv2 go test ./internal/integration/...
```

### Step 1: Extract Bootstrap

1. Move globals + `TestMain` + mock registry setup into `main_test.go`.
2. Keep behavior byte-for-byte equivalent.
3. Run integration tests; expect zero snapshot changes.

### Step 2: Extract Harness

1. Move `selectRules` and repeated command/assertion logic into `harness_test.go`.
2. Keep `TestCheck`/`TestFix` still in one file but call helpers.
3. Verify no behavior changes.

### Step 3: Split `TestCheck` by Domain

1. Create case catalogs in `check_cases_*.go` grouped by domain.
2. Keep a single `TestCheck` runner in `check_test.go` that concatenates catalogs.
3. Preserve each case `name` string exactly.

### Step 4: Split `TestFix` by Domain

1. Create `fix_cases_*.go` case catalogs.
2. Keep `TestFix` runner in `fix_test.go`.
3. Move one-off scenarios into dedicated files (`fix_realworld_test.go`, etc.).

### Step 5: Enforce Placement Rules for New Work

1. Add this document to review checklist.
2. New tests must be added to the correct catalog file per decision tree below.

### Step 6: Final Validation

Run full checks:

```bash
GOEXPERIMENT=jsonv2 go test ./internal/integration/... ./internal/...
GOEXPERIMENT=jsonv2 make test
```

If output shape intentionally changes:

```bash
UPDATE_SNAPS=true GOEXPERIMENT=jsonv2 go test ./internal/integration/...
```

## 7. Placement Policy for Future `tally/*` Rules

BuildKit/Hadolint are feature-complete in this project context. Treat them as compatibility baselines; treat `tally/*` as active development.

For every new `tally/*` rule:

1. Add or update **unit tests** near the rule implementation (primary logic coverage).
2. Add **integration check** coverage in `internal/integration/check_cases_tally_*.go`:
   - static/context/async/crossrule bucket selected by decision tree.
3. If rule is fixable, add **integration fix** coverage in `fix_cases_tally_test.go` or `fix_crossrule_test.go`.
4. Add dedicated scenario test only when table-driven format becomes unnatural.

### 7.1 Buckets

- `check_cases_tally_static_test.go`: no context, no network, deterministic CLI behavior.
- `check_cases_tally_context_test.go`: requires `--context`, filesystem, `.dockerignore`, discovery interactions.
- `check_cases_tally_async_test.go`: requires `--slow-checks`, registry/network/mocked async behavior.
- `check_cases_tally_crossrule_test.go`: multiple rules interacting (suppression, supersession, ordering).
- `fix_cases_tally_test.go`: single-rule fix behavior.
- `fix_crossrule_test.go`: fix priority/order conflicts or multi-rule edit overlap.

## 8. Decision Tree (Where Should This Test Go?)

```text
Start: I changed or added behavior
|
|-- Is this only parser/semantic/helper logic (no CLI contract)?
|      |-- Yes -> unit test in internal/<pkg>/*_test.go (stop)
|      |-- No  -> continue
|
|-- Is this a BuildKit/Hadolint compatibility regression?
|      |-- Yes -> check_cases_buildkit_compat_test.go or check_cases_hadolint_compat_test.go
|      |-- No  -> continue (assume tally/*)
|
|-- Does it require --fix / --fix-unsafe?
|      |-- Yes
|      |    |-- Single-rule fix -> fix_cases_tally_test.go
|      |    |-- Multi-rule ordering/conflict -> fix_crossrule_test.go
|      |
|      |-- No (check-only)
|           |-- Needs --slow-checks / registry / async timeout behavior?
|           |      |-- Yes -> check_cases_tally_async_test.go
|           |      |-- No  -> continue
|           |
|           |-- Needs --context, discovery, .dockerignore, filesystem context?
|           |      |-- Yes -> check_cases_tally_context_test.go
|           |      |-- No  -> continue
|           |
|           |-- Interacts with other rules (suppression/supersession/priority)?
|           |      |-- Yes -> check_cases_tally_crossrule_test.go
|           |      |-- No  -> check_cases_tally_static_test.go
|
|-- Is assertion shape not table-friendly (complex custom setup/assertions)?
       |-- Yes -> dedicated *_test.go scenario file + small helper reuse
       |-- No  -> keep in the appropriate case catalog
```

## 9. Fixture and Naming Conventions

### 9.1 `testdata` Placement

Use this naming for new fixtures:

- `internal/integration/testdata/tally-<rule-slug>-<scenario>/Dockerfile`
- Optional `.tally.toml` in same fixture directory.
- Additional context files only when needed for context-aware rules.

Examples:

- `tally-no-unreachable-stages-basic`
- `tally-prefer-vex-attestation-config-override`
- `tally-foo-rule-crossrule-bar`

### 9.2 Test Case Naming

- Keep `name` short and stable; snapshot file names depend on it.
- Prefix by behavior domain when helpful (`slow-checks-...`, `context-...`, `fix-...`).
- Do not rename case names unless snapshot churn is acceptable.

### 9.3 Snapshot Discipline

1. Prefer stable JSON output where possible.
2. Normalize platform-specific paths/line endings in harness helpers.
3. Keep snapshot extension explicit for non-JSON formats.

## 10. When to Use Dedicated Scenario Tests

Use dedicated top-level tests (not big table entries) when any is true:

1. Complex multi-step setup/teardown.
2. Unique assertions not reusable via `afterCheck`.
3. Real-world regression fixture that should read like a narrative test.
4. Cross-component behavior that would obscure case-table readability.

Otherwise, prefer case catalogs.

## 11. Review Checklist for New Rules

For each new `tally/*` rule PR:

1. Unit tests added/updated near rule package.
2. Integration check case added to the correct `check_cases_tally_*` file.
3. Fix case added if rule has fix support.
4. Cross-rule test added if behavior depends on ordering/suppression/conflicts.
5. Snapshot updates reviewed for intentional changes only.
6. No new ad-hoc harness duplication.

## 12. Success Criteria

The refactor is complete when:

1. `internal/integration/integration_test.go` is removed or reduced to small compatibility shims.
2. Case catalogs are separated by domain with shared harness helpers.
3. Engineers can place a new `tally/*` test via the decision tree without guessing.
4. Integration runtime remains comparable to pre-refactor runs.
