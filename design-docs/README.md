# Tally Architecture Documentation

This folder contains comprehensive architectural research and design documentation for the tally Dockerfile linter.

## Research Documents

### 1. [Linter Pipeline Architecture](01-linter-pipeline-architecture.md)

**Covers:** Processing pipeline structure, concurrency models, rule evaluation strategies

**Key Topics:**

- Standard pipeline stages (discovery → parsing → analysis → filtering → reporting)
- File-level vs rule-level parallelism
- Rule dispatch optimization
- Processing pipeline for violation filtering

**Based on:** ruff, oxlint, golangci-lint

---

### 2. [Docker Buildx Bake --check Analysis](02-buildx-bake-check-analysis.md)

**Covers:** Official Docker linting implementation, complete rule list, architecture patterns

**Key Topics:**

- All 22 BuildKit linting rules + 1 experimental
- Request flow and subrequest protocol
- Rule definition patterns
- Output formatting (text and JSON)
- Configuration system

**Based on:** docker/buildx, moby/buildkit

---

### 3. [Parsing and AST](03-parsing-and-ast.md)

**Covers:** Parser selection, AST vs CST, semantic analysis

**Key Topics:**

- AST vs CST trade-offs for linting
- Evaluation of moby/buildkit parser (recommended)
- Tree-sitter as optional enhancement
- Semantic model design
- When to use which approach

**Based on:** moby/buildkit/parser, tree-sitter, oxc semantic analysis

---

### 4. [Inline Disables and Configuration](04-inline-disables.md)

**Covers:** Inline suppression directives, configuration precedence

**Key Topics:**

- Common inline disable patterns
- Implementation approaches (comment parsing, interval trees, post-filtering)
- Recommended syntax for tally
- Unused directive detection
- Configuration integration

**Based on:** ruff, oxlint, golangci-lint, hadolint, buildx

---

### 5. [Reporters and Output Formatting](05-reporters-and-output.md)

**Covers:** Reporter API design, output formats, libraries

**Key Topics:**

- Core reporter interface pattern
- Standard formats (text, JSON, SARIF, GitHub Actions)
- Multiple simultaneous outputs
- Recommended Go libraries (Lip Gloss, go-sarif)
- Advanced features (grouping, filtering, summaries)

**Based on:** golangci-lint, ruff, oxlint, staticcheck

---

### 6. [Code Organization for Scalability](06-code-organization.md)

**Covers:** Project structure, rule organization, testing strategy

**Key Topics:**

- Recommended directory structure
- One file per rule pattern
- Rule registry and auto-registration
- Category-based organization
- Per-rule testing approach
- Documentation generation

**Based on:** ruff, oxlint, golangci-lint, hadolint

---

### 7. [Context-Aware Linting Foundation](07-context-aware-foundation.md)

**Covers:** Architecture for future context-aware features

**Key Topics:**

- What is context-aware linting?
- BuildContext interface design
- Progressive enhancement strategy (optional → default → full)
- Registry client integration
- File system context
- Caching expensive operations

**Based on:** Docker buildx, BuildKit context validation

---

### 8. [Hadolint Rules Reference](08-hadolint-rules-reference.md)

**Covers:** Complete reference of all Hadolint rules for implementation roadmap

**Key Topics:**

- 70 Hadolint rules organized by category
- Rule severity matrix
- Priority-based implementation roadmap
- Top 30 rules for v1.0
- Configuration examples
- ShellCheck integration strategy

**Based on:** hadolint (the most popular Dockerfile linter)

---

### 9. [Hadolint Research](09-hadolint-research.md)

**Covers:** Research and analysis of Hadolint’s implementation, behavior, and compatibility considerations

---

### 10. [BuildKit Phase 2 Rules: Path Forward](10-buildkit-phase2-path-forward.md)

**Covers:** Decision and roadmap for implementing BuildKit “Phase 2” lint rules in tally

**Companion research:** [BuildKit Phase 2 Rules Integration Research](buildkit-phase2-rules-research.md)

---

### 11. [VSCode Extension Architecture](11-vscode-extension-architecture.md)

**Covers:** LSP-first VSCode extension architecture, binary strategy, editor integration and test strategy

---

### 12. [JSON v2 Migration Plan](12-json-v2-migration.md)

**Covers:** Comprehensive plan to migrate from `encoding/json` to `github.com/go-json-experiment/json` (v2), including TypeScript-Go-style lint
enforcement

---

### 13. [AI AutoFix via ACP](13-ai-autofix-acp.md)

**Covers:** Opt-in AI-powered fixes using Agent Client Protocol (ACP), configuration, safety model, prompt contract, and a demo multi-stage build rule

---

### 14. [Prefer VEX Attestation (OpenVEX)](14-prefer-vex-attestation.md)

**Covers:** New tally rule to discourage embedding `*.vex.json` files into images, plus a roadmap for context-aware VEX/attestation linting

**Key Topics:**

- MVP detection of `COPY *.vex.json`
- Recommendation: attach VEX as an OCI attestation (in-toto predicate)
- Future: stage-aware checks, registry-attestation discovery, trust policy, VEX-aware vulnerability reporting

**Based on:** OpenVEX, OCI referrers/attestations, Sigstore/cosign ecosystem

---

### 15. [Async Checks (Slow Operations)](15-async-checks.md)

**Covers:** Optional slow/async checks infrastructure (network, filesystem, console I/O), budgets, CI auto-disable, and registry-backed BuildKit
parity rules

**Key Topics:**

- Async check requests + resolvers + worker pools
- Slow-checks configuration (`auto|on|off`) and CI detection
- Registry-backed checks using `containers/image/v5`
- Deterministic tests with `go-containerregistry` mock registry

---

