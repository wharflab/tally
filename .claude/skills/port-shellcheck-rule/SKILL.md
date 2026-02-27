---
name: port-shellcheck-rule
description: Port a ShellCheck SC rule to native Go in tally while preserving shellcheck/SC#### compatibility (config, reporting, fixes, and docs ownership).
argument-hint: SC rule code (e.g. SC1040)
disable-model-invocation: true
allowed-tools: Read, Write, Edit, Grep, Glob, Bash(go *), Bash(make update-shellcheck-wasm), Bash(git status), mcp__github__get_file_contents, mcp__github__search_code
---

# Port ShellCheck Rule to Native Go

You are porting one ShellCheck rule from embedded ShellCheck (WASM) to native Go implementation in `tally`.

Rule argument: `$ARGUMENTS` (for example `SC1040`).

This workflow must preserve the public rule contract:

- Rule code remains `shellcheck/$ARGUMENTS`
- Existing config keeps working (`[rules.shellcheck.$ARGUMENTS]`, `--select`, `--ignore`, inline ignores)
- LSP and CLI reporting keep the same rule code and severity semantics
- Embedded ShellCheck remains active for non-migrated SC rules

## Non-negotiables

1. Port ALL relevant upstream test vectors, not only one happy path.
2. Remove migrated rule emission from embedded ShellCheck via ast-grep rewrite.
3. Prefer parser-backed implementation (`mvdan.cc/sh/v3` and existing Dockerfile mapping) over string heuristics.
4. Parse errors own parse diagnostics; do not continue native SC linting after parse failure.
5. If auto-fix is possible, implement it with minimal, low-conflict edits.
6. Own documentation for migrated rules under `docs/rules/shellcheck/` and link diagnostics there.

Reference: `design-docs/28-shellcheck-go-reimplementation-bridge-sc1040.md`

Compatibility note: keep behavior aligned with `design-docs/27-windows-container-rules.md` ("How Rules Fire").

## Step 1: Identify Upstream Implementation and Tests

ShellCheck sources are not always organized as one-rule-per-file.

1. Pin upstream version used by tally (currently ShellCheck `v0.11.0`, see `_tools/shellcheck-wasm/Dockerfile`).
2. Find implementation by code search:
   - `rg -n "$ARGUMENTS|<numeric-code>" src test` (example for SC1040: `1040`)
3. If not obvious, pivot by parser/check entry points:
   - parser diagnostics: `src/ShellCheck/Parser.hs`
   - semantic checks: `src/ShellCheck/Analytics.hs` and `src/ShellCheck/Checks/*`
4. Collect ALL relevant upstream tests:
   - property tests in parser/check modules
   - direct checker assertions in `Checker` tests
5. Record exact source/test locations in your PR description.

Use GitHub MCP when convenient:

- `mcp__github__search_code` in `repo:koalaman/shellcheck`
- `mcp__github__get_file_contents` for discovered paths

### Alternative discovery path (GitHub MCP-first, verified)

If local `rg` is noisy or unavailable, use MCP code search first, then fetch exact files.

Verified query patterns that work against `koalaman/shellcheck`:

1. Find implementation by numeric code:
   - `mcp__github__search_code` query:
     - `repo:koalaman/shellcheck 1040 path:src/ShellCheck language:Haskell`
   - Expected: `src/ShellCheck/Parser.hs` for SC1040.

2. Find nearby upstream vectors/properties:
   - `mcp__github__search_code` query:
     - `repo:koalaman/shellcheck prop_readHereDoc2`
   - Expected: same parser file with heredoc property tests.

3. Fetch pinned file content at upstream tag:
   - `mcp__github__get_file_contents`:
     - `owner=koalaman`, `repo=shellcheck`, `path=src/ShellCheck/Parser.hs`, `ref=refs/tags/v0.11.0`

Fallback sequence when no hits:

1. Drop path filter and search broader:
   - `repo:koalaman/shellcheck <code-or-symbol>`
2. Search by parser/check function names (`readHereDoc`, `parseProblemAt`, `check*`).
3. Search tests by property/assertion names found in implementation.

## Step 2: Add Rewrite to Drop WASM Emission

Create a rewrite bundle under:

- `_tools/shellcheck-wasm/rewrites/`

Naming convention:

- zero-padded sequence, for example `0008-drop-$ARGUMENTS.yml`

Requirements:

1. Match only the specific Haskell expression that emits this rule.
2. Keep rewrite idempotent and deterministic.
3. Rebuild wasm:
   - `make update-shellcheck-wasm`
4. Ensure rewrite preflight/apply/postflight pass.

