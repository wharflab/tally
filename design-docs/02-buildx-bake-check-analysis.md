# Docker Buildx Bake --check Implementation Analysis

**Research Focus:** Understanding Docker's official Dockerfile linting implementation

Source: `github.com/docker/buildx` and `github.com/moby/buildkit`

---

## Overview

Docker buildx bake `--check` validates Dockerfile configurations without executing builds. It leverages BuildKit's frontend capabilities to run lint
checks as a subrequest.

**Version:** Docker Buildx 0.15.0+, Dockerfile syntax 1.8+

---

## Complete Rule List

BuildKit implements **22 standard rules** and **1 experimental rule**.

### Standard Rules (Active by Default)

| Rule Code | Name | Description |
|-----------|------|-------------|
| **Stage Management** |  |  |
| `StageNameCasing` | Stage name casing | Stage names should be lowercase |
| `FromAsCasing` | FROM AS casing | The 'as' keyword should match 'from' case |
| `DuplicateStageName` | Duplicate stages | Stage names must be unique |
| `ReservedStageName` | Reserved names | Reserved words cannot be stage names |
| **Syntax & Style** |  |  |
| `NoEmptyContinuation` | Empty continuation | Empty continuation lines are errors |
| `ConsistentInstructionCasing` | Instruction casing | Commands should use consistent case |
| `JSONArgsRecommended` | JSON args format | Use JSON for ENTRYPOINT/CMD |
| `LegacyKeyValueFormat` | Legacy format | Avoid whitespace-separated key=value |
| **Arguments & Variables** |  |  |
| `UndefinedArgInFrom` | Undefined ARG | FROM must use declared ARGs |
| `UndefinedVar` | Undefined variable | Variables must be defined before use |
| `InvalidDefaultArgInFrom` | Invalid default | Default ARG value creates invalid base |
| **Platform & Architecture** |  |  |
| `InvalidBaseImagePlatform` | Platform mismatch | Base platform doesn't match target |
| `RedundantTargetPlatform` | Redundant platform | FROM --platform=$TARGETPLATFORM is redundant |
| `FromPlatformFlagConstDisallowed` | Constant platform | FROM --platform shouldn't use constant |
| **Security** |  |  |
| `SecretsUsedInArgOrEnv` | Secrets in ARG/ENV | Sensitive data shouldn't be in ARG/ENV |
| **File Operations** |  |  |
| `CopyIgnoredFile` | Copy ignored file | COPY target excluded by .dockerignore |
| `WorkdirRelativePath` | Relative WORKDIR | Relative path without absolute base |
| **Network & Exposure** |  |  |
| `ExposeProtoCasing` | EXPOSE protocol case | Protocol should be lowercase |
| `ExposeInvalidFormat` | EXPOSE format | No IP/host-port in EXPOSE |
| **Deprecated Features** |  |  |
| `MaintainerDeprecated` | MAINTAINER deprecated | Use LABEL instead |
| `MultipleInstructionsDisallowed` | Multiple instructions | Multiple same instructions in stage |

### Experimental Rules (Opt-in)

| Rule Code | Name | Description |
|-----------|------|-------------|
| `InvalidDefinitionDescription` | Definition comments | Build stage/arg comments must follow format |

---

## Architecture

### Request Flow

```text
┌──────────────────────┐
│ buildx bake --check  │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────────────────┐
│ Build Request                    │
│ - CallFunc: "check"              │
│ - Targets: [dockerfile paths]    │
└──────────┬───────────────────────┘
           │
           ▼
┌──────────────────────────────────┐
│ BuildKit Frontend                │
│ - Parse Dockerfile               │
│ - Execute lint subrequest        │
└──────────┬───────────────────────┘
           │
           ▼
┌──────────────────────────────────┐
│ Linter (moby/buildkit)           │
│ - Load lint config               │
│ - Run enabled rules              │
│ - Collect warnings               │
└──────────┬───────────────────────┘
           │
           ▼
┌──────────────────────────────────┐
│ LintResults                      │
│ - Warnings[]                     │
│ - Sources[]                      │
│ - BuildError (optional)          │
└──────────┬───────────────────────┘
           │
           ▼
┌──────────────────────────────────┐
│ buildx printResult()             │
│ - Format output (text/JSON)      │
│ - Map source locations           │
│ - Return exit code               │
└──────────────────────────────────┘
```

