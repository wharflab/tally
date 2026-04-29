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

### 17. [IntelliJ Plugin (Lean build, no Gradle)](17-intellij-plugin-lean-build.md)

**Covers:** Plan for an IntelliJ Platform plugin under `extensions/` with a custom lean build pipeline that drives `kotlinc` directly (no Gradle).

---

### 18. [JSON-Schema-First Config and Rule System](18-json-schema-first-config-and-rule-system.md)

**Covers:** Migration to external JSON schema documents, schema-derived Go types via `omissis/go-jsonschema`, runtime validation with
`google/jsonschema-go`, and removal of `invopop/jsonschema` + `santhosh-tekuri/jsonschema/v6`.

---

### 19. [AI AutoFix via ACP: Diff/Patch Output Contract](19-ai-autofix-diff-contract.md)

**Covers:** Proposal to switch AI AutoFix output from “whole Dockerfile” to a unified diff/patch contract with patch-level heuristics.

---

### 20. [BuildKit-Parseable but Non-Buildable Dockerfiles (Heuristic Checks)](20-buildkit-parseable-non-buildable-dockerfiles.md)

**Covers:** Proposed “preflight” rules for Dockerfiles that parse into a BuildKit AST but are likely to fail builds (typos, half-edits, stage-graph
cycles).

---

### 21. [VS Code: AI CodeAction AutoFix for `tally/prefer-multi-stage-build` (Copilot / built-in assistant)](21-vscode-ai-codeaction-autofix-prefer-multi-stage-build.md)

**Covers:** Proposal for a VS Code Quick Fix that leverages the in-IDE assistant (Copilot via VS Code Language Model API) while keeping tally as the
source of truth for objective prompts and validation.

---

### 22. [Docker Desktop Extension: Tally as an in-product Dockerfile lint + fix marketing channel](22-docker-desktop-extension.md)

**Covers:** MVP and roadmap for a Docker Desktop Extension that runs `tally` on local Dockerfiles/Containerfiles, supports fix preview/apply, and
converts users into project-level adoption via `.tally.toml` + CI snippets.

---

### 23. [ShellCheck Integration — Design & Roadmap](23-shellcheck-integration.md)

**Covers:** What Hadolint’s ShellCheck integration *actually* does, what ShellCheck can output (JSON1 + fixes), and a concrete plan to ship
`shellcheck/SC####` rules in Tally by embedding `shellcheck.wasm` (WASI) and running it via `wazero` in a single GPLv3 binary.

---

### 24. [Lessons from TypeScript-Go VS Code Extension (TypeScript Native Preview)](24-vscode-extension-lessons-from-typescript-go-native-preview.md)

**Covers:** Actionable implementation patterns to borrow for Tally’s Go-binary-backed VS Code extension, especially around traceability, crash
recovery, diagnostic pull configuration, and status UI.

**Key Topics:**

- Separate output and protocol-trace channels for better LSP debugging
- `vscode-languageclient` watchdog behavior and how to surface restarts in the UI
- Standard `tally.trace.server` configuration instead of custom tracing plumbing
- Diagnostic pull-mode tuning and `match` handling when only a URI is available
- Concrete backlog items for `extensions/vscode-tally/`

**Based on:** `microsoft/typescript-go`’s `_extension/`, VS Code, and `vscode-languageclient`

---

### 25. [CLI-Config Integration Refactor](25-cli-config-integration-refactor.md)

**Covers:** Replacing hand-written CLI-to-config glue code with idiomatic urfave/cli v3 patterns (`Validator`, `Destination`, `Sources`)

**Key Topics:**

- Problem: ~100 lines of `cmd.IsSet`/`cmd.String` glue that grows with every flag
- Option A: `urfave/cli-altsrc` for unified TOML+env+CLI source chain
- Option B: `urfave/sflags` for struct-driven flag generation
- Option C: Koanf CLI provider adapter (minimal change, recommended first step)
- `Validator` field for enum flags (`--format`, `--slow-checks`, `--fail-level`)
- Phased migration plan

---

### 26. [Windows Container Support](26-windows-container-support.md)

**Covers:** Comprehensive plan to fix false positives, broken fixes, and missing detection for
Windows container Dockerfiles (cmd.exe, PowerShell, servercore/nanoserver base images)

**Key Topics:**

