# Implementation Roadmap

Prioritized action plan based on architectural research (`docs/01`–`docs/09`).

This roadmap focuses on building a scalable linter core (parsing, semantics, rules, processors, reporting) while:

- baking in key lessons from the research docs (so we don’t refactor later), and
- explicitly front-loading the few “decision spikes” that can otherwise block multiple tracks.

---

## Critical Insight: Dockerfiles Are Small (But Repos Aren’t)

The research notes Dockerfiles are typically **< 200 lines** (often 50–100). This shapes feasibility:

- ✅ Deep per-file semantic analysis is feasible (and can be a differentiator)
- ✅ Multi-pass parsing is unnecessary for v1.0
- ⚠️ Multi-file runs (many Dockerfiles) and build-context scanning can dominate runtime, so discovery + concurrency still matter

---

## Non-Negotiable Learnings to Bake Into the Architecture

From `docs/01`–`docs/09`, these are the “don’t miss” architecture lessons:

1. **Pipeline shape is stable:** discovery → parse (+source map) → semantic/context build → rules → processors (filter/dedup/sort) → reporting. (
   [01](01-linter-pipeline-architecture.md))
2. **Rich diagnostics are a feature, not polish:** plan a stable `Violation` schema with range (line+col), snippets, doc links, and (later) fixes. (
   [02](02-buildx-bake-check-analysis.md), [05](05-reporters-and-output.md))
3. **Progressive adoption matters:** experimental/opt-in rules + configurable failure threshold (`--fail-level`). ([02](02-buildx-bake-check-analysis.md), [09](09-hadolint-research.md))
4. **Context must be optional:** rule API should accept an optional `BuildContext` and still work without it (v1.0 shouldn't require full context). (
   [07](07-context-aware-foundation.md))
