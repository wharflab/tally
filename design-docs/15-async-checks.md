# Async Checks (Slow Operations)

## 1. Decision

Introduce an **async checks** infrastructure to run **potentially slow operations** in a controlled, cancellable, concurrency-limited way.

This enables parity for remaining BuildKit rules that require slow I/O (notably `InvalidBaseImagePlatform`) and improves checks that can be more
accurate with external data (notably `UndefinedVar` when base image `ENV` is known).

This design intentionally mirrors the existing **async fix** pattern (see `design-docs/13-ai-autofix-acp.md` and `internal/fix`), but for
**violations generation** rather than **text edits**.

## 2. Background

tally is primarily a fast, static linter. Some checks are expensive or nondeterministic because they depend on external inputs:

1. **Network**:
   - OCI registry access (base image config/env/platform, attestation discovery)
   - HTTP checksum validation for `COPY --checksum` (remote URLs)
2. **Filesystem**:
   - Context-aware checks (scan build context, `.dockerignore`, compute local checksums)
3. **Console I/O**:
   - Long-running subprocesses / interactive-ish workflows (AI ACP agent, MCP calls, etc.)

These operations are valuable locally, but are often undesirable in CI (latency, flakiness, credentials).

## 3. Goals / Non-Goals

### Goals

1. **Fast by default, richer when allowed**: keep the existing fast pipeline; add optional slow checks behind a switch.
2. **Explicit budgeting**: timeouts + bounded concurrency; never let a single slow check block the whole run indefinitely.
3. **Robust failure mode**: if a slow check fails (network/auth/timeout), linting still succeeds; the slow finding is skipped.
4. **Clear configuration**: CLI + config can enable/disable slow checks; default to enabled locally and disabled when CI is detected.
5. **Testability**: deterministic tests via a local mock registry (no real network).

### Non-Goals (MVP)

1. Streaming/partial output while checks are still running (single final report per run).
2. Persistent on-disk cache (in-memory per run first; disk cache can be a later iteration).
3. Perfect BuildKit parity across all edge cases (start with limited scope + measurable behavior).

## 4. Terminology

- **Fast checks**: deterministic checks using Dockerfile text, AST, semantic model, and optionally already-loaded build context.
- **Async checks**: checks that require one of the slow I/O categories and therefore run under the async runtime.
- **Request**: a planned unit of async work (e.g., “resolve env for `python:3.12` on linux/amd64”).
- **Resolver**: an implementation capable of fulfilling requests (e.g., OCI registry resolver).

## 5. Architecture Overview

### 5.1 Pipeline shape

MVP pipeline (single file, simplified):

1. Parse Dockerfile (BuildKit parser)
2. Build semantic model (fast)
3. Run fast rules (fast)
4. **Plan async checks** (fast; just creates requests)
5. If enabled: **execute async checks** (slow; concurrent)
6. Merge async violations into the final list
7. Report results

Fail-fast (default): if fast rules already produce any `SeverityError` violations for a file (after config overrides), skip/cancel async checks for
that file and report results immediately. This can be disabled to always run slow checks.

Key constraint: **planning must not perform I/O**. All I/O happens in step 5 under budget control.

### 5.2 Async checks API (proposed)

Add an optional interface for rules:

```go
// In internal/rules (conceptual)
type AsyncRule interface {
    rules.Rule
    PlanAsync(input rules.LintInput) []async.CheckRequest
}
```

Note: the async pipeline will **reuse BuildKit-native types** instead of creating parallel ones (ranges, positions, AST nodes, lint outputs).
Concretely, `PlanAsync` / `CheckRequest` / resolvers / `OnSuccess` should produce and consume types from `github.com/moby/buildkit` where applicable,
such as:

- `parser.Range` / `parser.Position` (`github.com/moby/buildkit/frontend/dockerfile/parser`)
- `instructions.Stage` / `instructions.Command` (`github.com/moby/buildkit/frontend/dockerfile/instructions`)
- `lint.Warning` / `lint.LintResults` (`github.com/moby/buildkit/frontend/subrequests/lint`)

`[]rules.Violation` remains the reporting boundary; conversion to it should happen only once (not via duplicated “Range/Stage/Command/etc.” types).

`PlanAsync` returns one or more `CheckRequest` objects. Each request declares:

- **Category**: `network | filesystem | console`
- **Key**: fully-specific, collision-free cache key (safe dedupe within the run). Key must encode **all inputs that affect resolution** so
  deduping by `(ResolverID, Key)` is correct; at minimum include `ref`, normalized platform (OS/arch/variant), and any resolver-specific options
  (auth/config paths, transport, flags). Key must be unique per distinct resolution context to prevent cross-request reuse when `OnSuccess`/resolvers
  convert resolved data into `[]rules.Violation`.
