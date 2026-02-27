# ShellCheck Partial Reimplementation Bridge and SC1040 Pilot

**Status:** Draft **Date:** 2026-02-27 **Scope:** Keep `shellcheck/*` user contract stable while moving selected SC rules from embedded ShellCheck
(WASM) to native Go checks (incrementally, rule-by-rule).

---

## 1. Current Codebase Status (Investigated)

This section reflects current repository behavior, not the older proposal language in `design-docs/23-shellcheck-integration.md`.

### 1.1 Embedded ShellCheck architecture is already in production

- ShellCheck is embedded as WASM at `internal/shellcheck/wasm/shellcheck.wasm`.
- Runtime wrapper is `internal/shellcheck/runner.go` (wazero, in-process, JSON1 parser).
- Build pipeline is in:
  - `_tools/shellcheck-wasm/Dockerfile`
  - `Makefile` targets `update-shellcheck-wasm` and `update-shellcheck-wasm-host`
- Build-time ast-grep rewrites are already applied from `_tools/shellcheck-wasm/rewrites/*.yml` with preflight/idempotence enforcement.

### 1.2 ShellCheck rule surface in tally

- Engine rule code: `shellcheck/ShellCheck` (`internal/rules/shellcheck/shellcheck.go`).
- It currently lints shell snippets extracted from:
  - `RUN` (shell form)
  - `ONBUILD RUN`
  - Shell-form `CMD`
  - Shell-form `ENTRYPOINT`
  - `HEALTHCHECK CMD-SHELL`
- Extraction includes Dockerfile-aware mapping helpers (`internal/rules/shellcheck/script.go`, `blank.go`) with location preservation.

### 1.3 Reporting pipeline already supports SC-level compatibility

- Findings are emitted as `shellcheck/SC####`.
- Doc URL is upstream ShellCheck wiki (`internal/rules/violation.go`, `ShellcheckDocURL`).
- Suggested fixes from JSON1 replacements are already converted to tally edits.
- Output processors (severity override, enable filter, inline directives) operate by final `RuleCode`, so SC rule behavior is controlled uniformly
  regardless of source.

### 1.4 Config/selection coupling is already in place

- `rules.shellcheck` namespace exists in config and schema.
- Pattern config supports `SC####` keys via schema pattern properties.
- Include coupling is implemented: selecting `shellcheck/SC2086` enables `shellcheck/ShellCheck` engine.
  - This logic is in `internal/config/rules.go`.

### 1.5 Source rewrite mechanism is established and stable

- Rewrites are applied in lexical order.
- Each rewrite bundle is enforced to match exactly once (or `-multi` rules >=1), then must become idempotent.
- Existing dropped emissions include SC2148/SC2187/SC1090/SC1091.

### 1.6 Important current limitations (relevant for bridge)

- No explicit “native SC rule ownership” layer exists yet.
- `internal/rules/shellcheck/index.schema.json` includes `ShellCheckInternalError`, but not `ShellCheckPreflightParseError` despite emission in code.
- Runner supports include/exclude/optional flags, but current rule wiring uses fixed options (`Severity=style`, `Norc=true`) and does not currently
  project per-rule ownership into runner include/exclude.

---

## 2. Target Problem

We want to replace selected ShellCheck rules with Go-native checks incrementally, while preserving:

1. Rule IDs (`shellcheck/SC####`)
2. Existing config behavior (`[rules.shellcheck.SC####]`, `--select`, `--ignore`, inline ignore)
3. Existing output/reporting semantics (CLI and LSP)
4. Ability to continue using upstream ShellCheck for all non-migrated SC rules

This must be low-risk and reversible.

---

## 3. Bridge Design

### 3.1 Core idea: “single namespace, dual implementation source”

Keep one public namespace (`shellcheck/SC####`), but allow diagnostics to come from:

- Embedded ShellCheck WASM (default source)
- Native Go checker for selected SC codes

Ownership is per SC code.

### 3.2 Ownership model

Introduce a table in `internal/rules/shellcheck`:

- `SCxxxx -> source`
- Sources:
  - `wasm` (default)
  - `native`

Initial state: all `wasm`, except pilot `SC1040 -> native`.

### 3.3 Conflict prevention (no duplicate diagnostics)

For each code migrated to native:

1. Remove emission from embedded ShellCheck using existing rewrite mechanism.
2. Emit same `shellcheck/SC####` from Go checker.

This keeps deduplication simple and avoids “double emit then dedupe” ambiguity.

### 3.4 Why this preserves config/reporting

Because processors consume only final `Violation.RuleCode`, behavior is unchanged if code stays identical:

- `rules.shellcheck.SC1040.severity = ...` still applies.
- `# tally ignore=SC1040` still applies.
- `--select shellcheck/SC1040` still works via existing include coupling.
- LSP diagnostics and code actions continue to use the same code/doc URL patterns.