## Step 3: Implement Native Rule in Go

Use colocated files in shellcheck package:

- `internal/rules/shellcheck/<rule-lower>.go`
- `internal/rules/shellcheck/<rule-lower>_test.go`

Example: `SC1040 -> sc1040.go`, `sc1040_test.go`.

Wire ownership/dispatch via existing native shellcheck ownership map and checker runner in `internal/rules/shellcheck/`.

### Parser-first implementation guidance

1. Reuse script extraction/mapping from existing shellcheck pipeline (`script.go`, `shellcheck.go`).
2. Parse shell snippet with `mvdan.cc/sh/v3/syntax`.
3. Walk parsed AST to find relevant nodes for your rule.
4. Reuse parser-derived locations/tokens whenever possible.
5. Avoid ad-hoc regex/string tokenization when AST can provide structure.

### Parse error behavior

- If snippet parse fails, do not attempt native SC detection as fallback.
- Parse-status rule owns diagnostics for parse failures.
- Keep this consistent in unit tests.

### Violation requirements

For each finding:

- `RuleCode`: `shellcheck/$ARGUMENTS`
- Message: match upstream wording unless project explicitly changes it
- Severity: match upstream level mapping
- `DocURL`: use tally docs for migrated native rules:
  - `rules.TallyDocURL("shellcheck/$ARGUMENTS")`

## Step 4: Implement Auto-fix (when safe)

If rule is deterministically fixable, add `SuggestedFix`.

Fix requirements:

1. Prefer narrow edits over broad rewrites.
2. Emit as many edits as needed to minimize conflict surface.
3. Keep edits local to the violating span.
4. Use `FixSafe` only when behavior is clearly preserved.

Example pattern for whitespace cleanup rules:

- delete only offending space runs
- preserve non-offending characters (for example tabs)
- use empty replacement for removal edits

## Step 5: Unit Tests (Mandatory)

In `internal/rules/shellcheck/<rule-lower>_test.go`:

1. Port ALL upstream vectors relevant to the rule branch.
2. Include both positive and negative cases.
3. Validate full violation contract:
   - rule code
   - message
   - severity
   - doc URL
   - line/column mapping
4. Validate fix contract (if present):
   - edit count
   - precise ranges
   - replacement text
5. Add edge cases for mixed whitespace / quoting / nested forms as applicable.

Run:

- `GOEXPERIMENT=jsonv2 go test ./internal/rules/shellcheck -count=1`

## Step 6: Integration Tests and Snapshots

Add or update fixtures under:

- `internal/integration/testdata/<case>/Dockerfile`

Add lint and fix coverage:

1. `TestLint` case proving detection and mapped location.
2. `TestFix` case proving `--fix` behavior.
3. For new fix mechanics, add one standalone integration test that snapshots the fixed Dockerfile end-to-end.

Update snapshots intentionally:

- `UPDATE_SNAPS=true GOEXPERIMENT=jsonv2 go test ./internal/integration -run '<pattern>' -count=1`

Then re-run without `UPDATE_SNAPS`.

## Step 7: Documentation Ownership

For migrated native ShellCheck rules, own docs in this repo.

Create/update:

1. `docs/rules/shellcheck/index.md`
2. `docs/rules/shellcheck/$ARGUMENTS.md`
3. `docs/rules/index.md` namespace links (if needed)

Doc page should include:

- Rule title: `shellcheck/$ARGUMENTS`
- Severity/category/default/auto-fix table
- Description and rationale
- Problematic and corrected examples
- Auto-fix behavior details
- Upstream reference link

Ensure emitted `DocURL` points to this page.

## Step 8: Validate End-to-End

Minimum checks:

1. `GOEXPERIMENT=jsonv2 go test ./internal/rules/shellcheck -count=1`
2. `GOEXPERIMENT=jsonv2 go test ./internal/integration -run '<new shellcheck cases>' -count=1`
3. If wasm changed, rebuild done successfully:
   - `make update-shellcheck-wasm`
4. Verify no unexpected drift:
   - `git status --short`

## Step 9: PR Checklist

Before opening PR, verify:

- [ ] Native rule implemented in `internal/rules/shellcheck/` with colocated tests
- [ ] Upstream test vectors fully ported
- [ ] ast-grep rewrite added and wasm rebuilt
- [ ] Rule code remains `shellcheck/$ARGUMENTS`
- [ ] Parse errors do not trigger fallback native linting
- [ ] Auto-fix is minimal and conflict-aware (if implemented)
- [ ] Integration snapshots updated
- [ ] Docs page added and `DocURL` points to tally docs
- [ ] Design alignment with windows rule-firing model preserved