- **Timeout / cost**: per-request budget hints
- **ResolverID + data**: routes to a resolver implementation
- **OnSuccess**: converts resolved BuildKit-native structs (e.g., `lint.Warning`/`lint.LintResults` + `[]parser.Range`) into `[]rules.Violation`

MVP keeps this minimal: a request maps to “run resolver → return violations”.

### 5.3 Async runtime (proposed)

Add a runtime that:

- accepts `[]CheckRequest`
- deduplicates by `(ResolverID, Key)`
- executes in concurrency-limited worker pools per category
- applies per-request and global timeouts
- collects:
  - `[]rules.Violation` results
  - `[]async.Skipped` (disabled, fail-fast, auth, not-found, network, timeout, resolver error)

This runtime should be usable later by **async fixes** too (shared budgets and caches), but MVP can be check-only.

### 5.4 Concurrency model and budgets

Use three pools to reflect the user-provided slow operation taxonomy:

- `network`: small concurrency (e.g., 4) + tighter timeout
- `filesystem`: moderate concurrency (e.g., `GOMAXPROCS`) + longer timeout
- `console`: single-threaded (concurrency = 1) + explicit opt-in (often interactive/expensive)

Provide:

- Global timeout for all async work per file/run (e.g., 10s/30s/60s): **wall-clock** budget (includes queue wait + execution) by default. Controlled
  by `slow-checks.timeout-mode = "include-queue" | "execution-only"`. Example: with `slow-checks.timeout=30s` and `slow-checks.network.concurrency=4`,
  ~12 network requests that each take ~10s can complete (3 waves); additional queued requests are skipped/canceled when the 30s deadline is reached.
- Per-request timeout override (e.g., registry resolve 5s): uses the same `slow-checks.timeout-mode` semantics. With `include-queue`, the per-request
  timeout starts when the request is enqueued (so requests that cannot start in time are skipped without running). With `execution-only`, the
  per-request timeout starts when the worker begins executing the resolver.

## 6. Configuration & UX

### 6.1 Config file

Add a top-level section to `tally.toml`:

```toml
[slow-checks]
mode = "auto"  # auto|on|off
fail-fast = true # skip/cancel async checks when fast checks already have SeverityError
timeout-mode = "include-queue" # include-queue|execution-only (queue wait semantics for timeouts)
timeout = "20s"

[slow-checks.network]
enabled = true
concurrency = 4
timeout = "10s"

[slow-checks.filesystem]
enabled = true
concurrency = 8
timeout = "30s"

[slow-checks.console]
enabled = false
concurrency = 1
timeout = "90s"
```

Notes:

- `mode=auto` uses CI detection to decide the default.
- Category knobs allow us to enable only the necessary subset (e.g., allow filesystem context checks but keep network off).

### 6.2 CLI

Add CLI flags to override config:

- `--slow-checks=auto|on|off`
- `--slow-checks-fail-fast=on|off`
- `--slow-checks-timeout=20s`
- `--slow-checks-network=on|off`
- (later) `--slow-checks-concurrency-network=4` etc.

MVP can start with only `--slow-checks`, `--slow-checks-fail-fast`, and `--slow-checks-timeout`.

### 6.3 CI auto-disable

Default behavior:

- Local machine: `slow-checks.mode=auto` → **enabled**
- CI detected: `slow-checks.mode=auto` → **disabled**

Use `github.com/gkampitakis/ciinfo` for detection:

- `ciinfo.IsCI` → treat as CI
- Optional: use `ciinfo.Name` to tailor messaging (GitHub Actions vs Buildkite etc.)

This is a “safe default” to avoid CI flakiness and credential prompts.

### 6.4 Reporting skipped async checks

MVP: do not fail the run if async checks are skipped.

Add (in verbose mode, or JSON metadata later):

- number of async requests planned/executed
- number skipped by reason: disabled / fail-fast / auth / not-found / network / timeout / resolver error

This provides observability without making CI brittle.

### 6.5 LSP / Editor integration (on/off + progress)

The LSP server should keep diagnostics **responsive** while still allowing async checks when appropriate.

#### Control: enabling/disabling from the editor

The LSP server already supports **workspace configuration overrides** (via `workspace/didChangeConfiguration`) and merges them with discovered
`.tally.toml` / `tally.toml`.

We should expose the slow-checks knobs to the editor settings UI and pass them as configuration overrides:

- `slow-checks.mode=auto|on|off`
- per-category toggles (initially: network on/off)
- timeouts (optionally a shorter “editor default” timeout)