### 3.5 Execution flow after bridge

For each extracted script snippet:

1. Run native checkers for codes owned by `native`.
2. Run embedded ShellCheck (with dropped emissions already removed at build time).
3. Convert both outputs into unified `[]rules.Violation` with identical namespace format.

### 3.6 Compatibility with Windows/Shell Gating

This bridge does not change rule applicability gates described in
`design-docs/27-windows-container-rules.md` (“How Rules Fire”):

- `shellcheck/SC*` diagnostics remain shell-gated via `IsShellCheckCompatible()`.
- Non-POSIX shells (PowerShell/cmd) continue to skip SC checks.
- Whether a specific SC code is emitted by WASM or native Go is an implementation detail; the
  user-visible rule code and gating behavior stay the same.

---

## 4. Concrete Pilot: SC1040

### 4.1 SC1040 semantics (upstream)

SC1040 message:

> `When using <<-, you can only indent with tabs.`

It fires on heredoc end-token lines when:

- heredoc operator is `<<-`
- end token line has leading indentation containing spaces (not tab-only)
- line is otherwise just the end token (no trailing characters)

Reference points validated from upstream:

- ShellCheck wiki page `SC1040`
- ShellCheck parser source (`src/ShellCheck/Parser.hs`) where `parseProblemAt ... 1040 ...` is emitted

### 4.2 Upstream Implementation and Test Discovery Protocol (Required)

For ShellCheck ports, discovery cannot assume one rule = one file. Use this protocol:

1. Pin and inspect the exact upstream tag used by tally (`v0.11.0` today).
2. Locate implementation by numeric code search:
   - `rg -n "1040|SC1040" src test`
3. If code search is sparse, pivot by parser/check function:
   - For SC1040: `readHereDoc` branch in `src/ShellCheck/Parser.hs`
4. Collect original test vectors from all relevant upstream locations.

Organization examples:

- Well-organized-ish semantic checks often split between:
  - implementation in `src/ShellCheck/Analytics.hs` / `src/ShellCheck/Checks/*`
  - expectations in `src/ShellCheck/Checker.hs` property tests (example: SC2086 has many `check ... == [2086]` cases).
- Parser diagnostics can be less rule-centric:
  - implementation and parser properties are co-located in `src/ShellCheck/Parser.hs`
  - SC1040 is in `readHereDoc`; relevant upstream tests are in the nearby heredoc property cluster (`prop_readHereDoc*`), not a dedicated `SC1040Spec`
    file.

Porting rule for tally (same standard we use for Hadolint ports):

- Port **all original upstream test vectors** relevant to the migrated rule branch, not only direct `SC1040` string matches.
- For SC1040, this includes the full set of heredoc cases around the `readHereDoc` branch needed to verify both triggering and non-triggering
  behavior.

### 4.3 Step A: drop SC1040 from embedded ShellCheck (same rewrite mechanism)

Add a new rewrite bundle:

- `_tools/shellcheck-wasm/rewrites/0007-drop-sc1040.yml`

Pattern intent: remove the Haskell expression that emits SC1040.

Skeleton:

```yaml
---
# yaml-language-server: $schema=https://raw.githubusercontent.com/ast-grep/ast-grep/main/schemas/rule.json
id: shellcheck-drop-sc1040
message: Remove SC1040 emission
severity: info
language: Haskell
rule:
  pattern:
    context: |
      module Test where
      foo = do
          parseProblemAt $POS ErrorC 1040 $MSG
    selector: exp
fix: ''
```

Then rebuild wasm via existing target and keep preflight/idempotence checks unchanged.

### 4.4 Step B: implement SC1040 checker in Go

Suggested files:

- `internal/rules/shellcheck/sc1040.go`
- `internal/rules/shellcheck/sc1040_test.go`

Suggested integration point:

- invoked from `checkShellMapping` and fallback snippet path (`checkShellSnippet`) before returning final violations

Checker contract:

- Input:
  - script text (without prelude)
  - mapping metadata (`OriginStartLine`)
  - file path
- Output:
  - `rules.Violation` with:
    - `RuleCode`: `shellcheck/SC1040`
    - message exactly matching upstream text
    - severity `error` (ShellCheck level for this parser diagnostic)
    - doc URL via `rules.ShellcheckDocURL("SC1040")`

### 4.5 SC1040 detector approach

Implement a focused scanner for dashed heredoc terminators:

1. Find pending heredoc starts using `<<-TOKEN` (including quoted token forms).
2. Track expected end token per pending heredoc.
3. While scanning subsequent lines, check candidate end-token lines.
4. Emit SC1040 when candidate line matches token with leading whitespace that includes a space (not tab-only).

Location mapping:

- Start line: offending end-token line in Dockerfile (`OriginStartLine + offset`)
- Column: first leading whitespace char on that line (0-based)
- Point location (start == end), matching parse-style diagnostics