### Key Components

#### 1. Subrequest Protocol

**Location:** `moby/buildkit/frontend/gateway/grpcclient/client.go`

BuildKit uses a subrequest system for lint checks:

```go
const RequestLint = "frontend.lint"  // Version 1.0.0

type LintResults struct {
    Warnings   []LintWarning
    Sources    []SourceLocation
    BuildError *BuildError
}

type LintWarning struct {
    RuleName    string
    Description string
    URL         string          // Documentation link
    Detail      string
    Location    SourceLocation
    Level       int             // 0=warning, 1=error
}

type SourceLocation struct {
    SourceIndex int
    Ranges      []Range
}

type Range struct {
    Start Position  // {Line, Character}
    End   Position
}
```

**Returned metadata:**

- `result.json` - Structured LintResults
- `result.txt` - Plain text format
- `result.statuscode` - Exit code (0 or 1)

#### 2. Linter Configuration

**Location:** `moby/buildkit/frontend/dockerfile/linter/linter.go`

```go
type Config struct {
    SkipRules       []string  // Rules to disable
    EnabledRules    []string  // Experimental rules to enable
    WarnOnUnknown   bool      // Warn on unrecognized rules
    Error           bool      // Treat warnings as build errors
}
```

**Configuration sources:**

1. **Dockerfile directive** (highest priority):

   ```dockerfile
   # check=skip=StageNameCasing,JSONArgsRecommended
   # check=experimental=InvalidDefinitionDescription
   # check=error=true
   ```

2. **Build argument**:

   ```bash
   docker buildx build --build-arg BUILDKIT_DOCKERFILE_CHECK="skip=all"
   ```

3. **BuildKit config** (in buildkitd.toml)

**Special values:**

- `skip=all` - Disable all rules
- `experimental=all` - Enable all experimental rules

#### 3. Rule Definition Pattern

**Location:** `moby/buildkit/frontend/dockerfile/linter/ruleset.go`

Each rule uses a generic `LinterRule[F]` struct:

```go
type LinterRule[F any] struct {
    Name         string
    Description  string
    URL          string
    Format       F              // Formatting function for warnings
    Deprecated   bool
    Experimental bool
}

// Example rule
var RuleStageNameCasing = LinterRule[func(string) string]{
    Name:        "StageNameCasing",
    Description: "Stage names should be lowercase",
    URL:         "https://docs.docker.com/go/dockerfile/rule/stage-name-casing/",
    Format: func(stageName string) string {
        return fmt.Sprintf("Stage name '%s' should be lowercase", stageName)
    },
}
```

**Rule execution:**

- Rules run during AST traversal
- Each rule function takes AST node and returns warnings
- Warnings include source location with precise line/column ranges
- No shared state between rules (pure functions)

#### 4. Output Formatting

**Location:** `docker/buildx/commands/build.go`

**Text format (default):**

```text
WARNING: StageNameCasing - https://docs.docker.com/go/dockerfile/rule/stage-name-casing/
Stage name 'MyStage' should be lowercase

Dockerfile:3
--------------------
   1 | FROM ubuntu:latest
   2 |
   3 | >>> FROM alpine:latest AS MyStage
   4 | RUN apk add curl
--------------------
```

Features:

- Colored output (red for errors, yellow for warnings)
- Source code snippet with context lines
- Highlighted problematic line (>>> prefix)
- Line numbers for navigation
- Clickable documentation URL

**JSON format:**

```json
{
  "warnings": [
    {
      "ruleName": "StageNameCasing",
      "description": "Stage names should be lowercase",
      "url": "https://docs.docker.com/go/dockerfile/rule/stage-name-casing/",
      "detail": "Stage name 'MyStage' should be lowercase",
      "location": {
        "sourceIndex": 0,
        "ranges": [
          {
            "start": {"line": 3, "character": 0},
            "end": {"line": 3, "character": 31}
          }
        ]
      },
      "level": 0
    }
  ],
  "sources": [
    {
      "filename": "Dockerfile",
      "language": "dockerfile",
      "data": "RlJPTSB1YnVudHU6bGF0ZXN0...",  // base64-encoded
      "definition": [
        {"line": 1, "column": 0},
        {"line": 4, "column": 20}
      ]
    }
  ]
}
```

---

## Integration Patterns

