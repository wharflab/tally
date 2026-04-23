# BuildInvocation, Orchestrator Entrypoints, and IDE Integration

**Status:** Implementation-ready proposal

**Primary goal:** make tally understand the planned build, not just the Dockerfile text, by introducing a first-class `BuildInvocation` model and
supporting Docker Bake and Docker Compose as explicit lint entrypoints. The same model must also be reusable by `tally lsp --stdio` and the IDE
plugins built on top of it.

**Tracking issue:** [#327](https://github.com/wharflab/tally/issues/327)

**Related design docs:**

- [01 — Linter Pipeline Architecture](01-linter-pipeline-architecture.md)
- [02 — Docker Buildx Bake `--check` Analysis](02-buildx-bake-check-analysis.md)
- [05 — Reporters and Output Formatting](05-reporters-and-output.md)
- [07 — Context-Aware Linting Foundation](07-context-aware-foundation.md)
- [11 — VSCode Extension Architecture](11-vscode-extension-architecture.md)
- [17 — IntelliJ Plugin Lean Build](17-intellij-plugin-lean-build.md)

---

## Decision Summary

This proposal makes four concrete product decisions:

1. `tally lint <path>` continues to be the only CLI entrypoint, but an **explicit file path** may now be a Dockerfile, a Bake file, or a Compose
   file.
2. tally introduces `BuildInvocation`, a stable data model describing **one planned build of one Dockerfile under one invocation context**.
3. Linting an orchestrator file fans out into **one lint run per invocation**, then fans results back into a single report with invocation-aware
   attribution.
4. The same invocation layer is shared by the CLI and LSP, but the editor workflow is intentionally narrower: the first LSP integration remains
   **Dockerfile-document-centric**, with invocation context supplied by explicit workspace settings rather than by making Bake or Compose documents
   first-class LSP targets.

The design is intentionally conservative where silent partial support would make results untrustworthy. Unsupported orchestrator constructs must
raise clear errors instead of being ignored.

---

## Problem

An increasing number of useful Dockerfile checks depend on how the file is actually built:

- build args
- selected target stage
- selected platform(s)
- additional or named build contexts
- service-level runtime metadata from Compose such as ports, env vars, labels, networks, and secrets

Today tally has a filesystem-oriented build context in [`internal/context/context.go`](../internal/context/context.go), but it does not have a
first-class concept of the **invocation context**. That leaves several classes of bugs invisible until build time:

- `COPY --from=<name>` that relies on a named build context declared outside the Dockerfile
- `FROM ${NODE_VERSION}` where the real value only exists in Bake or Compose
- platform-sensitive base image checks that need the actual target platform instead of host defaults
- Compose services whose `ports:` or `environment:` drift from the Dockerfile they build

Requiring users to restate `--build-arg`, `--target`, `--platform`, or `--build-context` on the tally CLI is not acceptable:

- the source of truth already exists in Bake HCL or Compose YAML
- duplicating it on the CLI creates drift
- once the two disagree, lint output stops being trustworthy

The correct unit is not “a Dockerfile plus a few optional flags”. The correct unit is “a planned build invocation”.

---

## Goals

- Add a `BuildInvocation` model that can express the build-time and service-time data future rules will need.
- Accept explicit Bake and Compose files as `tally lint` entrypoints without changing existing Dockerfile behavior.
- Preserve current rule behavior when invocation data is absent.
- Reuse the same invocation layer in CLI, LSP, and IDE plugins.
- Keep the MVP implementable without refactoring the entire lint engine or reporter stack.
- Preserve future extensibility for richer orchestrators, new invocation facets, and invocation-aware rules.

## Non-Goals

- New rules that consume invocation data in the same implementation issue. The MVP is plumbing first.
- Parsing CI definitions such as GitHub Actions or GitLab pipelines.
- Lint rules targeting Bake or Compose files themselves.
- Implicit CLI auto-discovery of orchestrator files from a Dockerfile path.
- Compose override layering beyond what the explicitly provided file already references through the upstream parser.
- Bake or Compose inline Dockerfiles in the MVP. These must fail clearly instead of being silently ignored.
- Autofix support when the CLI entrypoint is an orchestrator file.

---

## Hard Constraints

These are design constraints, not suggestions.

1. **CLI orchestrator entrypoints and `--fix` are incompatible.** The same Dockerfile can be linted under multiple invocations, so a mutating
   orchestrator run is ambiguous by definition.
2. **Malformed orchestrator files fail fast.** No partial linting.
3. **Unsupported orchestrator features fail clearly.** tally must not silently drop semantics it cannot model yet.
4. **Each invocation is independent.** The same Dockerfile referenced by N invocations yields N lint runs and N sets of attributed diagnostics.
5. **Existing rules remain valid.** A rule that does not care about invocation data must continue to work without code changes.
6. **Config discovery stays Dockerfile-based.** For an orchestrator run, each invocation loads config as if the referenced Dockerfile had been
   linted directly, then applies CLI overrides on top.
7. **The provider layer is shared.** CLI and LSP must build on the same invocation discovery and normalization code, not on separate ad hoc
   implementations.
8. **Paths are canonical internally.** All path-valued fields are stored as absolute, `filepath.Clean`'d paths.

---

## User-Facing CLI Behavior

### Entrypoint Rules

The CLI keeps one command: `tally lint <path>`.

Entrypoint classification changes as follows:

- Directory inputs and glob inputs keep their current meaning: Dockerfile discovery only.
- A single explicit regular file path may be classified as Dockerfile, Bake, or Compose.
- Orchestrator mode is only available when the user passes exactly one explicit regular file path.

That yields the following behavior:

| User input | Meaning in MVP |
|---|---|
| `tally lint .` | Existing Dockerfile discovery flow. No orchestrator detection. |
| `tally lint Dockerfile` | Existing single-Dockerfile flow. |
| `tally lint compose.yaml` | New Compose orchestrator flow. |
| `tally lint docker-bake.hcl` | New Bake orchestrator flow. |
| `tally lint compose.yaml Dockerfile` | Error, exit `2`. Mixed explicit inputs are not supported in orchestrator mode. |
| `tally lint -` | Existing stdin Dockerfile flow only. stdin is not an orchestrator entrypoint in MVP. |

This preserves current directory/glob behavior while making the orchestrator workflow explicit and predictable.

### Dispatch Algorithm

For a single explicit file input:

1. If the file name matches Dockerfile conventions (`Dockerfile`, `*.Dockerfile`, `Containerfile`, variants), treat it as Dockerfile mode.
2. Else if the file name or content matches Bake, treat it as Bake mode.
3. Else if the file name or content matches Compose, treat it as Compose mode.
4. Else error with exit `2`.

Filename patterns are a fast path; parser-backed sniffing is the source of truth.

### Flag Semantics

- `--target` is valid only in Bake mode.
- `--service` is valid only in Compose mode.
- Both flags may be repeated.
- unknown `--target` names exit `2`.
- unknown `--service` names, or services that exist but have no `build:` section, exit `2`.
- `--context` is valid only in plain Dockerfile mode. It is an error in orchestrator mode because the orchestrator is already the source of
  context truth.
- `--fix` is valid only in plain Dockerfile mode and stdin Dockerfile mode.

Invalid flag/mode combinations exit `2`.

### Exit Codes

The existing repo exit codes remain authoritative:

| Exit code | Meaning |
|---|---|
| `0` | Clean run, or a valid orchestrator file that resolves to no lintable Dockerfile invocations. |
| `1` | Violations at or above the configured fail level were found. |
| `2` | CLI misuse, orchestrator parse/load failure, config error, or unsupported orchestrator feature. |
| `3` | Existing “no Dockerfiles found” case from directory/glob discovery only. |
| `4` | Fatal Dockerfile syntax error in any referenced Dockerfile. |

Two points matter here:

- A valid Compose/Bake file with zero lintable Dockerfiles is **not** “no files found”; it exits `0`, not `3`.
- If an orchestrator resolves to one or more Dockerfiles and any of those Dockerfiles has a fatal syntax error, the run exits `4`, preserving the
  current Dockerfile syntax contract.

### Reporter UX

Reporter behavior must remain deterministic and invocation-aware:

- `text` and `markdown` group diagnostics by invocation source.
- `json` and `sarif` attach a structured `invocation` object to each diagnostic.
- `github-actions` stays flat, but prefixes the message with an invocation label because that format has no grouping primitive.

The report summary must distinguish:

- unique Dockerfiles scanned
- invocations scanned
- total diagnostics

`reporter.ReportMetadata` therefore needs a new `InvocationsScanned` field in addition to `FilesScanned`.

Illustrative text output:

```text
[compose service: api]
  api/Dockerfile:12: hadolint/DL3008 - Pin versions in apt-get install

[compose service: worker]
  worker/Dockerfile:8: hadolint/DL3008 - Pin versions in apt-get install

Summary: 2 Dockerfiles, 2 invocations, 2 violations.
```

Even if two invocations point at the same Dockerfile text, they remain distinct findings.

---

## Core Architecture

### BuildInvocation Model

`BuildInvocation` describes one planned build of one Dockerfile under one invocation context.

Illustrative shape:

```go
// Package invocation models planned Dockerfile builds.
// Illustrative only; exact package/file layout may vary.
package invocation

type BuildInvocation struct {
	// Stable identity for one invocation within a run. Used by reporters,
	// async bookkeeping, and LSP diagnostics.
	Key string

	Source InvocationSource

	// Absolute path to the Dockerfile being linted.
	DockerfilePath string

	// Declared primary build context, whether local or remote.
	ContextRef ContextRef

	BuildArgs     map[string]*string
	Platforms     []string
	TargetStage   string
	NamedContexts map[string]ContextRef

	// Compose-derived runtime metadata. Usually empty for Bake.
	Environment  map[string]*string
	ExposedPorts []Port
	Networks     []string
	Labels       map[string]string
	Secrets      []SecretRef
}

type InvocationSource struct {
	Kind string // "dockerfile" | "bake" | "compose"
	File string // absolute path to the originating file
	Name string // target or service name; empty for plain Dockerfile mode
}

type ContextRef struct {
	Kind  string // "dir" | "git" | "url" | "docker-image" | "target" | "service" | "oci-layout" | ...
	Value string // absolute path for local dirs; canonical string otherwise
}

type Port struct {
	ContainerStart int
	ContainerEnd   int
	HostStart      int
	HostEnd        int
	Protocol       string
}

type SecretRef struct {
	ID     string
	Source string
	Target string
}
```

### Why `BuildInvocation` stays declarative

`BuildInvocation` is the semantic model. It should describe what the build declaration says, not carry around a prepared local filesystem helper.

- `ContextRef` captures the declared primary context whether it is local, remote, git-based, or otherwise non-filesystem.
- local filesystem inspection is a **derived linter capability**, not part of the invocation model itself.

For local directory contexts (`ContextRef.Kind == "dir"`), the linter may construct an internal helper that implements the file-observation
capabilities needed by the facts layer and the small number of filesystem-aware checks. For remote or non-local contexts, no such helper exists.

This keeps the model correct while still allowing `.dockerignore` evaluation and safe file access when that capability is actually available.

### Build Arg Semantics

`BuildArgs` uses `map[string]*string` to preserve three states:

- declared without concrete value yet
- explicitly set to empty string
- explicitly set to a non-empty value

The lint pipeline derives a plain `map[string]string` only for the subset of args with concrete values when building the semantic model. Nil-valued
args remain “declared but unresolved”.

### Path Normalization

All path-valued fields are stored as absolute, cleaned paths:

- `BuildInvocation.DockerfilePath`
- `InvocationSource.File`
- `ContextRef.Value` when `Kind == "dir"`
- any local path captured in `SecretRef.Source`

Reporters and examples may render relative paths for readability, but equality and cache keys use the canonical absolute form.

### Discovery and Provider Contract

Introduce a shared provider layer under a new package such as `internal/invocation/`.

Illustrative contract:

```go
type ResolveOptions struct {
	Path     string
	Targets  []string // bake only
	Services []string // compose only
}

type DiscoveryResult struct {
	Kind              string // "dockerfile" | "bake" | "compose"
	EntrypointPath    string
	Invocations       []BuildInvocation
	ZeroLintableReason string // only set when Invocations is empty and parsing succeeded
}

type Provider interface {
	Discover(ctx context.Context, opts ResolveOptions) (*DiscoveryResult, error)
}
```

`ZeroLintableReason` exists so the CLI, LSP, and tests can distinguish:

- parser failure
- unsupported feature failure
- valid file with nothing to lint

This is better than inferring semantics from an empty slice alone.

### Pipeline Integration

The implementation should touch the current pipeline in the smallest set of places that preserves correctness.

### CLI wiring

`cmd/tally/cmd/lint.go` keeps today’s directory/glob discovery path for Dockerfiles. A new higher-level dispatcher handles the single-explicit-file
case:

1. classify the explicit file as Dockerfile, Bake, or Compose
2. plain Dockerfile: existing code path
3. Bake/Compose: provider discovery, then per-invocation lint fan-out

`internal/discovery` remains Dockerfile-focused. Orchestrator resolution is a higher layer, not a redefinition of directory/glob discovery.

### `linter.Input` and `rules.LintInput`

Add:

```go
Invocation *invocation.BuildInvocation
```

to both `linter.Input` and `rules.LintInput`.

Do **not** add `context.BuildContext` or a similar filesystem helper to `BuildInvocation`.

The current `rules.BuildContext` / `LintInput.Context` path should be retired rather than propagated. The implementation should derive a narrow
local-context reader from `Invocation.ContextRef` only when the primary context is a local directory, and use that internal capability where
needed during lint execution.

That reader can reuse the existing implementation in `internal/context/`, but it is an execution detail, not part of the public invocation model.

### Parse caching and per-invocation rebuild

When the same Dockerfile appears in multiple invocations:

- parse the Dockerfile once per unique `DockerfilePath`
- reuse the `dockerfile.ParseResult`
- rebuild the semantic model, facts, and rule input **per invocation**

This distinction is mandatory because invocation data affects semantic interpretation:

- build args
- selected target stage
- future platform-sensitive checks

The current semantic builder already accepts build args. It must also gain an invocation-aware target stage input, because using “last stage in the
file” is incorrect once the real build target comes from Bake or Compose.

### Derived local context capability

Filesystem-aware analysis should be driven from a narrow internal capability derived from `Invocation.ContextRef`, not from `*context.BuildContext`
stored on the model.

The recommended shape is:

- keep `BuildInvocation` declarative
- when `Invocation.ContextRef.Kind == "dir"`, build a local context reader for the facts layer
- refactor the remaining direct `LintInput.Context` consumer(s) to use facts or the same narrow reader
- remove `rules.BuildContext` from the steady-state rule API

This is feasible because the current production rule usage of `LintInput.Context` is minimal, while most context-derived behavior already flows
through the facts layer.

### Config loading

For orchestrator mode, config discovery must use the Dockerfile path, not the orchestrator file path. That preserves the current cascading config
semantics and avoids surprising users who already colocate `.tally.toml` next to Dockerfiles.

### Async and registry integration

The existing async runtime can remain in place, but the bookkeeping key space must expand.

Today several async flows identify work by combinations of:

- file path
- stage index
- resolver key

That is not sufficient once the same Dockerfile/stage is linted under multiple invocations.

Required change:

- add `InvocationKey string` to `async.CheckRequest`
- add `InvocationKey string` to `async.CompletedCheck`
- add `InvocationKey string` to `rules.Violation`

Resolver result dedupe may still happen by `(ResolverID, Key)` when the network lookup is identical. What changes is the **merge/suppression**
identity, which must become `(RuleCode, File, InvocationKey, StageIndex)`.

This preserves the current async architecture while preventing one invocation from suppressing another invocation’s diagnostics.

### Violation and reporter attribution

Add an optional `Invocation` field to `rules.Violation` for serialization and UI:

```go
Invocation *InvocationSource `json:"invocation,omitempty"`
```

Populate it whenever `LintInput.Invocation != nil`.

Required reporter updates:

- `reporter.SortViolations` sorts by invocation label before file/line/rule.
- JSON and SARIF include `invocation`.
- GitHub Actions prefixes the human-readable message with the invocation label.
- text and markdown group by invocation.

---

## Bake Provider

### Upstream dependency

Use the official Buildx package:

- [`github.com/docker/buildx/bake`](https://pkg.go.dev/github.com/docker/buildx/bake)

The provider must delegate HCL parsing and target resolution to Buildx rather than re-implementing Bake semantics.

### Target selection semantics

The provider must follow Buildx’s own target-selection semantics:

- explicit `--target` names are authoritative
- when no targets are supplied, use the same target set `docker buildx bake` would use for that file

In practice, that means:

- if the file defines a `default` group, lint the targets in that group
- otherwise lint the targets Buildx resolves as the implicit default for that file

tally must not invent a different Bake selection model.

### Mapping

| Bake field | BuildInvocation field | Notes |
|---|---|---|
| target name | `Source.Name` | Always set. |
| file path | `Source.File` | Absolute path to the Bake file. |
| `context` | `ContextRef` | Classify local dir vs remote/git/url. Local dir contexts may additionally yield a derived local context reader during lint execution. |
| `dockerfile` | `DockerfilePath` | Default `Dockerfile` under the resolved context dir. |
| `args` | `BuildArgs` | Preserve nil vs empty vs explicit values. |
| `platforms` | `Platforms` | |
| `target` | `TargetStage` | |
| `contexts` | `NamedContexts` | Classify each reference. |
| `secret` | `Secrets` | Metadata only, never secret values. |
| `labels` | `Labels` | |

### Unsupported or deferred Bake features

| Feature | MVP behavior | Reason |
|---|---|---|
| `dockerfile-inline` | hard error, exit `2` | No stable file-path/report/fix contract in the MVP. |
| unresolved matrix expansion | hard error, exit `2` | Partial or collapsed linting would be misleading. |
| remote Bake definition URLs | hard error, exit `2` | CLI entrypoint model is explicit local files only. |

Important nuance on matrix:

- if the upstream Buildx API already returns fully materialized targets, each materialized target becomes its own invocation
- if unresolved matrix structure remains visible to tally, the provider must error rather than guess

### Example of a concrete future payoff

A future rule can validate:

> `COPY --from=<name>` that is not a prior stage name must correspond to a declared named build context.

That check becomes possible because Bake already exposes the `contexts` map in provider discovery.

---

## Compose Provider

### Upstream dependency

Use the official Compose Go library:

- [`github.com/compose-spec/compose-go/v2`](https://github.com/compose-spec/compose-go)

Use the upstream project-loading flow rather than hand-rolling YAML, interpolation, profile filtering, or path normalization. The preferred
implementation path is the official project loader API (`cli.NewProjectOptions` / `LoadProject`) or an equivalent upstream-supported loader path.

### Service selection semantics

Without `--service`, lint all services in the loaded project that:

- are active in the default profile set
- have a `build:` section

With `--service`, lint only the selected services, but still reject profile-only services that are inactive because Compose profiles are out of
scope for the MVP.

Image-only services are valid inputs and count toward the “nothing to lint” notice, but they do not produce invocations.

### Mapping

| Compose field | BuildInvocation field | Notes |
|---|---|---|
| service name | `Source.Name` | Always set. |
| Compose file path | `Source.File` | Absolute path to the file the user passed. |
| `build.context` | `ContextRef` | Local path or remote source. Local dir contexts may additionally yield a derived local context reader during lint execution. |
| `build.dockerfile` | `DockerfilePath` | Default `Dockerfile` under the resolved context dir. |
| `build.args` | `BuildArgs` | Preserve nil values. |
| `build.platforms` | `Platforms` | Fall back to service `platform` only when appropriate. |
| `build.target` | `TargetStage` | |
| `build.additional_contexts` | `NamedContexts` | Classify `dir`, `docker-image`, `service`, git/url, etc. |
| `build.secrets` | `Secrets` | Cross-reference top-level secret sources when possible. |
| `build.labels` and service `labels` | `Labels` | Merge into one map with clear precedence. |
| service `environment` | `Environment` | Runtime env, not build args. |
| service `ports` | `ExposedPorts` | Preserve ranges instead of exploding them. |
| service `networks` | `Networks` | |

### Unsupported or deferred Compose features

| Feature | MVP behavior | Reason |
|---|---|---|
| `build.dockerfile_inline` | hard error, exit `2` | Same reason as Bake inline Dockerfiles. |
| profiles beyond the default set | unsupported | Avoid loading behavior that differs from explicit selection semantics. |
| implicit sibling override file loading | unsupported | CLI stays explicit; no hidden extra files. |
| unsaved orchestrator buffer state in LSP | unsupported | Initial LSP implementation reads orchestrators from disk only. |

This is intentionally strict. Compose files are often heavily templated, and silent partial support would create false confidence.

### Future rule examples unlocked by Compose

- cross-check Dockerfile `EXPOSE` against service `ports:`
- compare Dockerfile `ENV` / `ARG` usage against Compose `environment:` and `build.args:`
- diagnose a `COPY --from=` name that matches neither a stage name nor a declared additional context

---

## LSP and IDE Integration

This proposal must cover LSP and the maintained IDE plugins explicitly because the CLI fan-out model does not map directly onto “the user opened
one Dockerfile in an editor”.

### Scope Decision

The initial LSP integration stays **Dockerfile-document-centric**:

- Dockerfile documents remain the primary lint target in LSP mode.
- Bake and Compose documents are not first-class LSP lint targets in the MVP.
- Invocation context is applied to a Dockerfile document only when the workspace explicitly tells the server which orchestrator files matter.

This is the least risky path because it preserves the editor’s mental model:

- diagnostics attach to the file the user is editing
- quick fixes edit the current Dockerfile document only
- the plugins do not need to invent a multi-document grouped-report UI before the underlying invocation layer is stable

### New LSP Settings

Extend `internal/lspserver/settings.go` with a new folder-level setting:

```go
InvocationEntrypoints []string `json:"invocationEntrypoints"`
```

User-facing setting name in both plugins:

- `tally.invocationEntrypoints`

Rules:

- values are workspace-relative paths
- only explicit files are allowed
- empty list means invocation-aware editor linting is disabled

Example conceptual payload sent via `workspace/didChangeConfiguration`:

```json
{
  "tally": {
    "workspaces": [
      {
        "uri": "file:///repo",
        "settings": {
          "invocationEntrypoints": [
            "compose.yaml",
            "docker-bake.hcl"
          ]
        }
      }
    ]
  }
}
```

### LSP Server Behavior

For a Dockerfile document `D`:

1. resolve workspace settings
2. load and cache the configured orchestrator files for that workspace
3. find all invocations whose `DockerfilePath == D`
4. if zero invocations match, lint as today
5. if one invocation matches, lint with that `BuildInvocation`
6. if multiple invocations match, run one lint pass per invocation and publish multiple diagnostics for the document

The cache key for the invocation index should include:

- workspace root
- configured entrypoint paths
- content hash or mtime of each entrypoint file

This index is separate from the existing `lintCache`, which remains keyed by document version.

### Diagnostics UX in editors

LSP diagnostics need a compact attribution model because editors do not have the CLI’s grouped text report.

Required behavior:

- plain Dockerfile lint: `Diagnostic.Source = "tally"`
- invocation-aware lint: `Diagnostic.Source = "tally/<kind>:<name>"`
- `Diagnostic.Data` includes a structured invocation object so code actions and richer UIs can round-trip the metadata

Example conceptual `Diagnostic.Data`:

```json
{
  "invocation": {
    "kind": "compose",
    "file": "/repo/compose.yaml",
    "name": "api",
    "key": "compose|/repo/compose.yaml|api|/repo/api/Dockerfile"
  }
}
```

This gives VS Code and IntelliJ enough information to display a useful label or future hover/tooltip detail without parsing strings back out of the
message text.

### Fixes and Code Actions in LSP

Autofix policy must be stricter in the editor than on the CLI, because the editor always edits the Dockerfile directly.

Rules:

- zero matching invocations: existing quick-fix and fix-all behavior
- one matching invocation: existing quick-fix and fix-all behavior
- multiple matching invocations: diagnostics remain enabled, but **all mutating code actions are disabled**

That includes:

- per-diagnostic quick fixes
- `source.fixAll.tally`
- `tally.applyAllFixes`

Reason: even if a particular fix looks text-local, the editor cannot assume it is valid across all active invocation contexts.

The server should return no mutating code actions in the multi-invocation case. The plugins may optionally surface a non-mutating explanation such
as “Fixes are disabled because this Dockerfile is linted under multiple build invocations.”

### Orchestrator file change invalidation

The current LSP server already handles `workspace/didChangeConfiguration`, but it does not currently watch orchestrator files. That must change for
invocation-aware editor linting to be credible.

Required implementation:

- add `workspace/didChangeWatchedFiles` handling in the LSP server
- VS Code and IntelliJ plugins register watchers for `tally.invocationEntrypoints`
- when a watched orchestrator file changes on disk, the server invalidates the invocation index and refreshes diagnostics for open Dockerfile
  documents in that workspace

MVP limitation:

- unsaved Compose/Bake buffer edits are ignored until the orchestrator file is saved

This is an intentional scope cut. Pulling unsaved multi-document state into invocation discovery can be added later, but it should not be part of
the first implementation.

### Plugin responsibilities

#### VS Code

The VS Code extension should:

- expose `tally.invocationEntrypoints`
- forward it through `workspace/configuration`
- watch those files and forward save/change notifications
- show the diagnostic `source` label so users can distinguish invocation-specific findings

#### IntelliJ

The IntelliJ plugin should do the same at the LSP boundary:

- expose the same setting name and semantics
- watch configured orchestrator files
- invalidate diagnostics when those files change
- surface the invocation label in the LSP diagnostic UI where possible

Both plugins should stay thin. Orchestrator parsing remains server-side.

---

## Unsupported Feature Policy

The MVP must use a single consistent rule:

> If an orchestrator construct materially affects which Dockerfile is built or how it is built, and tally cannot model that construct
> correctly yet, the run errors out with exit `2`.

This is the safest policy for a lint tool. “Best effort” would create false negatives exactly in the cases where build context matters most.

At minimum this policy applies to:

- inline Dockerfiles
- unresolved Bake matrix expansion
- invalid selection flags
- unsupported profile-driven Compose selection

Remote build contexts do **not** fall under this hard-error policy as long as they can still be represented faithfully in `ContextRef`. In that
case:

- `ContextRef` is populated
- no derived local context reader is created
- filesystem-dependent rules no-op

That is a valid and truthful partial capability, unlike silently dropping a Dockerfile source or target-selection mechanism.

---

## Test Plan

The MVP is not complete without end-to-end tests that cover CLI and editor flows.

### Unit tests

Add package-level tests for:

- entrypoint classification
- path normalization
- Bake target mapping
- Compose service mapping
- unsupported-feature errors
- invocation-key stability
- async merge behavior with same file/stage under multiple invocations

### CLI integration tests

Add new integration fixtures under `internal/integration/testdata/` for at least:

- Bake file with multiple targets sharing one Dockerfile
- Compose file with multiple services, including image-only services
- valid orchestrator with zero lintable Dockerfiles
- malformed orchestrator file
- unsupported inline Dockerfile
- same Dockerfile under multiple invocations producing duplicate-attributed diagnostics
- `--fix` rejection
- `--context` rejection in orchestrator mode
- fatal Dockerfile syntax error discovered through an orchestrator run

Snapshot coverage must include:

- `text`
- `json`
- `sarif`
- `github-actions`
- `markdown`

### LSP server tests

Add tests in `internal/lspserver/` for:

- parsing and storing `invocationEntrypoints`
- zero/one/many matching invocations for one Dockerfile URI
- `Diagnostic.Source` and `Diagnostic.Data` population
- fix/code-action suppression in the multi-invocation case
- invocation-index invalidation on watched orchestrator changes

### IDE plugin tests

The plugins do not need full orchestrator parsers. They do need contract tests that verify:

- settings flow into `workspace/configuration`
- file watchers trigger refresh/invalidation
- invocation labels are visible in diagnostics

---

## Rollout Plan

### Phase 1: Shared invocation layer and CLI orchestration

Deliver:

- `internal/invocation` package with shared types and providers
- Bake and Compose provider implementations
- CLI explicit-file dispatch
- per-invocation lint fan-out
- reporter changes
- async bookkeeping changes

Acceptance:

- `tally lint compose.yaml` and `tally lint docker-bake.hcl` work end to end
- existing Dockerfile directory/glob behavior is unchanged

### Phase 2: LSP server integration

Deliver:

- `InvocationEntrypoints` LSP setting
- invocation index for Dockerfile documents
- diagnostic attribution in `Source` and `Data`
- watched-file invalidation
- multi-invocation fix suppression

Acceptance:

- opening a Dockerfile in VS Code or IntelliJ can produce invocation-aware diagnostics when the workspace is configured
- the same Dockerfile still behaves exactly as today when no invocation entrypoints are configured

### Phase 3: Invocation-aware rules

Candidate follow-ups:

- missing named build context for `COPY --from=`
- Compose `EXPOSE` vs `ports:` mismatch
- env/arg drift checks
- platform-aware registry checks
- orchestrator-file lint rules
- inline Dockerfile support
- Bake matrix expansion support
- richer editor UI for active invocations

---

## Implementation Checklist

- Add `internal/invocation` shared package.
- Add `BuildInvocation`, `InvocationSource`, `ContextRef`, and provider interfaces.
- Extend `linter.Input`, `rules.LintInput`, `rules.Violation`, and async bookkeeping with invocation identity.
- Update semantic builder to accept invocation target stage and concrete build args.
- Derive local context-reading capability from `Invocation.ContextRef` only for local directory contexts.
- Refactor remaining `rules.BuildContext` consumers onto facts or the derived local reader, and retire `rules.BuildContext` from the steady-state API.
- Add CLI dispatch for explicit file entrypoints.
- Preserve existing `internal/discovery` behavior for directories/globs.
- Update all reporters to carry invocation attribution.
- Add LSP settings, watched-file invalidation, and multi-invocation fix suppression.
- Add CLI, LSP, and plugin contract tests before any invocation-aware rules ship.

---

## Rationale for This Shape

This design is intentionally stricter than the original research framing in three places:

- orchestrator inputs are explicit and narrow on the CLI, because the current discovery model is Dockerfile-oriented and should not become
  ambiguous
- unsupported features error instead of degrading silently, because invocation-aware linting is only valuable when users can trust it
- LSP integration is Dockerfile-centric first, because that matches how the maintained IDE plugins already present diagnostics and fixes

That trade-off is the right one for the first implementation. It gives tally a reusable invocation foundation without forcing the CLI, the LSP
server, and both IDE plugins to solve every multi-file UX problem in one step.
