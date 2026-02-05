# BuildKit Phase 2 Rules Integration Research

This document summarizes research into integrating BuildKit's native Phase 2 lint rules into tally, rather than reimplementing them.

## Background

BuildKit's Dockerfile linter operates in two phases:

- **Phase 1**: Rules triggered during `instructions.Parse()` - syntax and basic semantic checks
- **Phase 2**: Rules triggered during `dockerfile2llb.Dockerfile2LLB()` - context-aware and build-time checks

Tally currently integrates Phase 1 rules via the existing parser, which captures 20+ warnings automatically. The question is whether to use BuildKit's
native Phase 2 rules or reimplement them.

## BuildKit Linting Architecture

### Phase 1 Flow (Currently Implemented)

```text
parser.Parse(dockerfile)
    ↓
instructions.Parse(ast, linter)  ← Triggers Phase 1 rules
    ↓
Warnings captured via linter.LintWarnFunc callback
```

Phase 1 rules include:

- `StageNameCasing` - Stage names should be lowercase
- `FromAsCasing` - AS keyword casing
- `DuplicateStageName` - Unique stage names
- `MaintainerDeprecated` - MAINTAINER is obsolete
- `UndefinedArgInFrom` - ARG references in FROM
- `JSONArgsRecommended` - JSON format for CMD/ENTRYPOINT
- ... and 14+ more

### Phase 2 Flow (Not Yet Integrated)

```text
dockerfile2llb.DockerfileLint(ctx, dt, opt)
    ↓
toDispatchState(ctx, dt, opt)  ← Internal, not exported
    ↓
dispatchEnv/dispatchArg/dispatchWorkdir/etc.  ← Triggers Phase 2 rules
    ↓
Warnings captured via opt.Warn callback
```

Phase 2 rules include:

- `CopyIgnoredFile` - COPY/ADD sources match .dockerignore
- `InvalidBaseImagePlatform` - Base image platform mismatch
- `WorkdirRelativePath` - Relative WORKDIR without base
- `SecretsUsedInArgOrEnv` - Secrets exposed in ARG/ENV
- `RedundantTargetPlatform` - Unnecessary $TARGETPLATFORM

## Research Findings

### DockerfileLint API

```go
func DockerfileLint(ctx context.Context, dt []byte, opt ConvertOpt) (*lint.LintResults, error)

type ConvertOpt struct {
    dockerui.Config                    // Build configuration
    Client         *dockerui.Client   // BuildKit client (for .dockerignore)
    MainContext    *llb.State         // LLB context state
    SourceMap      *llb.SourceMap     // Source location mapping
    TargetPlatform *ocispecs.Platform // Target build platform
    MetaResolver   llb.ImageMetaResolver // Image metadata resolver
    LLBCaps        *apicaps.CapSet    // LLB capabilities
    Warn           linter.LintWarnFunc // Warning callback
    AllStages      bool               // Lint all stages
}
```

### Dependency Analysis per Rule

| Rule | Required Dependencies | Notes |
|------|----------------------|-------|
| `CopyIgnoredFile` | `Client.DockerIgnorePatterns()` | Needs .dockerignore patterns from build context |
| `InvalidBaseImagePlatform` | `MetaResolver` | Needs to resolve actual image platform from registry |
| `WorkdirRelativePath` | None (static analysis) | Checks if WORKDIR is relative without prior absolute |
| `SecretsUsedInArgOrEnv` | None (static analysis) | Pattern matching on ARG/ENV keys |
| `RedundantTargetPlatform` | None (static analysis) | Checks $TARGETPLATFORM usage in FROM --platform |

### Key Discovery: Infrastructure Requirements

Attempting to call `DockerfileLint` with minimal options fails:

```go
// This panics with nil pointer dereference
opt := dockerfile2llb.ConvertOpt{
    Warn:      warnFunc,
    SourceMap: llb.NewSourceMap(&llb.Scratch(), "Dockerfile", "", content),
}
results, err := dockerfile2llb.DockerfileLint(ctx, content, opt)
```

