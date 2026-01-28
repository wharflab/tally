# BuildKit Phase 2 Rules: Path Forward

This document captures a fact-based recommendation for how tally should handle BuildKit “Phase 2” Dockerfile lint rules long-term.
It builds on the research in `docs/buildkit-phase2-rules-research.md`.

## Executive summary (decision)

**Recommendation:** Implement Phase 2 rules in tally using our semantic model (and local filesystem context) rather than calling BuildKit’s
`dockerfile2llb.DockerfileLint` directly. Keep any registry-/image-metadata-backed checks behind an explicit opt-in mode.

**Why:** BuildKit’s Phase 2 lint implementation is coupled to the Dockerfile → LLB conversion pipeline and can require:

- A non-trivial `SourceMap` setup (including a marshaled LLB `Definition`)
- Optional gateway-backed `dockerui.Client` behavior (e.g., `.dockerignore` retrieval)
- Image metadata resolution via `MetaResolver` (defaulting to a networked registry resolver)

These are architectural mismatches for a fast, deterministic, offline-friendly CLI linter.

## Scope and inputs

- The statements below are grounded in the BuildKit version currently used by tally: `github.com/moby/buildkit v0.27.0` (see `go.mod`).
- Code references below refer to BuildKit source paths under:
  - `frontend/dockerfile/dockerfile2llb/convert.go`
  - `frontend/subrequests/lint/lint.go`
  - `client/llb/imagemetaresolver/resolver.go`
  - `frontend/dockerfile/linter/ruleset.go`

## Facts from BuildKit v0.27.0

### 1) `DockerfileLint` is not “lint-only”

`dockerfile2llb.DockerfileLint(ctx, dt, opt)`:

- Adds a source to lint results via `results.AddSource(opt.SourceMap)`.
- Runs the full conversion setup by calling `toDispatchState(ctx, dt, opt)`.

The lint results source handling expects a `SourceMap` that can be serialized to protobuf. In `frontend/subrequests/lint/lint.go`,
`LintResults.AddSource` calls `sourceMap.Definition.ToPB()`, so `SourceMap.Definition` must be present for safe use.

### 2) Base image config resolution happens for reachable stages

In `frontend/dockerfile/dockerfile2llb/convert.go`, `toDispatchState` selects an image metadata resolver:

```go
metaResolver := opt.MetaResolver
if metaResolver == nil {
    metaResolver = imagemetaresolver.Default()
}
```

Later, for reachable stages, it resolves image config:

```go
mutRef, dgst, dt, err := metaResolver.ResolveImageConfig(ctx, d.stage.BaseName, sourceresolver.Opt{ ... })
```

The default resolver (`client/llb/imagemetaresolver/resolver.go`) uses containerd’s docker registry resolver (`docker.NewResolver(...)`) and calls
`imageutil.Config(...)`, which implies registry/network access and an auth surface.

**Implication:** even if tally already builds a correct stage graph and reachability model, calling BuildKit’s Phase 2 lint implementation will still
perform image config resolution for reachable stages unless you provide a resolver that satisfies it.

### 3) Several “Phase 2” rules are pure semantic/static analysis

Some rules are fully implementable from the parsed Dockerfile + semantic state, without registry access:

- **`WorkdirRelativePath`**: triggered only for local WORKDIR instructions (`commit == true`), uses a stage-level `workdirSet` flag and
  `system.IsAbs(...)`.
- **`SecretsUsedInArgOrEnv`**: regex match on ARG/ENV keys (deny tokens include `apikey`, `auth`, `credential(s)`, `key`, `password/pword/passwd`,
  `secret`, `token`; allow token includes `public`).
- **`RedundantTargetPlatform`**: derived from the platform expression substitution result and the `TARGETPLATFORM` env value.
- **`CopyIgnoredFile`**: uses dockerignore patterns via `patternmatcher.PatternMatcher`.

These are excellent fits for tally’s semantic model approach and can be deterministic/offline.

### 4) Some rules are inherently image-metadata-backed

- **`InvalidBaseImagePlatform`**: compares the expected target platform with the platform of the resolved base image config.

To implement this rule with full parity, you need a reliable image metadata source (registry access, local content store, or explicit user-provided
metadata).

## Recommended path for tally

### Default behavior (v1.x): semantic-model implementation

Implement the Phase 2 rules that are deterministic from:

- Dockerfile text
- Parsed instructions + stage graph
- Local filesystem context (e.g. `.dockerignore`)

Concretely:

- Keep/finish: `CopyIgnoredFile`
- Add: `WorkdirRelativePath`, `SecretsUsedInArgOrEnv`, `RedundantTargetPlatform`

**Parity requirements:**

- Use BuildKit’s rule IDs/names (e.g., `WorkdirRelativePath`) and match message formatting where practical.
- Match semantics, not just messages (e.g., `WorkdirRelativePath` warns on the first local relative WORKDIR when no prior local absolute WORKDIR
  exists).

### Optional “parity mode” (explicit opt-in)

Add a mode that performs registry-/metadata-backed checks when the user opts in:

- Enable `InvalidBaseImagePlatform` only when:
  - the user allows image resolution, or
  - the user provides an image metadata source (cache, OCI layout, etc.).

Guardrails:

- Make this mode best-effort and cache results keyed by `ref + platform`.
- Clearly separate “could not resolve image metadata” from “rule passed/failed”.

### Upstream strategy (nice-to-have, not gating)

If we want full BuildKit parity without reproducing BuildKit frontend plumbing, propose upstream changes such as:

- A true “lint-only” API that does not require `SourceMap.Definition` and can work with an optional/empty source.
- Optional disabling of base image metadata resolution for lint-only runs (or a resolver API designed for offline use).
- Ability to inject dockerignore patterns directly (decouple from gateway-backed `dockerui.Client`).

## Why not call BuildKit `DockerfileLint` directly (even with a semantic model)

Even with a correct semantic model in tally:

- BuildKit’s lint path is still coupled to image config resolution for reachable stages (unless a resolver is provided).
- BuildKit’s lint results expect `SourceMap.Definition` to be populated, pushing you toward LLB marshaling or gateway emulation.
- This coupling reduces determinism/offline-friendliness and increases maintenance/complexity for a CLI linter.

## Implementation checklist

1. Implement the static Phase 2 rules in `internal/lint/` using the semantic model.
2. Add/update integration snapshots to pin rule IDs/messages and behavior.
3. (Optional) add a separate opt-in pathway for registry-backed checks with caching and clear UX.

## When to revisit this decision

Re-evaluate using upstream BuildKit lint directly if:

- BuildKit introduces an officially supported lint-only API that removes SourceMap/LLB and registry coupling, or
- tally evolves into a tool that already runs in a BuildKit gateway environment (unlikely for a local CLI linter).