### 1. Parser Integration

BuildKit's linter uses the official Dockerfile parser:

```go
// Parse Dockerfile
result, err := parser.Parse(reader)
if err != nil {
    return nil, err
}

// Walk AST
for _, child := range result.AST.Children {
    switch child.Value {
    case "from":
        checkFromInstruction(child, lintCtx)
    case "run":
        checkRunInstruction(child, lintCtx)
    // ... etc
    }
}
```

**AST structure:**

- `Node.Value` - Instruction name (lowercase: "from", "run", etc.)
- `Node.Next` - Linked list of arguments
- `Node.Children` - Nested nodes (for multi-line instructions)
- `Node.StartLine/EndLine` - Source locations
- `Node.Heredocs` - Attached heredoc content

### 2. Two-Phase Linting Architecture

**Critical insight:** BuildKit's linting happens in **two distinct phases**:

#### Phase 1: Instruction Parsing (`instructions.Parse`)

Most rules (20 of 22) run during `instructions.Parse(ast, linter)`:

```go
// Parse Dockerfile and run syntax/semantic rules
lint := linter.New(&linter.Config{Warn: warnFunc})
stages, metaArgs, err := instructions.Parse(ast.AST, lint)
```

**Rules triggered in Phase 1:**

- StageNameCasing, FromAsCasing, DuplicateStageName, ReservedStageName
- ConsistentInstructionCasing, NoEmptyContinuation, JSONArgsRecommended
- MaintainerDeprecated, LegacyKeyValueFormat, MultipleInstructionsDisallowed
- UndefinedArgInFrom, UndefinedVar, InvalidDefaultArgInFrom
- WorkdirRelativePath, SecretsUsedInArgOrEnv
- RedundantTargetPlatform, FromPlatformFlagConstDisallowed
- ExposeProtoCasing, ExposeInvalidFormat
- InvalidDefinitionDescription (experimental)

#### Phase 2: LLB Conversion (`dockerfile2llb.Dockerfile2LLB`)

**Context-aware rules** run during LLB conversion, which requires build context:

```go
// In dockerfile2llb/convert.go
dockerIgnorePatterns, err := opt.Client.DockerIgnorePatterns(ctx)
dockerIgnoreMatcher, err = patternmatcher.New(dockerIgnorePatterns)

// During COPY/ADD dispatch
func validateCopySourcePath(src string, cfg *copyConfig) error {
    if cfg.ignoreMatcher == nil {
        return nil  // No context = skip this rule
    }
    ok, err := cfg.ignoreMatcher.MatchesOrParentMatches(src)
    if ok {
        msg := linter.RuleCopyIgnoredFile.Format(cmd, src)
        cfg.opt.lint.Run(&linter.RuleCopyIgnoredFile, cfg.location, msg)
    }
    return nil
}
```

**Rules triggered in Phase 2 (context-aware):**

- **CopyIgnoredFile** - requires `.dockerignore` patterns from build context
- **InvalidBaseImagePlatform** - requires MetaResolver to pull image manifest

**Implication for tally:** To support `CopyIgnoredFile`, we have two options:

1. **Full LLB conversion** - Use BuildKit's `dockerfile2llb.Dockerfile2LLB()` with mocked client
2. **Own implementation** - Parse `.dockerignore` ourselves using `moby/patternmatcher` and check COPY/ADD sources

Option 2 is lighter and aligns with our context-aware foundation (Priority 6).

**Platform validation (Phase 2):**

```go
// InvalidBaseImagePlatform requires MetaResolver to fetch image manifest
// This happens during LLB conversion, not instruction parsing
func dispatchFrom(...) {
    // Platform check requires resolving base image metadata
    if d.platform != nil {
        img, err := metaResolver.ResolveImageConfig(ctx, d.image, ...)
        // Compare img.Platform with target platform
    }
}
```

### 3. Multi-Stage Analysis

The linter tracks stage definitions for cross-stage validation:

```go
type StageTracker struct {
    stages map[string]*StageInfo
}

type StageInfo struct {
    Name     string
    Line     int
    Platform string
}

// Check for duplicate stage names
func checkDuplicateStage(stageName string, tracker *StageTracker) []Warning {
    if existing, ok := tracker.stages[stageName]; ok {
        return []Warning{{
            RuleName: "DuplicateStageName",
            Detail: fmt.Sprintf("Stage '%s' already defined at line %d",
                               stageName, existing.Line),
        }}
    }
    tracker.stages[stageName] = &StageInfo{Name: stageName, ...}
    return nil
}
```