**Root cause**: `DockerfileLint` immediately calls `results.AddSource(opt.SourceMap)` which tries to marshal the LLB state via `Definition.ToPB()`. A
scratch state has no marshaled definition.

### How BuildKit Frontend Uses DockerfileLint

In the actual BuildKit frontend (`frontend/dockerfile/builder/build.go`):

```go
// SourceMap comes from the gateway client's file read
src, err := bc.ReadEntrypoint(ctx, "dockerfile")

// MetaResolver is the gateway client itself
opt.MetaResolver = c  // gateway.Client

// Client provides .dockerignore patterns
opt.Client = bc  // dockerui.Client wrapping gateway

Lint: func(ctx context.Context) (*lint.LintResults, error) {
    return dockerfile2llb.DockerfileLint(ctx, src.Data, convertOpt)
}
```

The infrastructure assumes a full BuildKit gateway connection with:

- Access to build context files (.dockerignore)
- Registry access for image metadata
- Proper LLB state management

## Challenges

### 1. SourceMap Requires Marshaled LLB State

The `SourceMap` structure expects a properly marshaled LLB `Definition`:

```go
// From lint.go:80
func (results *LintResults) AddSource(sourceMap *llb.SourceMap) int {
    def := sourceMap.Definition.ToPB()  // Panics if Definition is nil/empty
    // ...
}
```

Creating a valid Definition requires building actual LLB operations, not just a scratch state.

### 2. Client is a Struct, Not Interface

```go
type Client struct {
    Config
    // Has unexported fields.
}

func NewClient(c client.Client) (*Client, error)
```

`dockerui.Client` requires a `client.Client` (gateway client) to be instantiated. We cannot easily mock it for `.dockerignore` patterns.

### 3. MetaResolver Requires Registry Access

```go
type ImageMetaResolver interface {
    ResolveImageConfig(ctx context.Context, ref string, opt ResolveImageConfigOpt) (string, digest.Digest, []byte, error)
}
```

The default resolver (`imagemetaresolver.Default()`) attempts to pull image metadata from registries, which requires network access and potentially
authentication.

### 4. Internal Functions Not Exported

The actual dispatch functions that trigger Phase 2 rules are internal:

```go
// These are not exported
func toDispatchState(ctx context.Context, dt []byte, opt ConvertOpt) (*dispatchState, error)
func dispatchEnv(d *dispatchState, c *instructions.EnvCommand, lint *linter.Linter) error
func dispatchWorkdir(d *dispatchState, c *instructions.WorkdirCommand, commit bool, opt *dispatchOpt) error
```

## Proposed Solutions

### Option 1: Full LLB Infrastructure (Heavy)

**Approach**: Set up the complete infrastructure required by `DockerfileLint`.

**Implementation**:

1. Create a mock `gateway.Client` that provides:
   - File system access for .dockerignore
   - Stub responses for image metadata
2. Build proper LLB states for SourceMap
3. Implement `ImageMetaResolver` mock for platform detection

**Pros**:

- Uses BuildKit's exact rule implementations
- Automatic updates when BuildKit adds new rules
- Guaranteed behavior parity with `docker buildx build --check`

**Cons**:

- Significant new dependencies (containerd, grpc, etc.)
- Complex mock implementations required
- High maintenance burden for mocks
- Binary size increase

**Estimated Effort**: High (2-3 weeks)

### Option 2: Fork/Extract BuildKit Code (Medium)

**Approach**: Copy the relevant dispatch functions from BuildKit and adapt them.

**Implementation**:

1. Extract `toDispatchState` and related dispatch functions
2. Remove LLB generation code, keep only validation logic
3. Adapt to work with our parsed instructions
4. Maintain as internal fork

**Pros**:

- Full control over behavior
- No runtime dependencies on BuildKit infrastructure
- Can optimize for lint-only use case

**Cons**:

- Maintenance burden to track BuildKit changes
- Risk of divergence from upstream behavior
- Licensing considerations (Apache 2.0, compatible)

**Estimated Effort**: Medium (1-2 weeks initial, ongoing maintenance)

### Option 3: Selective Reimplementation (Current/Pragmatic)

