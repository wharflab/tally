---
name: create-tally-rule
description: Implement a new custom `tally/*` Dockerfile lint rule end-to-end (rule code, detection logic, tests, realistic fixtures, snapshots, and docs). Use when a user describes desired behavior for a new tally-specific rule.
argument-hint: plain-language description of what the rule should detect/enforce
allowed-tools: Read, Write, Edit, Grep, Glob, Bash(go *), Bash(make *), Bash(git status), mcp__github__search_code, mcp__github__get_file_contents
---

# Create a New `tally/*` Rule

Implement a **new custom tally rule** (not BuildKit, not Hadolint) in the `tally` repository.

Input is `$ARGUMENTS`: a natural-language description of rule intent.

## Step 0: Enter Plan Mode

**Always start in plan mode**, even if the rule sounds simple. Explore the codebase first to understand:

- How existing rules handle similar constructs (heredocs, continuations, multi-stage builds)
- Cross-rule interactions and fix priority gaps
- BuildKit parser quirks (e.g., `End.Line == Start.Line` for multi-line `\` continuations)

Present the plan for approval before writing code.

## Step 0.5: Derive Rule Name From Description

Derive a concise kebab-case slug from `$ARGUMENTS` and use it consistently as `<rule_slug>`.

Rule naming requirements:

1. Prefer action-first patterns: `no-*`, `prefer-*`, `require-*`, `avoid-*`.
2. Keep it concise (typically 2-4 words).
3. Keep it descriptive and stable (avoid vague names like `best-practice-rule`).
4. Keep it unique across `internal/rules/tally/*.go` and `docs/rules/tally/*.md`.
5. Final rule code is `tally/<rule_slug>`.

## Step 1: Define Rule Contract

Pick and lock these before coding:

1. Rule code: `tally/<rule_slug>`
2. Severity: `error`, `warning`, `info`, or `style`
3. Category: `security`, `correctness`, `maintainability`, `style`, etc.
4. Default behavior:
   - Enabled by default (`DefaultSeverity != off`) or
   - Off by default (`IsExperimental: true` and enabled via config)
5. Fix strategy (**assume auto-fix is required** — most tally rules should be fixable, even if partially):
   - Sync fix (`SuggestedFix.Edits`) — default choice
   - Async fix (`NeedsResolve: true`) — only when sync cannot be reliable
6. Coordination strategy:
   - Decide precedence if this rule overlaps with existing rules
   - Decide whether to suppress this rule, suppress only its fix, or defer by priority

## Step 2: Choose Code Location

Create the rule in:

- `internal/rules/tally/<rule_slug_with_underscores>.go`

If detection logic is reusable across rules, extract helpers into focused packages:

- `internal/shell/count.go`
- `internal/shell/file_creation.go`
- `internal/runmount/runmount.go`

## Step 3: Implement Rule Skeleton

Use this structure:

```go
package tally

import (
    "github.com/tinovyatkin/tally/internal/rules"
)

type MyRule struct{}

func NewMyRule() *MyRule { return &MyRule{} }

func (r *MyRule) Metadata() rules.RuleMetadata {
    return rules.RuleMetadata{
        Code:            rules.TallyRulePrefix + "<rule_slug>",
        Name:            "...",
        Description:     "...",
        DocURL:          "https://github.com/tinovyatkin/tally/blob/main/docs/rules/tally/<rule_slug>.md",
        DefaultSeverity: rules.SeverityStyle,
        Category:        "style",
        IsExperimental:  false,
    }
}

func (r *MyRule) Check(input rules.LintInput) []rules.Violation {
    // Rule logic
    return nil
}

func init() {
    rules.Register(NewMyRule())
}
```

Notes:

- Rules self-register via `init()`; no central tally registry file exists.
- If the rule needs semantic context, use `input.Semantic.(*semantic.Model)` with nil/type guards.
- If configurable, implement:
  - `Schema() map[string]any`
  - `DefaultConfig() any`
  - `ValidateConfig(config any) error` via `configutil.ValidateWithSchema`
  - `resolveConfig()` using `configutil.Resolve(...)`

## Step 3.5: Handle Cross-Rule Interactions Early

If your rule can target the same command region as another rule, define coordination explicitly during implementation.

Use these mechanisms:

1. **Detection-time gating**:
   - Use `input.IsRuleEnabled("<other_rule_code>")` to avoid dual suggestions on the same construct.
2. **Fix-time precedence**:
   - Use `RuleMetadata.FixPriority` to enforce deterministic ordering.
   - Lower priority runs first (content edits), higher priority runs later (structural transforms).
3. **Scope partitioning**:
   - Narrow one rule to patterns it owns (for example, pure file-creation vs general chained RUN transformation).

Add regression tests for overlap behavior in both involved rule test files when practical.

## Step 3.7: Handle Heredocs and Multi-Line Instructions

Dockerfile instructions can span multiple lines via `\` continuations and heredocs (`<<EOF`, `<<-EOF`). Both detection and fix logic **must** account
for these:

1. **Continuation lines**: BuildKit's parser may report `End.Line == Start.Line` for `\`-continued instructions. Use `sourcemap.SourceMap` to scan for
   actual end lines (see `resolveEndLine` pattern).
2. **Heredoc body lines**: `RUN` and `COPY` can use heredocs. Body lines between `<<EOF` and `EOF` belong to the instruction but have their own
   indentation rules.
3. **`<<` vs `<<-`**: The `<<-` variant strips leading tabs from body lines. When a fix adds tab indentation to a heredoc instruction, it must also
   convert `<<` to `<<-` to avoid breaking the heredoc content.
4. **Test coverage**: Include explicit test cases for continuation lines and heredoc variants (both `<<` and `<<-`).

## Step 4: Use Existing Analysis Primitives

Prefer existing infrastructure over ad-hoc parsing:

- `internal/semantic` for stage/shell/variable context
- `internal/shell` for shell command parsing and command-shape detection
- `internal/sourcemap` for stable location/snippet handling
- `internal/runmount` when mount-aware behavior matters

Do not use brittle string splitting/regex if semantic/shell helpers can model the behavior.

## Step 4.5: Choose Sync vs Async Fix (Prefer Simpler)

Default to **sync fixes**. Use **async fixes** only when sync cannot be reliable.

Use sync fix when:

- Edit locations are known during `Check(...)`.
- Replacement is local and deterministic.
- No post-fix re-parse is needed.

Use async fix when:

- Correct edits depend on content **after** other fixes apply.
- A robust solution requires reparsing current file state.
- External resolution is required (network, lookup, expensive deferred computation).

If async is required:

1. Set `NeedsResolve: true`, `ResolverID`, `ResolverData`, and `Priority`.
2. Implement/register resolver under `internal/fix/`.
3. Ensure resolver computes edits from current modified content.
4. Keep async scope minimal; avoid async when a sync edit can cover the case safely.

## Step 5: Add Unit Tests

Create:

- `internal/rules/tally/<rule_slug_with_underscores>_test.go`

Follow existing pattern:

1. `Test...Metadata`
2. `Test...DefaultConfig` (if configurable)
3. `Test...ValidateConfig` (if configurable)
4. `Test...Check` with `testutil.RunRuleTests`
5. `Test...CheckWithFixes` for fix-bearing rules

If you added helper packages, add dedicated tests there too (like `internal/shell/*_test.go`).
If rule coordination exists, add explicit overlap tests (for example: competing rules enabled simultaneously).

Coverage target:

- Aim for **>=85% coverage** for each newly added rule/helper file.
- Use package coverage as gate and review file/function gaps:

```bash
go test ./internal/rules/tally/... -coverprofile=/tmp/tally.cover
go tool cover -func=/tmp/tally.cover
```

- Add focused tests for any uncovered branches in new files until the target is met.

## Step 6: Add Integration Coverage

1. Create fixture:
   - `internal/integration/testdata/<rule_slug>/Dockerfile`
   - Optional: `.tally.toml` for rule enablement/tuning

2. Add `TestCheck` case in `internal/integration/integration_test.go`:
   - Use `selectRules("tally/<rule_slug>")` to isolate behavior

3. If the rule has fixes, add/extend `TestFix` case(s).
   - Include at least one case where another relevant rule is also enabled.
   - Prefer realistic Dockerfile content instead of toy-only snippets.

4. Update snapshots:

```bash
UPDATE_SNAPS=true go test ./internal/integration/...
```

Important: adding a new enabled rule can change `rules_enabled` values and the `total-rules-enabled` snapshot.

## Step 6.5: Use Real-World Dockerfile Examples

When creating docs examples and integration fixtures, prefer life-like patterns from public repositories.

Recommended workflow (GitHub MCP):

1. Search GitHub code with a Dockerfile language filter:
   - Use `lang:Dockerfile` (or GitHub equivalent `language:Dockerfile`) plus rule-relevant keywords.
2. Pull candidate files and extract representative snippets.
3. Adapt snippets minimally for deterministic tests (small, stable, focused on the behavior under test).
4. Avoid fully synthetic fixtures when a real-world pattern exists.

## Step 7: Update Documentation

Update all of:

1. `docs/rules/tally/<rule_slug>.md` (new rule page)
2. `docs/rules/tally/index.md` (tally rules table)
3. `RULES.md`
   - tally summary table row
   - dedicated section for `tally/<rule_slug>`
   - namespace counts if changed
4. `README.md` supported rules count/table when totals change
5. `docs/index.md` tally rule count if shown

## Step 8: Validate End-to-End

Run focused tests first, then broad checks:

```bash
go test ./internal/rules/tally/... -run <rule_slug_or_test_name_fragment> -v
go test ./internal/integration/... -run <rule_slug_or_test_name_fragment> -v
go test ./...
make lint
make cpd
```

If docs changed and `zensical` is available:

```bash
zensical build --clean
```

## Step 9: Configuration + UX Checks

Confirm:

- Rule can be enabled/disabled via `[rules] include/exclude`
- Per-rule config in `[rules.tally.<rule_slug>]` works
- Violation messages are explicit and actionable
- `DocURL` resolves to the new docs page
- Fix safety level is correct:
  - `FixSafe`
  - `FixSuggestion`
  - `FixUnsafe`
- If fix overlaps with another rule, precedence is deterministic and tested.
- Sync fix is used unless async is necessary for correctness.

## Completion Checklist

- [ ] Rule implemented in `internal/rules/tally/`
- [ ] `init()` registration added
- [ ] Auto-fix implemented (sync preferred; at minimum cover the common case)
- [ ] Config schema/default/validation implemented (if configurable)
- [ ] Unit tests added for rule behavior and config
- [ ] Heredoc and `\`-continuation edge cases tested (detection + fix)
- [ ] Helper tests added for extracted utilities
- [ ] New rule/helper files are covered at >=85%
- [ ] Integration fixture + `TestCheck` case added
- [ ] `TestFix` cases added for fix-capable rules
- [ ] Fixtures/docs examples are based on realistic Dockerfile patterns
- [ ] Snapshots updated
- [ ] Docs page + docs indexes updated
- [ ] `RULES.md` and `README.md` counts/details updated
- [ ] `go test ./...`, `make lint`, and `make cpd` pass