---

## Exit Codes & Error Handling

### Exit Codes

- **0** - No warnings/errors found
- **1** - Warnings/errors found (or build error)

### Error vs Warning Mode

Controlled by `error=true` config:

```dockerfile
# Treat all warnings as build-blocking errors
# check=error=true
FROM ubuntu:latest
```

**Behavior:**

- Without `error=true`: Warnings printed, exit 0 (success)
- With `error=true`: Warnings printed, exit 1 (failure)
- Always exit 1 if parse errors occur

---

## Lessons for Tally

### 1. Rule Organization ✅

**Pattern to adopt:**

```go
// pkg/rules/rules.go
type Rule struct {
    Code        string
    Name        string
    Description string
    URL         string
    Severity    Severity
    Check       func(*AST, *Config) []Violation
}

var AllRules = []Rule{
    StageNameCasing,
    JSONArgsRecommended,
    UndefinedVar,
    // ...
}
```

### 2. Configuration Flexibility ✅

Support multiple configuration methods:

1. Inline directives (`# tally ignore=...`)
2. Config file (`.tally.toml`)
3. CLI flags
4. Environment variables

### 3. Documentation-Driven Development ✅

Every rule should have:

- Unique code/identifier
- Clear description
- Documentation URL with:
  - Examples (good/bad)
  - Rationale
  - How to fix
  - When to ignore

### 4. Rich Diagnostics ✅

Provide:

- Precise source locations (line + column ranges)
- Code snippets with context
- Clickable documentation links
- Severity levels
- Actionable messages

### 5. Progressive Adoption ✅

Use experimental/opt-in rules for:

- Controversial checks
- Performance-intensive rules
- New/unproven rules
- Breaking changes

### 6. Integration Points 🔄

Plan for future integrations:

- BuildKit plugin system
- CI/CD platforms (GitHub Actions, GitLab CI)
- Editor extensions (LSP)
- Git hooks (pre-commit)

---

## Implementation Checklist

For Tally v1.0:

- [ ] Rule registry with metadata (code, name, description, URL)
- [ ] Severity levels (error, warning, info, style)
- [ ] Inline disable directives (`# tally ignore=RULE`)
- [ ] Config file support (skip/enable rules)
- [ ] `error` mode (warnings as errors)
- [ ] Source location tracking (line + column)
- [ ] Code snippet generation
- [ ] JSON output format
- [ ] Text output format (colored)
- [ ] Exit codes (0 = clean, 1 = violations)

For Tally v2.0+:

- [ ] Experimental rule system
- [ ] BuildKit integration (as subrequest)
- [ ] Context-aware rules (.dockerignore, build args)
- [ ] SARIF output format
- [ ] Rule documentation site
- [ ] Auto-fix capabilities
- [ ] Performance profiling

---

## Rule Implementation Priority

Based on BuildKit rules, suggested priority for Tally:

### Phase 1: Foundation (Must-have)

1. `StageNameCasing` - Easy to implement, widely applicable
2. `DuplicateStageName` - Critical for multi-stage builds
3. `UndefinedVar` - Common mistake, high value
4. `JSONArgsRecommended` - Best practice, security-relevant
5. `MaintainerDeprecated` - Simple, deprecated instruction

### Phase 2: Best Practices

6. `ConsistentInstructionCasing` - Style enforcement
7. `WorkdirRelativePath` - Common pitfall
8. `MultipleInstructionsDisallowed` - Logic error detection
9. `InvalidDefaultArgInFrom` - Prevents build failures

### Phase 3: Advanced

10. `CopyIgnoredFile` - Requires .dockerignore parsing
11. `InvalidBaseImagePlatform` - Requires platform tracking
12. `SecretsUsedInArgOrEnv` - Security-focused
13. `FromPlatformFlagConstDisallowed` - Multi-arch builds

---

## References

- BuildKit linter: `moby/buildkit/frontend/dockerfile/linter/`
- Buildx check command: `docker/buildx/commands/bake.go`
- Subrequest protocol: `moby/buildkit/frontend/gateway/`
- Rule documentation: <https://docs.docker.com/go/dockerfile/>
