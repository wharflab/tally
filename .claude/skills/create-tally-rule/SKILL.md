---
name: create-tally-rule
description: Implement a new custom `tally/*` Dockerfile lint rule end-to-end (rule code, overlap research, fix coordination, realistic tests/fixtures, snapshots, and docs). Use when a user describes desired behavior for a new tally-specific rule.
argument-hint: plain-language description of what the rule should detect/enforce
---

# Create a New `tally/*` Rule

Implement a **new custom tally rule** (not BuildKit, not Hadolint) in the `tally` repository.

Input is `$ARGUMENTS`: a natural-language description of rule intent.

## Step 0: Enter Plan Mode and Build a Conflict Inventory

**Always start in plan mode**, even if the rule sounds simple. Explore the codebase first to understand:

- How existing rules handle similar constructs (heredocs, continuations, multi-stage builds)
- Cross-rule interactions and fix priority gaps
- BuildKit parser quirks (e.g., `End.Line == Start.Line` for multi-line `\` continuations)

Present the plan for approval before writing code. The plan must include a **conflict inventory**. Do not treat "no overlapping rules found" as the
default outcome; for tally it is usually wrong. A plan that concludes there are no overlaps must name the rules/files inspected and explain why the
lack of overlap is credible.

During planning, explicitly investigate both:

- **Direct overlaps**: rules that target the same instruction family (`RUN`, `COPY`, `ADD`, `ENV`, etc.), the same line, or the same source region.
- **Indirect overlaps**: rules that modify shared text surfaces such as whitespace, indentation, line splitting/joining, heredoc conversion, newline
  insertion/removal, package sorting, or flag insertion on the same instruction.

Minimum planning research:

- Search `internal/rules/tally/*.go` for rules touching the same instruction type or syntax surface.
- Read implementations of the closest existing rules, not just their tests/docs, to find logic that is already reusable or almost reusable.
- Search for `FixPriority`, `WithSuggestedFix`, `NeedsResolve`, and `input.IsRuleEnabled(...)` in related rules.
- Read existing combined-fix coverage in:
  - `internal/integration/fix_cases_test.go`
  - `internal/integration/fix_scenarios_test.go`
- Check docs for nearby rules when behavior boundaries are unclear.

If planning finds logic in another rule that is similar or nearly reusable for the new rule, default to **extract-and-share**, not
copy-paste-and-tweak. Plan the extraction as part of the rule work:

- move shared detection/parsing/edit-building into a helper, shared package, or facts primitive
- update both the existing rule and the new rule to consume the shared logic
- keep rule-local wrappers thin

Do **not** clone chunks of another rule and adjust them locally. CI runs copy/paste detection and review will flag duplication anyway; avoiding the
extraction up front only creates a harder refactor later.

Required planning output:

1. Proposed rule contract (`code`, severity, category, fixability)
2. Candidate overlap list with:
   - rule code
   - direct vs indirect overlap
   - shared instruction/surface
   - expected failure mode (`duplicate report`, `overlapping edits`, `priority race`, `fix should skip`, etc.)
   - intended coordination strategy
3. Specific rules that must be co-enabled in fix integration tests
4. Whether the facts layer should be reused/extended
5. Reuse/extraction candidates found in existing rules, and where the shared logic should live

During planning, explicitly evaluate whether the rule should consume or extend the
facts framework instead of deriving state ad hoc inside the rule:

- Use `input.Facts` when the rule needs reusable derived knowledge (effective
  stage env/shell/workdir, run-level command parsing, package-install detection,
  cache-related env state, cross-instruction context).
- Prefer extending `internal/facts/` when the new heuristic is likely to be
  useful to multiple rules or is expensive enough that it should be computed
  once and shared.
- Keep rule-local logic in the rule only when the heuristic is narrow,
  rule-specific, and unlikely to be reused.

Where to look first:

- `internal/facts/doc.go` for the facts model overview and intended usage
- `internal/facts/facts.go` for current file/stage/run facts
- `internal/facts/*_test.go` for examples of how facts are expected to behave

If a new rule uncovers a good reusable candidate, extend the facts layer first,
then consume it from the rule. Do not duplicate the same heuristic in multiple
rules.

## Step 0.5: Derive Rule Name From Description

Derive a concise kebab-case slug from `$ARGUMENTS` and use it consistently as `<rule_slug>`.

Rule naming requirements:

1. Prefer action-first patterns: `no-*`, `prefer-*`, `require-*`, `avoid-*`.
2. Keep it concise (typically 2-4 words).
3. Keep it descriptive and stable (avoid vague names like `best-practice-rule`).
4. Keep it unique across `internal/rules/tally/*.go` and `_docs/rules/tally/*.mdx`.
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
   - Name the specific overlapping rules from Step 0; do not leave coordination as a vague follow-up

## Step 2: Choose Code Location

Create the rule in:

- `internal/rules/tally/<rule_slug_with_underscores>.go`

If detection logic is reusable across rules, extract helpers into focused packages:

- `internal/shell/count.go`
- `internal/shell/file_creation.go`
- `internal/runmount/runmount.go`

If you are borrowing non-trivial logic from an existing rule, stop and extract it before implementing the new rule around it. "Mostly the same, with a
few tweaks" is a refactor signal, not permission to duplicate code.

## Step 3: Implement Rule Skeleton

Use this structure:

```go
package tally

import (
    "github.com/wharflab/tally/internal/rules"
)

type MyRule struct{}

func NewMyRule() *MyRule { return &MyRule{} }

func (r *MyRule) Metadata() rules.RuleMetadata {
    return rules.RuleMetadata{
        Code:            rules.TallyRulePrefix + "<rule_slug>",
        Name:            "...",
        Description:     "...",
        DocURL:          rules.TallyDocURL(rules.TallyRulePrefix + "<rule_slug>"),
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

Assume a new tally rule has neighbors until proven otherwise. In practice, this means checking not only semantic peers, but also formatting and
structural rules that may touch the same instruction after normalization. Pay particular attention to:

- whitespace cleanup (`no-multi-spaces`, `no-trailing-spaces`)
- indentation/continuation formatting (`consistent-indentation`)
- line splitting / blank-line normalization (`newline-per-chained-call`, `newline-between-instructions`, `no-multiple-empty-lines`)
- heredoc rewrites / structural transforms (`prefer-*-heredoc`)
- content rewrites on the same `RUN`/`COPY`/`ADD` (`sort-packages`, mount/flag insertion rules, chmod/ADD transforms)

Use these mechanisms:

1. **Detection-time gating**:
   - Use `input.IsRuleEnabled("<other_rule_code>")` to avoid dual suggestions on the same construct.
2. **Fix-time precedence**:
   - Use `RuleMetadata.FixPriority` to enforce deterministic ordering.
   - Lower priority runs first (content edits), higher priority runs later (structural transforms).
3. **Scope partitioning**:
   - Narrow one rule to patterns it owns (for example, pure file-creation vs general chained RUN transformation).

Add regression tests for overlap behavior in both involved rule test files when practical. If you truly find no meaningful overlap, record the
evidence in the plan and still sanity-check the usual formatting/whitespace neighbors before closing the question.

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

- `internal/facts` for reusable cached file/stage/run facts shared by rules
- `internal/semantic` for stage/shell/variable context
- `internal/shell` for shell command parsing and command-shape detection
- `internal/sourcemap` for stable location/snippet handling
- `internal/runmount` when mount-aware behavior matters

Do not use brittle string splitting/regex if semantic/shell helpers can model the behavior.

Facts guidance:

- Start by checking whether `input.Facts.(*facts.FileFacts)` already exposes the
  data you need.
- If not, ask whether the missing signal is:
  - derived from Dockerfile-local state across commands or stages
  - useful to more than one rule
  - expensive enough that repeated per-rule recomputation is wasteful
- If yes, add it to `internal/facts/` and test it there before wiring the new
  rule to consume it.
- Keep remote lookups, optional slow enrichment, or highly rule-specific
  decisions out of the initial facts addition unless the rule contract clearly
  needs them.

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

## Step 4.7: Design Narrow, Non-Overlapping Fix Edits

The fixer applies fixes from multiple rules to the same file. A `SuggestedFix` is **atomic**: if ANY of its
`TextEdit` ranges overlap with an already-reserved edit from a higher-priority rule, the ENTIRE fix is skipped.
Conflict detection uses **original positions** (before any edits are applied). Design every edit to be the
narrowest range that achieves the goal.

### Core principles

1. **Delete only excess characters, never replace-and-reinsert.**
   Bad: replace a run of N spaces with `" "` (touches the entire run).
   Good: keep the first space, delete only the N−1 extras → `NewText: ""`, range covers only the surplus.
   This keeps the edit range small so it doesn't collide with adjacent edits from other rules.

2. **Prefer zero-width insertions over range replacements.**
   A zero-width edit (`Start == End`) inserts text at a point without consuming source characters.
   By the overlap formula (`aEnd.Column <= bStart.Column` → no overlap), a zero-width insertion at column C
   never conflicts with a deletion or replacement that **ends** at C or **starts** at C.
   Use this for structural transforms that add content (continuation `\`, newlines, indentation).

3. **Split wide replacements into deletion + insertion pairs.**
   Bad: replace `" && "` (space-operator-space) with `" \\\n\t&& "` — one wide edit that conflicts with any
   space-cleanup edit in the same region.
   Good: emit two edits —
   - Deletion: remove exactly 1 space before the operator (narrow range, adjacent to but not overlapping
     with space-cleanup edits from other rules).
   - Insertion: zero-width at the operator position, inserting `\\\n\t` (the continuation line break).
   The operator itself (`&&`) stays in place and is never part of any edit range.

4. **Never include existing source tokens in `NewText` when they can stay in place.**
   If `&&` is already in the document, don't replace it — insert/delete around it. This keeps the edit
   zero-width at the token boundary and avoids claiming the token's column range.

5. **One violation per line for line-scoped rules.**
   The post-processing pipeline deduplicates violations by `(file, line, rule)`. If a rule can produce
   multiple findings on the same line, group them into a **single `Violation`** whose `SuggestedFix` contains
   **multiple `TextEdit` entries** (one per finding). Each edit is still narrow and independent.

### Verifying non-overlap

When a new rule's fixes may touch the same instruction as an existing rule, dump all edit ranges to confirm
no pair overlaps:

```bash
tally lint --format json --ignore '*' \
  --select 'tally/new-rule' --select 'tally/other-rule' \
  Dockerfile | python3 -c "
import json, sys
for f in json.load(sys.stdin)['files']:
  for v in f['violations']:
    fix = v.get('suggestedFix')
    if fix:
      for i, e in enumerate(fix['edits']):
        el = e['location']
        print(f'{v[\"rule\"]:40s} edit[{i}]: L{el[\"start\"][\"line\"]}:{el[\"start\"][\"column\"]:>3d}'
              f'-L{el[\"end\"][\"line\"]}:{el[\"end\"][\"column\"]:>3d}  -> {e[\"newText\"]!r}')
"
```

Two edits on the same line overlap iff neither is completely before the other:
`NOT (aEnd ≤ bStart OR bEnd ≤ aStart)`. Adjacency (aEnd == bStart) is **not** overlap.

### Integration test for cross-rule fixes

Always add a `TestFix*` scenario that enables **all interacting rules simultaneously** and snapshots the
result. This catches regressions in edit width, priority ordering, and conflict resolution. See
`TestFixCrossRuleMultiSpacesIndentationChain` for a 3-rule example (`no-multi-spaces` + `consistent-indentation`
and `newline-per-chained-call`).

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

3. If the rule has fixes, add/extend `TestFix` case(s) as a **requirement**, not an option.
   - Do not "fix" failing integration behavior by dropping fix coverage. A fix-capable rule must have fix integration coverage.
   - The primary fixture must not be a trivial green path. Make it intentionally mixed and adversarial.
   - Include multiple cases for the same rule in the fixture:
     - at least one violation that should auto-fix
     - at least one violation or suspicious pattern that should **not** auto-fix (unsupported, unsafe, intentionally skipped, or owned by another
       rule)
     - at least one nearby clean example when practical, to prove the rule does not overreach
   - Include the highest-risk overlapping rules identified in Step 0 in the **same** fix run. Do not isolate the new rule just to simplify the
     snapshot.
   - If an overlap is expected to suppress or skip one fix, assert that outcome intentionally in the combined snapshot/test comments.
   - Prefer a single realistic fixture that exercises both the new rule and its neighbors on the same instruction type or stage, rather than separate
     toy snippets.
   - If the rule is experimental or overlap rules need config, enable them in the fixture config so the combined `--fix` run matches the planning
     assumptions.

4. Use existing cross-rule fix scenarios as the model for test shape and assertions:
   - `internal/integration/fix_scenarios_test.go`
   - `internal/integration/fix_cases_test.go`

5. Update snapshots:

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

1. `_docs/rules/tally/<rule_slug>.mdx` (new rule page)
2. `_docs/rules/tally/` (tally rules directory)
3. `RULES.md`
   - tally summary table row
   - dedicated section for `tally/<rule_slug>`
   - namespace counts if changed
4. `README.md` supported rules count/table when totals change
5. `_docs/index.mdx` tally rule count if shown

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

- [ ] Planning included a conflict inventory covering direct and indirect overlaps
- [ ] Expected overlapping rules were named explicitly; "none" is justified with evidence
- [ ] Similar logic in existing rules was investigated for reuse/extraction before new code was written
- [ ] Rule implemented in `internal/rules/tally/`
- [ ] `init()` registration added
- [ ] Auto-fix implemented (sync preferred; at minimum cover the common case)
- [ ] Config schema/default/validation implemented (if configurable)
- [ ] Unit tests added for rule behavior and config
- [ ] Heredoc and `\`-continuation edge cases tested (detection + fix)
- [ ] Helper tests added for extracted utilities
- [ ] Shared logic was extracted instead of copy-pasted when a near-match existed in another rule
- [ ] New rule/helper files are covered at >=85%
- [ ] Integration fixture + `TestCheck` case added
- [ ] `TestFix` coverage added for fix-capable rules; integration fixes were not omitted to avoid conflicts
- [ ] Fix fixture is intentionally mixed: fix, no-fix, and clean/nearby cases for the same rule when practical
- [ ] Combined-rule fix test enables the overlapping rules identified during planning
- [ ] Combined snapshot/test documents expected precedence, suppression, or skipped-fix behavior
- [ ] Fixtures/docs examples are based on realistic Dockerfile patterns
- [ ] Snapshots updated
- [ ] Docs page + docs indexes updated
- [ ] `RULES.md` and `README.md` counts/details updated
- [ ] `go test ./...`, `make lint`, and `make cpd` pass