This allows:

- enabling slow checks locally while keeping CI defaults unchanged (CI auto-disable still applies when `mode=auto`)
- per-workspace control (some repos want registry checks, others do not)

#### When to run slow checks in LSP

Running network-backed checks on every keystroke is undesirable. A simple MVP strategy:

- **On change** (`didChange`, pull-diagnostics requests): run **fast checks only**.
- **On save** (`didSave`): if slow checks are enabled, run async checks with a budget (timeouts + concurrency), then refresh diagnostics.

If we later want “background while idle”, we can add a debounce (e.g., start after 1–2s of no edits) and cancel on the next edit.

#### Progress + cancellation

When async checks run (typically on save), the LSP server should:

1. create a work-done progress token (if the client supports it)
2. send progress updates (planned / running / completed counts)
3. respect request cancellation and prevent publishing stale results:
   - capture the document `version` (or a monotonic `nonce`) when async checks start and propagate it into the async run context (so async checks,
     work-done progress, and diagnostics publishing are all version/nonce-aware)
   - if a new save happens or the document version/nonce changes, cancel the in-flight async run
   - if fail-fast triggers (fast diagnostics include `SeverityError`), cancel the in-flight async run and publish fast diagnostics immediately
   - `PublishDiagnostics` (or the equivalent diagnostics sender) must verify the async run's version/nonce still matches the latest known document
     version/nonce before sending; otherwise skip publishing (also applies to progress updates)
   - network/file operations should receive the same `context.Context` (cancellation + version/nonce)

Diagnostics update behavior depends on diagnostic mode:

- **Push** mode: publish fast diagnostics immediately; publish an updated set when async checks complete.
- **Pull** mode: return fast diagnostics; when async checks complete, trigger `workspace/diagnostic/refresh` so the client re-pulls.

This gives the user quick feedback plus richer results when the slow work finishes, without blocking typing.

## 7. Registry Integration (Network Category)

### 7.1 Why `github.com/containers/image/v5`

We want:

- registry access without requiring a Docker daemon
- support for **buildah/podman** registry config overrides and auth handling
- consistent behavior across environments where users already have `registries.conf`, `containers-auth.json`, etc.

`containers/image/v5` provides:

- `types.SystemContext` to control config paths and auth
- transports for `docker://`, `oci:`, etc.

### 7.2 Proposed resolver API

Introduce a small internal abstraction (conceptual):

```go
type ImageResolver interface {
    // ResolveConfig resolves image config (env + resolved digest/platform) for the requested image ref and platform.
    //
    // Error contract (callers must branch via errors.As/errors.Is to decide retry vs permanent failure vs reporting):
    //   - AuthError: authentication required/failed (401/403, missing creds, expired token). Retryable only after credential refresh.
    //   - NetworkError: transient network failure. Retryable with backoff until the request/global deadline.
    //   - TimeoutError (or errors.Is(err, context.DeadlineExceeded)): resolver timed out. Treat like NetworkError (usually fewer retries).
    //   - NotFoundError: ref/tag/manifest not found. Permanent; do not retry.
    //   - PlatformMismatchError: image exists but no manifest matches requested platform. Permanent; caller should report
    //     `buildkit/InvalidBaseImagePlatform` (not as a skipped check).
    //
    // Implementations should wrap the underlying error via Unwrap() so logs/debugging retain the root cause.
    ResolveConfig(ctx context.Context, ref string, platform string) (ImageConfig, error)
}

// Error categories (conceptual). Implementations may use typed errors or a single error type with a Code/Kind field.
type AuthError struct{ Err error }
type NetworkError struct{ Err error }  // includes non-timeout transient errors
type TimeoutError struct{ Err error }  // or expose via net.Error.Timeout()
type NotFoundError struct{ Ref string; Err error }
type PlatformMismatchError struct {
    Ref       string
    Requested string   // normalized platform
    Available []string // normalized platforms, if known
    Err       error
}

type ImageConfig struct {
    Env      map[string]string // from config.Env KEY=VALUE
    OS       string
    Arch     string
    Variant  string
    Digest   string            // resolved digest, if available
}
```

The `platform` input should be normalized (`linux/amd64[/variant]`). In MVP we can:

- use stage’s `FROM --platform` (after ARG expansion) if present and resolvable
- if stage `FROM --platform` is absent or unresolvable, tally will default to `runtime.GOOS/runtime.GOARCH` of the running `tally` process unless a
  user-configurable `TARGETPLATFORM` override is set; this aligns with BuildKit’s `TARGETPLATFORM` semantics by treating the configured
  `TARGETPLATFORM` as the canonical override and falling back to the tool’s host platform