**Approach**: Implement needed rules ourselves, skip infrastructure-dependent ones.

**Implementation**:

1. Keep `CopyIgnoredFile` (already implemented)
2. Implement `WorkdirRelativePath` - track WORKDIR state per stage
3. Implement `SecretsUsedInArgOrEnv` - pattern match on ARG/ENV keys
4. Implement `RedundantTargetPlatform` - check $TARGETPLATFORM in FROM
5. Skip `InvalidBaseImagePlatform` - requires registry access

**Pros**:

- Minimal dependencies
- Full control and understanding
- Can tailor to our specific needs

**Cons**:

- Must track BuildKit rule changes manually
- Risk of behavior differences
- Duplicated effort

**Estimated Effort**: Low-Medium (3-5 days per rule)

### Option 4: Hybrid with Upstream Contribution

**Approach**: Contribute to BuildKit to make linting more standalone-friendly.

**Implementation**:

1. Propose upstream changes to decouple linting from LLB
2. Add optional `DockerIgnorePatterns []string` to `ConvertOpt`
3. Make `SourceMap` optional for lint-only mode
4. Create standalone `LintOnly()` function

**Pros**:

- Benefits entire community
- Reduces our maintenance burden long-term
- Aligns with BuildKit's direction

**Cons**:

- Uncertain timeline for upstream acceptance
- Need to maintain fork until merged
- May not align with BuildKit maintainers' vision

**Estimated Effort**: High (uncertain timeline)

## Recommendation

Given the constraints and goals:

**Short-term (v1.0)**: Option 3 - Selective Reimplementation

- Implement `WorkdirRelativePath`, `SecretsUsedInArgOrEnv`, `RedundantTargetPlatform`
- Keep existing `CopyIgnoredFile` implementation
- Skip `InvalidBaseImagePlatform` (requires registry access)
- Document which rules are supported vs. BuildKit

**Medium-term (v1.x)**: Option 4 - Upstream Contribution

- Propose standalone linting API to BuildKit
- If accepted, migrate to using upstream
- If rejected, evaluate Option 2 for full parity

## Appendix: Rule Implementation Details

### WorkdirRelativePath

**Trigger condition**:

```go
if commit && !d.workdirSet && !system.IsAbs(c.Path, d.platform.OS) {
    // Warn: relative WORKDIR without prior absolute WORKDIR
}
```

**Our implementation approach**:

- Track `workdirSet` boolean per stage
- On WORKDIR instruction, check if path is absolute
- If relative and `workdirSet` is false, emit warning

### SecretsUsedInArgOrEnv

**Trigger condition**:

```go
func validateNoSecretKey(instruction, key string, location []parser.Range, lint *linter.Linter) {
    if isSecretKey(key) {
        msg := linter.RuleSecretsUsedInArgOrEnv.Format(instruction, key)
        lint.Run(&linter.RuleSecretsUsedInArgOrEnv, location, msg)
    }
}
```

**Secret key patterns** (from BuildKit source):

- Contains "SECRET", "PASSWORD", "TOKEN", "KEY", "CREDENTIAL"
- Case-insensitive matching

**Our implementation approach**:

- Pattern match ARG/ENV keys against known secret patterns
- Emit warning on match

### RedundantTargetPlatform

**Trigger condition**:

```go
// In FROM --platform=$TARGETPLATFORM
if len(nameMatch.Matched) == 1 && nameMatch.Matched["TARGETPLATFORM"] != "" {
    if nameMatch.Result == env.Get("TARGETPLATFORM") {
        // Warn: redundant use of TARGETPLATFORM
    }
}
```

**Our implementation approach**:

- Parse FROM --platform value
- Check if it resolves to just $TARGETPLATFORM
- Emit warning if redundant

## References

- BuildKit source: <https://github.com/moby/buildkit>
- Key files:
  - `frontend/dockerfile/dockerfile2llb/convert.go` - Main conversion and linting
  - `frontend/dockerfile/linter/ruleset.go` - Rule definitions
  - `frontend/dockerfile/builder/build.go` - Frontend integration
  - `frontend/subrequests/lint/lint.go` - Lint results structure