- `tally/platform-mismatch` redesign: validate `--platform` against registry, not host
- Semantic model: `BaseImageOS` detection per stage (heuristic + optional registry)
- Semantic model: effective shell per instruction (Windows default = `cmd /S /C`)
- ShellCheck gating for Windows stages (suppress WASM invocation entirely)
- BuildKit WCOW feature matrix (heredoc, `--mount`, `--chown` all unsupported)
- `prefer-multi-stage-build`: Windows build tool recognition (MSBuild, choco, nuget)
- `WorkdirRelativePath`: Windows absolute path recognition (`C:\`, `c:/`)
- Escape character audit across all rules
- 5-phase implementation plan

---

### 27. [Windows Container Rules (`tally/windows/*`)](27-windows-container-rules.md)

**Covers:** Dedicated lint rules for Windows container Dockerfiles, treating them as first-class citizens

**Key Topics:**

- `tally/windows/prefer-powershell-as-shell`: detect `RUN powershell` anti-pattern, recommend `SHELL` instruction
- `tally/windows/group-run-layers`: combine consecutive RUNs (layer sizes are ~100x larger on Windows)
- `tally/windows/cleanup-in-same-layer`: file deletion in a different RUN doesn't reduce image size
- `tally/windows/prefer-nanoserver`: suggest NanoServer runtime stage (300 MB vs 5 GB ServerCore)
- `tally/windows/progress-preference`: suppress PowerShell progress bars in builds
- `tally/windows/error-action-preference`: set `$ErrorActionPreference = 'Stop'` (Windows `set -e`)

**Depends on:** [26. Windows Container Support](26-windows-container-support.md) for `BaseImageOS` detection

---

### 28. [ShellCheck Go Reimplementation Bridge + SC1040 Pilot](28-shellcheck-go-reimplementation-bridge-sc1040.md)

**Covers:** Incremental migration of `shellcheck/SC####` checks from embedded ShellCheck WASM to native Go while preserving rule IDs, config,
directives, reporting, and fixes.

**Key Topics:**

- Rule ownership bridge (`wasm` vs `native`) under one stable `shellcheck/*` namespace
- SC1040 pilot implementation strategy and compatibility constraints
- Upstream ShellCheck source/test discovery protocol for future ports
- Required “port all upstream-relevant test vectors” policy
- Native SC autofix integration model and rollout checklist
- Alignment with Windows shell-gating behavior from doc 27

---

### 29. [Lessons from Shipwright: Build-Aware Dockerfile Repair](29-shipwright-lessons-build-aware-repair.md)

**Covers:** Actionable lessons from Shipwright’s academic Dockerfile repair system, mapped onto Tally’s roadmap for diagnostics, slow checks,
repair safety, datasets, and future search/ACP workflows.

**Key Topics:**

- Why static linting alone misses many real build failures
- Error taxonomy lessons for package churn, missing dependencies, stale URLs, and base-image drift
- Build-time validation and fix-safety classification for slower or riskier repairs
- Search-based and ML-assisted recommendation ideas worth adapting selectively
- Benchmark-dataset and real-world PR-validation lessons for evaluating repair quality

**Based on:** the Shipwright paper, dataset, and implementation

---

### 30. [AST-Aware Semantic Highlighting for CLI Snippets and LSP](30-ast-semantic-highlighting-cli-lsp.md)

**Covers:** A shared semantic-token engine for Dockerfile and embedded shell syntax that replaces Chroma in the CLI and powers LSP
`semanticTokens/full|range`.

**Key Topics:**

- One tokenization engine with separate CLI and LSP adapters
- Reuse of existing BuildKit AST, shell parsing, semantic model, and sourcemap primitives
- Token normalization rules that preserve stable spans across renderers
- Incremental rollout strategy: `full` and `range` first, `full/delta` later
- Parser-backed PowerShell support and conservative lexical fallback for `cmd`

**Based on:** tally’s existing AST/semantic/highlighting internals and the LSP semantic tokens protocol

---

### 31. [Semantic Token Alignment With `better-dockerfile-syntax`](31-semantic-token-alignment-with-better-dockerfile-syntax.md)

**Covers:** How Tally’s semantic-token model should align with the `better-dockerfile-syntax` TextMate grammar without erasing useful Dockerfile
and embedded-shell scopes.

**Key Topics:**

- Why `better-dockerfile-syntax` is useful as a grammar reference, not as a semantic-token source
- Where semantic token boundaries currently override helpful TextMate scopes
- Why parser directives such as `# syntax=` and `# escape=` need Tally-owned semantic structure
- How `semanticTokenScopes` fallback mapping should mirror the grammar’s real scope vocabulary
- A staged plan: fix boundaries first, then fix scope mapping, then add Dockerfile-specific token types if needed

**Based on:** `jeff-hykin/better-dockerfile-syntax`, VS Code semantic token behavior, and Tally’s current token legend and fallback mapping

---

### 32. [GPU Container Rules](32-gpu-container-rules.md)

**Covers:** Consolidated proposal for a new GPU-specific rule namespace, including normalized rule naming, priority ranking, rollout waves, AI/ACP
fit, and explicit out-of-scope generic proposals

**Key Topics:**

- Cross-report consensus rules: `prefer-runtime-final-stage` and `prefer-uv-over-conda`
- High-confidence GPU correctness checks such as CUDA version drift and build-time GPU queries
- Runtime policy rules for visible devices and driver capabilities
- Why generic cache/no-cache and `--no-install-recommends` rules should stay out of `tally/gpu/*`
- Feasibility with Tally's existing `SuggestedFix`, async resolver, and ACP plumbing

**Based on:** Docker, NVIDIA Container Toolkit, Hugging Face, uv, and real-world GitHub GPU Dockerfile corpus analysis

---

### 33. [STOPSIGNAL Rules (top-level `tally/*`)](33-stopsignal-rules.md)

**Covers:** Evidence-backed proposal for STOPSIGNAL-specific lint rules, including daemon-aware
signal mappings, PID 1 / wrapper caveats, and a phased rollout plan

**Key Topics:**

- Balanced GitHub corpus analysis of 100 STOPSIGNAL-bearing Dockerfiles/Containerfiles
- Generic correctness and hygiene rules for `SIGKILL`, non-canonical tokens, and shell/wrapper PID 1
- Daemon-specific mappings for systemd/init, nginx, php-fpm, postgres, and httpd
- Explicit out-of-scope daemons where default `SIGTERM` is already the right behavior
- Recommended implementation order and fix-safety boundaries

**Based on:** Dockerfile reference docs, upstream daemon shutdown docs, official image templates, and
real-world GitHub Dockerfile/Containerfile corpus analysis

---

### 34. [USER / useradd / privilege transitions in Dockerfiles](34-user-instructions.md)

**Covers:** Evidence-backed analysis of what `USER` actually changes at build time and runtime, common real-world `useradd` / numeric-user patterns,
and a high-signal proposal for USER-specific `tally/*` rules.

**Key Topics:**

- Why `USER` is important but narrower than generic host-level "root vs non-root" explanations imply
- Build-time semantics: `RUN`, `WORKDIR`, and why `COPY` / `ADD` ownership is separate
- Real-world patterns across Debian/Ubuntu, Alpine, distroless, and `scratch`
- High-confidence Dockerfile-only USER rules vs future cross-surface deployment-aware rules
- A proposed supporting fact for known default non-root base images

**Based on:** Dockerfile reference docs, Docker rootless docs, Distroless and Chainguard guidance, OpenShift/Podman guidance, and a curated GitHub
Dockerfile corpus analysis

---

### 35. [PHP Container Rules (`tally/php/*`)](35-php-container-rules.md)

**Covers:** Corpus-backed proposal for PHP-specific Dockerfile rules around Composer, PHP extension builds, Xdebug, OPcache, and runtime-user
patterns.

**Key Topics:**

- Balanced and app-heavy GitHub corpus analysis of PHP Dockerfiles/Containerfiles
- PHP-specific rule candidates for Composer install flags, manifest bind mounts, extension-build cleanup, and runtime hardening
- Alignment against Docker's PHP guide, Composer docs, Symfony deployment guidance, Laravel Sail, and the PHP OPcache manual
- Interoperability notes with the broader USER-rule strategy from doc 34
- Tracking follow-up work such as multi-fix LSP support for alternative IDE quick fixes
- Appendix guidance for publishing Tally to Composer/Packagist via `codewithkyrian/platform-package-installer`

**Based on:** Docker PHP guidance, Composer docs, Symfony docs, PHP manual, community PHP image guidance, and a curated GitHub PHP Dockerfile
corpus analysis

---

### 36. [Telemetry Opt-Out Rule Research](36-telemetry-opt-out-rule-research.md)

**Covers:** Source-backed research for a stage-scoped rule that disables telemetry, analytics, or tracking for tools used inside a container image.

**Key Topics:**

- Tool-specific telemetry opt-out signals vs unsupported "global switch" assumptions
- When `ENV`-based fixes are credible and when command/config-based fixes are too weak
- Stage-scoped detection and insertion strategy
- Windows container constraints and noise-reduction policy

**Based on:** Official tool documentation, Windows container guidance, and a research pass over common CLI telemetry conventions

---

### 37. [Command-Family Normalization: Semantic Lift/Lower With ACP Fallback](37-ai-autofix-command-family-normalization.md)

**Covers:** A semantic command-family normalization design for `hadolint/DL4001` where tally builds reusable command-family operation facts once per
file, using semantic-model context plus env and observable-file state, then lowers those operations into a preferred target tool, validates the
Dockerfile-relevant outcome mechanically, and only falls back to ACP when deterministic transpilation fails.

**Key Topics:**

- Why command-family fixes should model operations and outcomes rather than map flags argument-to-argument
- How to adapt `curlconverter`'s parse/lift/lower architecture without inheriting its warning-only acceptance model
- Why the IR should live in the facts layer and be shared across rules rather than rebuilt per rule
- Dockerfile-relevant equivalence: files, streams, exit behavior, package state, and contextual config inputs
- Provenance from env bindings, observable files, and command windows back to source lines
- Family-specific IRs and capability tables for `curl`/`wget` and later `npm`/`bun`
- Replacement-window ACP fallback with structured blocker and partial-operation context

**Based on:** tally shell/fix internals, `curlconverter`, and prior ACP design docs

---

### 38. [BuildInvocation Model and Docker Bake / Compose Integration](38-buildinvocation-bake.md)

**Covers:** A research deliverable that proposes `BuildInvocation` as a first-class representation of *how* a Dockerfile is actually built, and
designs orchestrator-file entrypoints (`tally lint docker-bake.hcl`, `tally lint compose.yaml`) that harvest build args, platforms, target stages,
named build contexts, and service-level runtime metadata, so context-aware rules can reason about invocation data without users restating it on
the CLI.

**Key Topics:**

- `BuildInvocation` as the top-level concept wrapping today's filesystem-oriented `BuildContext`
- Symmetric treatment of Docker Bake HCL and Docker Compose YAML as primary invocation sources (upstream libraries as deps, never vendored)
- Content-sniffed `tally lint <path>` entrypoint dispatch; `--target` / `--service` for scope narrowing
- Hard rejection of `--fix` on orchestrator entrypoints as a design invariant (conflicting fixes across invocations)
- Zero-Dockerfile orchestrator exits 0 with an explanatory notice so CI lint can be wired up early
- Multi-invocation reporting: one Dockerfile × N invocations → N lint runs grouped by target/service
- Interaction with the async / registry-backed analysis layer via `Invocation.Platforms` / `BuildArgs`
- UX tradeoffs (entrypoint vs flag vs subcommand); three-phase rollout plan with concrete MVP acceptance criteria

**Based on:** issue [#327](https://github.com/wharflab/tally/issues/327), existing design docs 02 and 07, `github.com/docker/buildx/bake`,
`github.com/compose-spec/compose-go/v2`, real bake file from `moby/buildkit`

---

### 39. [Dockadvisor Parity Analysis](39-dockadvisor-parity-analysis.md)

**Covers:** A rule-by-rule comparison between Dockadvisor and tally, identifying overlap, gaps, and follow-up opportunities such as EXPOSE
validation, image-reference checks, and Dockadvisor's quality scoring and WASM model.

---

### 40. [LABEL Rules Research and Proposal](40-label-rules-research.md)

**Covers:** Research and implementation planning for the `tally/labels/*` namespace, including duplicate label detection, label-key validation,
Buildx and Compose label ownership, schema-driven label policy, and future label organization rules.

---

### 41. [Docker CLI Plugin Support](41-docker-cli-plugin.md)

**Covers:** Two-phase implementation plan for running tally as `docker lint`: first migrate the small standalone CLI from `urfave/cli/v3` to
Cobra while preserving koanf-backed config precedence, then add Docker CLI plugin support with Docker's Cobra plugin helper, Homebrew
`lib/docker/cli-plugins` integration, WinGet considerations, release smoke tests, and dedicated docs.

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
- **Schema-first config/rule migration** → [18-json-schema-first-config-and-rule-system.md](18-json-schema-first-config-and-rule-system.md)

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