### 16. [Integration Tests Refactor and Placement](16-integration-tests-refactor-and-placement.md)

**Covers:** Step-by-step split of `internal/integration/integration_test.go` plus a placement decision tree for future tests

**Key Topics:**

- File-by-file refactor plan with zero-behavior-change constraints
- Shared harness design for check/fix case runners
- Canonical placement buckets for future `tally/*` rules
- Decision tree for check vs fix vs context vs async vs cross-rule tests

---

## Quick Start Guides

### For Immediate Implementation

1. **Start with pipeline:** Read [01-linter-pipeline-architecture.md](01-linter-pipeline-architecture.md) → File-level parallelism pattern
2. **Parser decision:** Read [03-parsing-and-ast.md](03-parsing-and-ast.md) → Stick with moby/buildkit
3. **Rule implementation:** Read [06-code-organization.md](06-code-organization.md) → One file per rule pattern
4. **First rules:** Read [08-hadolint-rules-reference.md](08-hadolint-rules-reference.md) → Top 30 priority list

### For Specific Features

- **Inline `# tally ignore=`** → [04-inline-disables.md](04-inline-disables.md)
- **JSON/SARIF output** → [05-reporters-and-output.md](05-reporters-and-output.md)
- **Context-aware rules** → [07-context-aware-foundation.md](07-context-aware-foundation.md)
- **Docker buildx compatibility** → [02-buildx-bake-check-analysis.md](02-buildx-bake-check-analysis.md)
- **Integration test placement** → [16-integration-tests-refactor-and-placement.md](16-integration-tests-refactor-and-placement.md)

---

## Implementation Roadmap

**See prioritized plan below for implementation sequencing.**

### Top 10 Priorities (v1.0)

1. **Restructure Rule System** - Establish scalable architecture
2. **Build Semantic Model** - Enable context-aware rules
3. **Inline Disable Support** - `# tally ignore=` directives
4. **Reporter Infrastructure** - Text, JSON, SARIF outputs
5. **File-Level Parallelism** - Efficient multi-file linting
6. **Top 5 Critical Rules** - DL3006, DL3004, DL3020, DL3024, DL3002
7. **Violation Processing Pipeline** - Filter, deduplicate, sort
8. **File Discovery** - Recursive search, glob patterns
9. **SARIF Reporter** - CI/CD integration
10. **Enhanced Integration Tests** - Comprehensive end-to-end coverage

### Future Phases

- **v1.1-1.5**: Additional rules from [08-hadolint-rules-reference.md](08-hadolint-rules-reference.md)
- **v2.0+**: Context-aware features from [07-context-aware-foundation.md](07-context-aware-foundation.md)

---

## Key Architectural Decisions

Based on comprehensive research of ruff, oxlint, golangci-lint, buildx, and hadolint:

### ✅ Confirmed Decisions

1. **Parser:** Continue using `moby/buildkit/frontend/dockerfile/parser`
   - Sufficient for semantic linting
   - Official Docker parser
   - Tree-sitter is overkill for v1.0

2. **Concurrency:** File-level parallelism with worker pool
   - Simple, effective for Dockerfiles
   - No coordination complexity
   - Can optimize later if needed

3. **Rule Organization:** One file per rule + category folders
   - Easy navigation and maintenance
   - Clear ownership
   - Automatic registration via init()

4. **Inline Disables:** Post-filtering approach
   - Simplest to implement
   - Supports `# tally ignore=RULE` syntax
   - Compatible with buildx `# check=` syntax

5. **Reporters:** Factory pattern with format selection
   - Text (colored with Lip Gloss)
   - JSON (for tooling)
   - SARIF (for CI/CD)
   - Multiple simultaneous outputs

6. **Context:** Optional in v1.0, default in v2.0
   - Rules work with or without context
   - Progressive enhancement
   - BuildContext interface ready for future

### 🔄 Deferred Decisions

1. **Tree-sitter integration** - Only if style rules become important
2. **ShellCheck integration** - Complex, defer to v2.0+
3. **Registry validation** - Nice to have, not critical
4. **Auto-fix capabilities** - v2.0+ feature
5. **LSP server** - Future editor integration

---

## Research Methodology

All research based on analyzing source code from:

- **ruff** (astral-sh/ruff) - Python linter in Rust
- **oxlint** (oxc-project/oxc) - JavaScript/TypeScript linter in Rust
- **golangci-lint** (golangci/golangci-lint) - Go linter orchestrator
- **hadolint** (hadolint/hadolint) - Dockerfile linter in Haskell (industry standard)
- **buildx** (docker/buildx) - Official Docker CLI plugin
- **buildkit** (moby/buildkit) - Docker build backend

Research conducted through:

- GitHub code search and exploration
- Direct source code analysis
- Documentation review
- Pattern extraction and comparison

---

## Contributing to Documentation

When adding new architectural decisions:

1. Create a new numbered document (e.g., `09-new-topic.md`)
2. Follow the existing structure:
   - Research focus statement
   - Executive summary
   - Detailed sections with code examples
   - Key takeaways
   - References
3. Update this README with a summary
4. Update [CLAUDE.md](../CLAUDE.md) if it affects development workflow

---

## Related Files

- **[../CLAUDE.md](../CLAUDE.md)** - Project guidance for Claude (build commands, conventions)
- **[../README.md](../README.md)** - User-facing project documentation
- **[../internal/](../internal/)** - Implementation code
- **[../internal/integration/](../internal/integration/)** - Integration tests

---

## Research Date

This research was conducted in January 2025 based on the latest versions of:

- ruff v0.8.x
- oxlint v0.13.x
- golangci-lint v1.63.x
- hadolint v2.12.x
- buildx v0.20.x
- buildkit v0.19.x

Architectural patterns are expected to remain stable, but specific APIs and features may evolve.