Callers of `ResolveConfig` (i.e., the network resolver that services async `CheckRequest`s) must classify these error categories to drive behavior:

- **Credential refresh**: on `AuthError`, reload registry auth/config (or `types.SystemContext`) and retry once; if still failing, mark skipped as
  `auth`.
- **Backoff retries**: on `NetworkError`/`TimeoutError`, apply bounded retries with jitter until the request/global deadline; if exhausted, mark
  skipped as `network` or `timeout` (depending on classification).
- **Permanent failures**: on `NotFoundError`, do not retry; mark skipped as `not-found`.
- **Platform mismatch reporting**: on `PlatformMismatchError`, do not retry; emit the `buildkit/InvalidBaseImagePlatform` violation using the error’s
  requested/available platform details.

### 7.3 Caching & dedupe

In-run cache:

- key by `(ref, platform)` and by resolved digest when available
- store:
  - resolved config env map
  - resolved platform
  - errors with short TTL (optional in MVP)

This prevents N stages referencing the same base image from multiplying network calls.

## 8. First Async Checks (MVP Scope)

### 8.1 `buildkit/UndefinedVar` (enhanced)

Current fast implementation uses a semantic env approximation (e.g., always seed `PATH` for external images) and stage-to-stage env inheritance.

Async enhancement when network checks are enabled:

1. For each stage with an **external base image**, resolve base image config env via registry.
2. Seed the stage’s initial env with those values (instead of the approximation).
3. Run the same semantic undefined-var analysis (order-sensitive) to produce final violations.

Behavior when slow checks are disabled / resolution fails:

- fall back to fast approximation mode
- optionally record “async enhancement skipped” in verbose output

### 8.2 `buildkit/InvalidBaseImagePlatform`

Async-only rule:

1. Determine expected platform for each stage:
   - from `FROM --platform=...` after ARG expansion, or
   - if absent/unresolvable: default to `runtime.GOOS/runtime.GOARCH` of the running `tally` process unless a user-configurable `TARGETPLATFORM`
     override is set (treat the configured `TARGETPLATFORM` as canonical, host platform as fallback)
2. Resolve base image platform from registry:
   - for a manifest list, select the matching platform entry
   - for a single manifest, read config.OS/Arch/Variant
3. Compare resolved base image platform to expected platform.
4. Emit BuildKit-compatible message/doc URL.

Behavior when slow checks are disabled / resolution fails:

- no violations emitted (rule effectively becomes a no-op)
- record skip diagnostics in verbose output (optional in MVP)

## 9. Testing Strategy

### 9.1 Deterministic mock registry

Use `github.com/google/go-containerregistry` to stand up a local `mockregistry`:

- deterministic image config with fixed `Env` (e.g., `PYTHON_VERSION=...`)
- deterministic multi-arch index for platform mismatch cases

Tests should:

- run without external network
- avoid real credentials
- assert stable behavior across platforms

### 9.2 Production vs tests implementation mismatch

Production uses `containers/image/v5`.

Tests will still use a real HTTP registry (mockregistry), so we test the full HTTP integration path, even if the image construction uses
go-containerregistry helpers.

If we find incompatibilities, define an internal `ImageResolver` interface and:

- keep `containers/image` as the default implementation
- allow swapping a fake resolver in unit tests
- keep at least one end-to-end test that exercises `containers/image` against `mockregistry`

## 10. Rollout Plan (Feasible MVP)

1. **Config + CLI plumbing**:
   - add `slow-checks.mode` and `--slow-checks`
   - CI detection via `ciinfo`
2. **Async runtime skeleton**:
   - request planning + per-category worker pools
   - in-run dedupe + timeouts
3. **Registry resolver (containers/image)**:
   - resolve image config env + platform for `docker://` refs
   - respect buildah/podman config overrides via `SystemContext`
4. **Rules**:
   - upgrade `buildkit/UndefinedVar` to optionally use resolved base image env
   - implement `buildkit/InvalidBaseImagePlatform` behind network slow-checks
5. **Tests**:
   - mockregistry-based deterministic tests
   - integration test fixture(s) for both rules with slow-checks on/off

## 11. Future Work (After MVP)

- Filesystem async checks:
  - scan build context under `.dockerignore`
  - local file checksums for `COPY --checksum` (local paths)
- Network async checks:
  - remote HTTP checksum verification
  - OCI referrers/attestations (ties into `tally/prefer-vex-attestation`)
  - persistent cache to disk (optional)
- Console async checks:
  - unify “async checks” runtime with “async fix” resolvers (AI ACP)
  - explicit interactive opt-in (`--slow-checks-console=on`)