5. **Parse failures stop the pipeline:** if parsing fails, report the parse error with location info and exit—do not run any rules. Rules can assume
   AST is always valid (following ESLint's approach).
6. **Avoid future refactors:** keep parser concerns separate from rules; preserve enough trivia (comments, line/col, original text) for directives +
   snippets. ([01](01-linter-pipeline-architecture.md), [03](03-parsing-and-ast.md), [04](04-inline-disables.md), [05](05-reporters-and-output.md))

---

## Code Reuse Opportunities (Prefer Reuse Over Reinvent)

Before building new subsystems, evaluate these upstream building blocks:

| Library | Use For | Notes |
|---------|---------|-------|
| `moby/buildkit/frontend/dockerfile/parser` | Dockerfile parsing | Already used (`internal/dockerfile`). Good enough for semantic linting. ([03](03-parsing-and-ast.md)) |
| `moby/buildkit/frontend/dockerfile/linter` | Existing rule implementations | ✅ **Now integrated.** Parser captures 20 Phase-1 warnings via `instructions.Parse()`. 2 Phase-2 rules (context-aware) need separate implementation. ([02](02-buildx-bake-check-analysis.md)) |
| `moby/buildkit/frontend/dockerfile/dockerignore` | `.dockerignore` parsing | Parse `.dockerignore` files into pattern list. ([07](07-context-aware-foundation.md)) |
| `github.com/moby/patternmatcher` | Pattern matching | Docker-compatible glob matching (**, negation). Used by BuildKit for `CopyIgnoredFile` rule. |
| `moby/buildkit/frontend/dockerfile/shell` (and related) | Shell lexing helpers | Useful foundation for future ShellCheck integration and RUN parsing (don’t re-invent tokenization). ([09](09-hadolint-research.md)) |
| `mvdan.cc/sh/v3/syntax` | Shell parsing (AST) | Robust parser/formatter for sh/bash/mksh; good base for deep `RUN` heredoc analysis and future shell rules. |
| `github.com/wasilibs/go-shellcheck` | ShellCheck-as-library (WASM) | Optional: run upstream ShellCheck without shipping an external binary (heavier, but high coverage). |
| `github.com/owenrumney/go-sarif/v2` | SARIF output | Standard CI/security tooling integration. ([05](05-reporters-and-output.md)) |
| `github.com/charmbracelet/lipgloss` | Terminal output | Nice human output with minimal complexity. ([05](05-reporters-and-output.md)) |
| `github.com/bmatcuk/doublestar/v4` | Glob patterns | For discovery (CLI input + directory scanning). ([01](01-linter-pipeline-architecture.md)) |
| `containers/*` libraries | Registry & image metadata (future) | If we implement “trusted registries”, digest pinning, manifest/platform checks, consider using the existing containers ecosystem rather than hand-rolled HTTP clients. (Aligns with context-aware direction in [07](07-context-aware-foundation.md)) |

---

## Decision Spikes (Do These Before Priority 1)

These are explicitly front-loaded to avoid cross-blocking later.

1. **Spike A: Reuse BuildKit linter vs. reimplement** ✅ DECIDED
   - **Decision: Hybrid approach**
   - BuildKit has **two-phase linting**:
     - **Phase 1:** `instructions.Parse(ast, linter)` — triggers 20 syntax/semantic rules (no context needed)
     - **Phase 2:** `dockerfile2llb.Dockerfile2LLB()` — triggers 2 context-aware rules (`CopyIgnoredFile`, `InvalidBaseImagePlatform`)
   - **Implementation:**
     - ✅ Parser now captures Phase 1 warnings via `linter.New(&linter.Config{Warn: warnFunc})`
     - ✅ Parser provides typed `Stages` and `MetaArgs` from `instructions.Parse()`
     - For Phase 2 rules: implement our own using `moby/patternmatcher` (lighter than full LLB conversion)
     - Custom rules for hadolint parity built on top of typed instructions

2. **Spike B: Freeze `Violation` + output contract** ✅ DECIDED
   - **Implemented in `internal/rules/violation.go`:**
     - `Location` with `File`, `Start`, `End` (Position with Line/Column)
     - `RuleCode`, `Message`, `Detail`, `Severity`, `DocURL`
     - `SourceCode` (optional snippet)
     - `SuggestedFix` with structured `TextEdit` array
   - JSON marshaling tested and stable

3. **Spike C: Source-map strategy** ✅ DECIDED
   - **Implementation:**
     - Parser preserves raw `Source []byte` for snippet extraction
     - `ParseResult.AST` provides BuildKit node locations (line/column ranges)
     - `LintInput` provides both `Source` and `Lines` for rules
   - Sufficient for rich diagnostics and inline directives

4. **Spike D: Shell commands + heredocs strategy (standardize early)**
   - Goal: avoid per-rule ad-hoc regex/tokenization decisions; ensure consistent handling for `RUN`, `RUN <<EOF` and Dockerfile heredocs in `COPY`/
     `ADD`.
   - Decision (recommended):
     - **Tier 1 (default):** use BuildKit’s lightweight shell lexer (`moby/buildkit/frontend/dockerfile/shell`) for word-splitting/tokenization when a
       rule needs to recognize patterns inside `RUN` commands.
     - **Tier 2 (deep/structured):** use `mvdan.cc/sh/v3/syntax` for rules that require real shell structure (heredocs, pipelines, conditionals) and
       for future “ShellCheck-like” linting without an external binary.
     - Keep all shell parsing behind an internal facade (`internal/shell/…`) so rule authors never pick libraries directly.
   - Heredocs handling (must be first-class in our parse model):
     - **Dockerfile heredocs in `COPY`/`ADD`:** treat as **inline sources** (virtual files) that are not subject to `.dockerignore` and do not require
       build-context presence.
     - **`RUN` heredocs:** treat heredoc bodies as **virtual scripts** for shell analysis (tokenization/AST), but do not attempt to infer filesystem
       side effects (“files created by the script”) in v1.x.
   - Cross-blocking: context-aware linting must know whether a file input came from build context vs. `COPY/ADD <<EOF` heredoc, otherwise rules like
     “copy ignored file” or “missing file” will be wrong. ([07](07-context-aware-foundation.md))

5. **Optional Spike E: Style/parser deep dive**
   - If we want “format/style enforcement” or LSP later, validate the “BuildKit now, tree-sitter later” plan. ([03](03-parsing-and-ast.md))

---

## Priority 1: Core Rule System + Stable Data Model ✅

**Goal:** Establish the stable interfaces that everything else builds on (minimize future refactors).

**Status:** Completed

**Actions:**

1. Expand `internal/testutil/` (already exists but empty) with helpers:
   - Parse Dockerfile from string/file and return AST + raw source (for directives/snippets)
   - Lint helpers for table-driven rule tests

2. Create `internal/rules/` structure (or migrate from `internal/lint/` in-place):

   ```text
   internal/rules/
   ├── registry.go          # Rule registration
   ├── rule.go              # Rule metadata + execution contract
   ├── severity.go          # Severity enum
   ├── location.go          # Range/Location types (line+col)
   └── violation.go         # Stable Violation schema
   ```

3. Define a **future-proof `Violation`** schema (don't under-specify):
   - `File`, `StartLine`, `StartColumn`, `EndLine`, `EndColumn`
   - `RuleCode`, `Message`, `Detail` (optional), `Severity`, `DocURL`
   - `SourceCode` (optional) and `SuggestedFix` (optional structured edit hint; supports "auto-fix suggestion" without auto-applying)

4. Add **progressive adoption hooks** to `Rule` metadata:
   - `EnabledByDefault`
   - `IsExperimental`

5. Make the rule execution signature **context-ready**:
   - `ctx *context.BuildContext` can be `nil` (v1.0 works without it). ([07](07-context-aware-foundation.md))

**Success Criteria:**

- [x] Rules can be registered and enumerated
- [x] `Violation` schema is stable and used everywhere
- [x] `Violation.SuggestedFix` supports structured edit hints (even if we don't auto-apply fixes yet)
- [x] Unit tests can lint a Dockerfile string without CLI wiring
- [x] Rule interface accepts optional context without forcing it

---

## Priority 2: Parser Facade + Source Map (Line/Column, Snippets, Comments) ✅

**Goal:** Provide the "compiler frontend" primitives required by multiple tracks (inline disables, reporters, SARIF).

**Status:** Completed

**Actions:**

1. ✅ Extend `internal/dockerfile` parse output to include raw source:
   - `ParseResult.Source` preserves full file contents
   - `LintInput.SourceMap()` provides line-based access
   - `LintInput.Snippet()` and `SnippetForLocation()` for easy extraction

2. ✅ Establish a `SourceMap` abstraction (`internal/sourcemap/`):
   - `SourceMap.Line(n)` - get single line
   - `SourceMap.Snippet(start, end)` - extract line range
   - `SourceMap.LineOffset(n)` - byte offset for column calculations
   - `SourceMap.Comments()` - extract all comments with line numbers
   - `SourceMap.CommentsForLine(n)` - get comments preceding a line

3. ✅ Capture comment trivia needed for:
   - `Comment` struct with `Line`, `Text`, and `IsDirective` fields
   - Directive detection for: tally, hadolint, check=, syntax=, escape=
   - `CommentsForLine()` matches BuildKit's PrevComment behavior

4. ✅ Make heredocs first-class in the parse model (`internal/dockerfile/heredoc.go`):
   - `HeredocInfo` struct wraps BuildKit's `parser.Heredoc` with classification
   - `HeredocKindScript` for RUN heredocs
   - `HeredocKindInlineSource` for COPY/ADD heredocs
   - `ExtractHeredocs()` and `HasHeredocs()` functions
   - Note: Requires `# syntax=docker/dockerfile:1` directive for heredoc parsing

**Success Criteria:**

- [x] Every violation can report line+column (via `Location` type with 0-based LSP semantics)
- [x] Reporter can show snippets without re-parsing the file (`LintInput.SnippetForLocation()`)
- [x] Inline directives can be parsed from source consistently (`SourceMap.Comments()` with `IsDirective` flag)

---

## Priority 3: Semantic Model v1 (Keep It Extensible)

**Goal:** Enable rules that need cross-instruction context without baking rule-specific logic everywhere.

**Actions:**

1. Build `SemanticModel` in a dedicated pass (or builder) and keep it parser-agnostic. ([03](03-parsing-and-ast.md))
2. Track the v1 essentials:
   - Stages + stage order
   - Duplicate stage names (DL3024: detected during construction, not as a separate rule)
   - ARG/ENV scoping (global vs stage-local) with CLI `--build-arg` overrides (highest precedence)
   - `COPY --from` references
   - Base image refs (`FROM`, `--platform`)
   - `SHELL` per stage (dialect/argv) for correct `RUN` parsing defaults
3. Add the “easy extensions” now (cheap, unlocks many rules later):
   - Variable reference tracking (enables unused/shadowed var rules)
   - Comment associations (enables directive/doc checks)
   - Stage graph hooks (enables “unreachable stage” style rules)

**Success Criteria:**

- [ ] Semantic model supports stage + var resolution
- [ ] CLI `--build-arg` overrides work with correct precedence (BuildArgs > Stage ARG/ENV > Global ARG)
- [ ] DL3024 (duplicate stage names) detected during semantic model construction
- [ ] Construction-time violations supported (when they're more natural than separate rules)
- [ ] Unit tests cover semantic resolution edge cases

---

## Priority 4: Inline Disable Directives (Post-Filter Approach)

**Goal:** Allow users to suppress violations with migration compatibility.

**Key takeaways to preserve:**

- Use post-filtering (simplest) ([04](04-inline-disables.md))
- Support compatibility syntax (hadolint / buildx)
- Validate codes + detect unused directives
- Define precedence explicitly: **inline > CLI > config > defaults** ([04](04-inline-disables.md))

**Success Criteria:**

- [ ] `# tally ignore=...` and `# tally global ignore=...`
- [ ] `# hadolint ignore=...` and `# hadolint global ignore=...` (migration)
- [ ] `# check=skip=...` (buildx compat)
- [ ] Unknown codes warn; unused directives detectable

---

## Priority 5: Reporter Infrastructure (Multi-Output, CI Integrations, Exit Codes)

**Goal:** Make results useful everywhere: terminal, CI annotations, and machine output.

**Status:** Partially implemented (text reporter)

**Actions:**

1. Implement reporters against the stable `Violation` schema:
   - ✅ text (BuildKit-style with source snippets) - `internal/reporter/buildkit.go`
   - json
   - github-actions annotations
   - sarif (go-sarif)
   - ensure reporters preserve and emit `SuggestedFix` data when present (at least JSON + SARIF)

   **Note:** Text reporter adapted from BuildKit's `errdefs.Source.Print()` and `lint.Warning.PrintTo()`
   without importing heavy dependencies (containerd, grpc). Produces output consistent with
   `docker buildx build --check`.

2. Implement **multi-reporter output** (console + file) from the start:
   - matches research (`[[output]]` pattern) and avoids later config churn ([05](05-reporters-and-output.md))

3. Add CLI flags for output ergonomics:
   - `--format` (or `--output` blocks later)
   - `--output=stdout|stderr|path`
   - `--no-color`
   - `--show-source/--hide-source`

4. Define + test exit codes:
   - `0` clean (or below threshold)
   - `1` violations at/above threshold
   - `2` parse/config error

5. Add `--fail-level` (error|warning|info|style|none) (aka “error mode”). ([02](02-buildx-bake-check-analysis.md), [09](09-hadolint-research.md))

**Success Criteria:**

- [ ] At least text + JSON are stable and snapshot-tested
- [ ] GitHub Actions + SARIF available for CI
- [ ] Multi-output works (stdout + file)
- [ ] Exit codes match documented behavior

---

## Priority 6: File Discovery + Config Cascade + Optional Build Context

**Goal:** Enable “lint the repo” workflows without sacrificing per-file correctness.

**Actions:**

1. Discovery:
   - inputs: files, dirs, globs, multiple paths
   - defaults: `Dockerfile`, `Dockerfile.*`, `*.Dockerfile`
   - exclude patterns via `--exclude`
   - optional `.gitignore` support (nice-to-have, not blocker)

2. Config cascade per target file:
   - discovery must return **(file path, config root)** pairs to avoid “wrong config applied to file” bugs

3. Build context (optional for v1.0, but API-ready):
   - `.dockerignore` parsing (BuildKit matcher)
   - context dir scanning (only when needed)
   - treat `COPY/ADD` heredoc sources as **virtual inline files** (not affected by `.dockerignore`, not required to exist in context)
   - caching hooks for expensive operations (registry, FS scans) ([07](07-context-aware-foundation.md))

**Success Criteria:**

- [ ] `tally check .` behaves predictably in repos
- [ ] Each Dockerfile uses the nearest `.tally.toml` when present
- [ ] Context can be provided but is not required

---

## Priority 7: Violation Processing Pipeline + Rule Configuration

**Goal:** Centralize “policy” logic: filtering, severity overrides, sorting, snippets.

**Actions:**

1. Implement a processor chain (golangci-lint style). ([01](01-linter-pipeline-architecture.md))
2. Include at least:
   - Path normalization
   - Inline disable filter
   - Config exclusion filter (per-file)
   - Severity overrides + enable/disable (per rule)
   - Deduplication
   - Sorting (stable output for snapshots)
   - SourceCode/snippet attachment (enables rich diagnostics) ([02](02-buildx-bake-check-analysis.md), [05](05-reporters-and-output.md))

**Success Criteria:**

- [ ] Output is stable across runs (sorting)
- [ ] Severity overrides and enable/disable work
- [ ] Snippet attachment works without reporter-specific hacks

---

## Priority 8: Implement an Initial Rule Baseline (Reuse-First)

**Goal:** Deliver immediate value with minimal reinvention.

**Approach (pick based on Spike A decision):**

1. **Reuse path:** wrap BuildKit’s 22 rules as a baseline provider, then add hadolint-parity rules on top. ([02](02-buildx-bake-check-analysis.md))
2. **Hybrid/Rewrite path:** implement the “top rules” list from hadolint research. ([08](08-hadolint-rules-reference.md), [09](09-hadolint-research.md))

**Rule priority sanity check (from research):**

- Top of list includes: `DL3006`, `DL3000`, `DL3002`, `DL3004`, `DL3008`, `DL3020`, `DL3025`, `DL4006`… ([08](08-hadolint-rules-reference.md))

**Success Criteria:**

- [ ] At least 5–10 high-value rules shipped (not just one demo rule)
- [ ] Experimental rules supported as opt-in for “controversial” checks
- [ ] Every rule has a doc URL and actionable message

---

## Priority 9: Rule CLI + Documentation Generation

**Goal:** Make rule discovery and documentation frictionless.

**Actions:**

1. `tally rules list/show` (already planned)
2. Add a docs generator hook:
   - Generate Markdown from rule registry (name/description/examples placeholders)
   - Enables a future docs site without hand-written drift ([06](06-code-organization.md), [02](02-buildx-bake-check-analysis.md))

**Success Criteria:**

- [ ] `tally rules list` filters by category/severity/experimental
- [ ] `tally rules show DL3006` prints stable, complete info
- [ ] Docs generation produces deterministic output

---

## Priority 10: Integration Tests (Snapshots + Fixtures)

**Goal:** Lock behavior end-to-end and allow intentional output evolution.

**Actions:**

1. Expand `internal/integration/testdata/`:
   - clean
   - critical rules
   - multi-stage
   - inline disables (tally/hadolint/buildx)
   - config cascade (closest `.tally.toml`)
   - reporter formats (text/json/sarif/github-actions)
   - exit codes (clean → 0, violations → 1, parse error → 2)

2. Snapshot:
   - stable ordering
   - color disabled for text snapshots
   - exit code assertions

**Success Criteria:**

- [ ] Fixtures cover the "core pipeline" interactions (config + directives + reporting)
- [ ] Exit codes tested: 0 (clean), 1 (violations at/above threshold), 2 (parse/config error)
- [ ] `UPDATE_SNAPS=true go test ./internal/integration/...` updates snapshots intentionally

---

## Cross-Blocking / Dependency Map (Updated)

```text
Decision Spikes (A/B/C)
    ↓
Priority 1 (Rule system + stable Violation schema)
    ↓
Priority 2 (Parser facade + SourceMap)
    ↓
Priority 3 (Semantic model)
    ↓
Priority 4 (Inline directives) ─┐
    ↓                          │
Priority 7 (Processors) ───────┤
    ↓                          │
Priority 5 (Reporters) ────────┘
    ↓
Priority 6 (Discovery + config cascade + optional context)
    ↓
Priority 8 (Initial rule baseline)
    ↓
Priority 9 (Rule CLI + docs generation)
    ↓
Priority 10 (Integration tests)
```

**Key blockers:**

- If `Violation` + `Location` aren’t stable early, reporters + SARIF + snapshots churn (fix via Spike B + Priority 1).
- If we don’t have SourceMap, inline ignores and rich diagnostics both become hacks (fix via Spike C + Priority 2).
- If rule API doesn’t accept optional context, adding `.dockerignore`/registry features later becomes signature churn (fix via Priority 1 +
  [07](07-context-aware-foundation.md)).

---

## Feasibility & Deeper Dives (Post v1.0)

Dockerfiles being small means we can afford “more semantic” features later without performance fear:

- **Tree-sitter (optional):** style/format rules and LSP-friendly parsing. ([03](03-parsing-and-ast.md))
- **ShellCheck integration:** track `SHELL` per stage + vars for RUN validation. ([09](09-hadolint-research.md))
- **Context-aware rules:** file existence, `.dockerignore` correctness, platform/manifest checks. ([07](07-context-aware-foundation.md))
- **Registry-backed checks:** trusted registries, digest pinning, platform validation (consider `containers/*` reuse).
- **Suggested fixes / auto-fix:** start with structured “fix hints”, then actual edits later. ([05](05-reporters-and-output.md))
- **Label schema validation:** hadolint-style label typing and “strict labels” mode. ([09](09-hadolint-research.md))