### 4.6 SC1040 autofix integration (required for pilot)

This pilot should demonstrate native `shellcheck/SC*` integration with tally’s fix infrastructure.

Fix behavior:

1. On each SC1040 violation, compute the leading horizontal whitespace prefix on the offending end-token line.
2. Replace that prefix with tabs-only content (drop spaces, keep existing tabs).
3. If the prefix was spaces-only, this becomes an empty prefix (end token moves to column 0).

Suggested fix shape:

- `SuggestedFix.Description`: `Normalize <<- heredoc terminator indentation (tabs only)`
- `Safety`: `FixSafe` (deterministic syntax correction)
- `IsPreferred`: `true`
- Single `TextEdit` per violation for the line prefix range

This makes SC1040 fixes available in:

- CLI `--fix` (no `--fix-unsafe` required for `FixSafe`)
- LSP quick-fix/code action flow (already wired for `SuggestedFix`)

### 4.7 Why SC1040 is a good first migration

- Narrow parser-level check with deterministic message.
- Includes a deterministic autofix, which exercises native `SC` fix plumbing end-to-end.
- Low coupling with dataflow/CFG.
- Exercises full bridge lifecycle (rewrite + native emit + compatibility).

---

## 5. Implementation Checklist

### 5.1 Bridge infrastructure

- Add native-check registry interface in `internal/rules/shellcheck`.
- Add code ownership map (`SC -> native/wasm`).
- Add helper to merge native + wasm findings into one `[]rules.Violation`.

### 5.2 SC1040 migration

- Add `_tools/shellcheck-wasm/rewrites/0007-drop-sc1040.yml`.
- Rebuild wasm with existing make target.
- Implement `sc1040.go` checker and wire into rule flow.
- Implement `sc1040_test.go` with full upstream test-vector coverage.
- Implement SC1040 `SuggestedFix` generation and integration tests for `--fix`.

### 5.3 Schema/config consistency

- Keep `SC####` pattern behavior unchanged (already present).
- Consider adding `ShellCheckPreflightParseError` to `internal/rules/shellcheck/index.schema.json` for consistency with emitted meta rule.

---

## 6. Test Plan for SC1040 Pilot

### 6.1 Unit tests (native checker)

Port all relevant upstream test vectors and add targeted native tests:

- Cases corresponding to upstream heredoc parser properties near `readHereDoc` (triggering and non-triggering).
- `<<-` + tab-indented end token -> no violation.
- `<<-` + space-indented end token -> SC1040 violation.
- `<<` (undashed) + indentation case should not emit SC1040.
- End token with trailing text should not emit SC1040 (belongs to other codes).
- Multiple heredocs in one snippet.
- Autofix-specific tests:
  - spaces-only prefix -> prefix removed
  - mixed tabs+spaces prefix -> spaces removed, tabs preserved
  - produced edit range and replacement are stable

### 6.2 Integration tests (end-to-end)

Add fixture(s) that are valid Dockerfiles and exercise shell snippets capable of producing SC1040 in extracted script context.

Assertions:

- Rule code is `shellcheck/SC1040`
- Message text matches upstream
- Location is precise and stable
- `--select shellcheck/SC1040` works
- `# tally ignore=SC1040` suppresses it
- `rules.shellcheck.SC1040.severity = "off"` suppresses it
- `--fix` applies SC1040 auto-fix correctly with stable snapshot output

### 6.3 Regression guard: parity window

Temporary (migration-phase) check:

- Before dropping from wasm, compare native SC1040 detection against wasm SC1040 on a corpus and record mismatches.
- After confidence, keep rewrite drop and remove parity-only harness.

---

## 7. Rollout Strategy

1. Land bridge scaffolding + SC1040 native checker behind a dev toggle (optional).
2. Land SC1040 rewrite drop and enable native path by default.
3. Monitor snapshots and real-world reports.
4. Repeat for next candidate SC codes.

Rollback is simple:

- Remove/disable native ownership for SC1040.
- Revert SC1040 rewrite bundle.
- Rebuild wasm.

---

## 8. Candidate Selection Guidance for Next SC Rules

Prefer next rules with these properties:

- Parse-local or AST-local checks (no deep dataflow).
- Deterministic location mapping.
- Stable message semantics.
- Useful in Dockerfile snippet context.

Avoid early migration of rules heavily dependent on full ShellCheck analysis graph.

---

## 9. Summary

The repository already has the critical primitives needed for incremental ShellCheck replacement:

- stable `shellcheck/SC####` namespace,
- embedded ShellCheck rewrite pipeline,
- precise source mapping,
- unified processors for config/directives/reporting.

The missing piece is a small ownership bridge that lets specific SC codes move from WASM to native Go while remaining invisible to users as a
behavioral contract change.

SC1040 is a suitable first pilot to validate this migration model end-to-end.
