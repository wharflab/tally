# 26. Windows Container Support

**Status:** Proposed
**Triggered by:** False positives observed on real-world Windows Dockerfiles
**Test fixtures:** `internal/integration/testdata/real-world-fix-ticketdesk/` (microsoft/windows-containers-demos),
`internal/integration/testdata/real-world-fix-metalama/` (metalama/Metalama)

---

## Executive Summary

Tally produces 8 false positives out of 14 violations on a real-world Windows IIS Dockerfile, and
applies a heredoc fix that would break the build. The root causes are:

1. No concept of "Windows container" in the semantic model
2. ShellCheck fires on `cmd.exe` / PowerShell commands (partially guarded — see findings below)
3. `buildkit/WorkdirRelativePath` doesn't recognize Windows absolute paths (`C:\`, `c:/`)
4. `tally/platform-mismatch` compares against the host machine instead of validating registry data
5. `tally/prefer-run-heredoc` suggests heredoc syntax that doesn't work on Windows
6. `tally/prefer-multi-stage-build` doesn't recognize Windows build tools
7. `tally/prefer-package-cache-mounts` doesn't recognize Windows package managers
8. Escape character (`# escape=` backtick) may not be respected by all rules

This document proposes a phased plan to fix these issues, anchored by a semantic model enhancement
that detects the base image OS and exposes the effective shell per stage.

---

## Current State: What Works

The codebase already has some Windows awareness:

**Shell variant detection** (`internal/shell/shell.go`):

- `VariantFromShell()` normalizes paths, strips `.exe`, and classifies shells
- `powershell`, `pwsh`, `cmd` → `VariantNonPOSIX`
- Handles Windows backslash paths in shell commands

**ShellCheck gating** (`internal/rules/shellcheck/shellcheck.go`):

- When shell variant is `VariantNonPOSIX`, ShellCheck is skipped entirely
- This works for stages with an explicit `SHELL ["powershell", ...]` instruction

**Escape token** (`internal/semantic/builder.go`):

- Reads `AST.EscapeToken` from BuildKit parser (supports `# escape=` directive)
- Passes it to `dfshell.NewLex()` for proper line continuation

**What does NOT work:**

- Default shell detection without an explicit `SHELL` instruction (Windows containers use `cmd /S /C`
  by default, but tally assumes `/bin/sh -c`)
- ShellCheck still fires when no `SHELL` instruction is present (the default `/bin/sh` assumption
  means shellcheck runs on Windows `cmd` commands)
- No per-stage OS awareness — rules can't query "is this a Windows stage?"

---

## 1. `tally/platform-mismatch` — Redesign

### Problem

The current rule (`buildkit/InvalidBaseImagePlatform`) determines the "expected" platform via:

```go
// semantic/platform.go
func defaultPlatform() string {
    if dp := os.Getenv("DOCKER_DEFAULT_PLATFORM"); dp != "" {
        return dp
    }
    spec := platforms.DefaultSpec()  // uses runtime.GOARCH
    spec.OS = "linux"               // hardcodes OS to linux
    return spec.OS + "/" + spec.Architecture
}
```

This means:

- On macOS arm64: expected = `linux/arm64` → flags `amd64`-only images as wrong
- On Linux amd64 CI: expected = `linux/amd64` → accepts those same images
- **A static linter producing different results per host is fundamentally broken**

For Windows images it's worse: `spec.OS` is forced to `"linux"`, so any Windows image
(`windows/amd64`) always mismatches.

### Proposed Behavior

The rule should only fire when there is a **provable conflict** between what the Dockerfile
requests and what the registry offers:

| Scenario | Action |
|----------|--------|
| `FROM --platform=linux/arm64 image:tag` and registry has `linux/arm64` | No violation |
| `FROM --platform=linux/arm64 image:tag` and registry does NOT have `linux/arm64` | Violation: "image does not publish platform linux/arm64; available: [linux/amd64]" |
| `FROM image:tag` (no `--platform`) | No violation (builder picks at build time) |
| `FROM --platform=$BUILDPLATFORM image:tag` | No violation (dynamic, can't validate statically) |

### Valid Use Cases for `--platform`

Hardcoding `--platform` is legitimate in several scenarios:

- **AWS Graviton / ARM-only services** (e.g. AWS AgentCore only supports arm64)
- **Cross-compilation** (`FROM --platform=$BUILDPLATFORM golang:1.22 AS builder`)
- **Windows containers** (`FROM --platform=windows/amd64 mcr.microsoft.com/...`)

The linter should validate these against the registry, not against the host.

### Docker Language Server Comparison

Docker's language server shows a similar check:

```text
Base image mcr.microsoft.com/windows/servercore/iis:windowsservercore-ltsc2019
was pulled with platform "windows(10.0.17763.8389)/amd64",
expected "linux/arm64" for current build
```

This has the same host-dependent bug. We should do better.

### Implementation

1. Change `ExpectedPlatform()` to return the explicit `--platform` value only (or empty if none)
2. When `--platform` is set and resolvable: query registry, compare, report mismatch
3. When `--platform` is not set: no violation (drop the host-platform comparison entirely)
4. Add the available platforms list to the violation detail for actionable guidance

---

## 2. Semantic Model: Base Image OS Detection

### Problem

Rules need to know whether a stage targets Windows or Linux to suppress inappropriate suggestions.
Currently there is no per-stage OS signal.

### Proposal: `StageInfo.BaseImageOS`

Add a field to the existing `StageInfo` struct:

```go
type BaseImageOS string

const (
    BaseImageOSLinux   BaseImageOS = "linux"
    BaseImageOSWindows BaseImageOS = "windows"
    BaseImageOSUnknown BaseImageOS = ""
)
```

**Detection heuristics (fast, no network):**

| Signal | OS | Confidence |
|--------|-----|------------|
| `FROM mcr.microsoft.com/windows/*` | Windows | High |
| `FROM mcr.microsoft.com/dotnet/*:*-nanoserver*` | Windows | High |
| `FROM mcr.microsoft.com/dotnet/*:*-windowsservercore*` | Windows | High |
| `FROM --platform=windows/*` | Windows | High |
| `# escape=` `` ` `` (backtick) | Windows | Medium |
| `SHELL ["powershell"...]` or `SHELL ["cmd"...]` | Windows | Medium |
| `FROM alpine:*`, `FROM ubuntu:*`, `FROM debian:*` | Linux | High |
| Anything else | Unknown | — |

**Registry-backed detection (slow, optional):**

When slow checks are enabled and the heuristic returns `Unknown`, query the registry for the
image's platform list. If all platforms are `windows/*`, set `BaseImageOSWindows`. If all are
`linux/*`, set `BaseImageOSLinux`. Mixed → keep `Unknown`.

This integrates with the existing `AsyncImageResolver` infrastructure.

### Default Shell Inference

When `BaseImageOS` is `Windows` and no `SHELL` instruction is present, the effective default shell
should be `["cmd", "/S", "/C"]` instead of `["/bin/sh", "-c"]`.

Current default in `semantic.go`:

```go
var DefaultShell = []string{"/bin/sh", "-c"}
```

This should become stage-aware:

```go
func DefaultShellForOS(os BaseImageOS) []string {
    if os == BaseImageOSWindows {
        return []string{"cmd", "/S", "/C"}
    }
    return []string{"/bin/sh", "-c"}
}
```

---

## 3. Semantic Model: Shell Variant per Instruction

### Problem

Rules need to query "what shell does this RUN instruction execute under?" This is already partially
modeled via `StageInfo.ShellSetting`, but:

- It returns the stage-level default, not per-instruction (a `SHELL` instruction mid-stage changes
  it for subsequent RUNs)
- It assumes `/bin/sh` when no `SHELL` is present, even for Windows stages

### Proposal

Expose an ergonomic per-instruction query:

```go
// On StageInfo or Model:
func (m *Model) ShellVariantAt(stageIdx int, line int) shell.Variant
```

This returns the effective shell variant for a RUN/CMD/HEALTHCHECK at the given line, accounting
for:

1. Base image OS (Windows → `VariantNonPOSIX` default)
2. `SHELL` instructions that appear before the given line
3. Inline shell directives (`# hadolint shell=bash`)

Rules that currently call `shell.VariantFromShell()` manually would use this instead.

---

## 4. Escape Character Audit

### Current State

The escape character is read from `AST.EscapeToken` (BuildKit parser) and defaults to `\`.
Windows Dockerfiles commonly use `` # escape=` `` to avoid conflicts with Windows path separators.

### Known Usage Points

| Location | Uses escape token? |
|----------|-------------------|
| `semantic/builder.go` | Yes (from AST) |
| `semantic/platform.go` | Yes (from AST) |
| `shellcheck/shellcheck.go` | Yes (passed to script extraction) |
| `fix/heredoc_resolver.go` | Needs audit |
| Individual rule implementations | Need audit |

### Action Items

1. Audit all rules for hardcoded `\` assumptions (grep for `\\\\` in rule files)
2. Add a backtick-escape test fixture (the Metalama Dockerfile uses `# escape=` `` ` ``)
3. Ensure `SourceMap` methods handle backtick continuation correctly
4. Verify fix generators respect the escape token when producing output

---

## 5. `tally/prefer-multi-stage-build` — Windows Build Tools

### Current Detection

The `scoreStage` function recognizes these build tools:

**Build steps:** `go build`, `cargo build`, `npm run build`, `yarn build`, `pnpm build`,
`dotnet publish`, `mvn package`, `gradle build`, `make`, `cmake`, `ninja`

**Package managers:** `apt-get install`, `apt install`, `apk add`, `dnf install`, `yum install`

### Missing Windows Tools

| Tool | Keyword | Proposed Score | Notes |
|------|---------|---------------|-------|
| MSBuild | `msbuild` (case-insensitive) | 4 | Often invoked via full path `C:\...\MSBuild.exe` |
| NuGet | `nuget restore` | 2 | .NET package restore |
| Chocolatey | `choco install` | 4 | Windows system package manager |
| dotnet build | `dotnet build` | 4 | Already have `dotnet publish`; `build` is also common |

The MSBuild detection needs case-insensitive matching and should match the basename regardless of
path prefix (e.g. `C:\Windows\Microsoft.NET\...\MSBuild.exe` should match).

---

## 6. BuildKit Windows Container Syntax — Current State (2025)

### BuildKit WCOW Status: Experimental

BuildKit added experimental Windows Containers on Windows (WCOW) support in v0.13.0 (early 2024).
As of v0.22.0, it remains experimental:

- Not integrated with `docker build` (requires manual `buildkitd.exe` + `docker buildx`)
- Only containerd worker supported (no OCI worker)
- Supported images: ServerCore:ltsc2019/ltsc2022, NanoServer:ltsc2022

### Feature Support Matrix

| Feature | Linux | Windows (BuildKit) | Windows (Classic) |
|---------|-------|--------------------|-------------------|
| `FROM` | Yes | Yes | Yes |
| `FROM scratch` | Yes | **No** (moby/buildkit#5264) | No |
| `RUN` (shell/exec form) | Yes | Yes | Yes |
| `RUN --mount=type=cache` | Yes | **No** (moby/buildkit#5678) | No |
| `RUN --mount=type=secret` | Yes | **No** (moby/buildkit#5273) | No |
| `RUN --mount=type=ssh` | Yes | **No** (moby/buildkit#4837) | No |
| `RUN --mount=type=bind` | Yes | **No** | No |
| Heredoc (`RUN <<EOF`) | Yes | **Unreliable** | No |
| Multi-stage builds | Yes | Yes | Yes |
| `SHELL` instruction | Yes | Yes | Yes |
| `COPY --chown` | Yes | **No** | No |
| `# syntax=` directive | Yes | Yes | No |
| `# escape=` directive | Yes | Yes | Yes |

### Implications for Tally Rules

| Rule | Impact |
|------|--------|
| `tally/prefer-run-heredoc` | **Must suppress** for Windows stages |
| `tally/prefer-copy-heredoc` | **Must suppress** for Windows stages |
| `tally/prefer-package-cache-mounts` | **Must suppress** for Windows stages (no `--mount` support) |
| `tally/prefer-add-unpack` | Needs investigation — does `ADD --checksum` work on Windows? |
| `hadolint/DL3020` (ADD→COPY) | Still valid on Windows |
| `tally/newline-between-instructions` | Still valid on Windows |

### Package Managers on Windows Containers

| Manager | Available in Containers | Common in Dockerfiles |
|---------|------------------------|----------------------|
| Chocolatey (`choco`) | Yes (installable) | Very common |
| NuGet (`nuget`) | Yes (in .NET images) | .NET only |
| Winget | **No** (not in server images) | Not used |
| Scoop | Yes (installable) | Rare |
| Direct download (PowerShell) | Always | Very common |

`tally/prefer-package-cache-mounts` currently recognizes `dotnet` → `/root/.nuget/packages` but
not `choco`. However, since `--mount=type=cache` doesn't work on Windows, the correct action is
to **suppress the entire rule** for Windows stages rather than add Windows cache paths.

---

## 7. ShellCheck Gating

### Current State

ShellCheck gating works correctly when the shell variant is known:

- `VariantNonPOSIX` (powershell, cmd) → ShellCheck skipped
- `VariantBash`, `VariantPOSIX` → ShellCheck runs with appropriate dialect

### The Gap

When no `SHELL` instruction is present, tally assumes `/bin/sh` as the default shell. For Windows
containers, the actual default is `cmd /S /C`. This means ShellCheck fires on `cmd.exe` commands
with false positives (SC1001, SC1012, SC2154 on Windows paths and PowerShell syntax).

### Fix

Once the semantic model exposes `BaseImageOS`, the ShellCheck rule should:

1. Resolve the effective shell using `BaseImageOS` (not the hardcoded `/bin/sh` default)
2. If effective shell is `cmd` or `powershell` → skip ShellCheck
3. This is independent of whether `SHELL` instruction is explicit

ShellCheck is an expensive WASM operation. The OS-based gating should happen early, before any
WASM compilation or invocation.

---

## 8. `buildkit/WorkdirRelativePath` — Windows Paths

### Problem

`WORKDIR c:/build` is flagged as a relative path because only `/`-prefixed paths are recognized
as absolute. On Windows, `C:\path` and `c:/path` are absolute (drive letter).

### Fix

Add Windows absolute path detection:

```go
func isAbsolutePath(p string, baseOS BaseImageOS) bool {
    if strings.HasPrefix(p, "/") {
        return true
    }
    if baseOS == BaseImageOSWindows && len(p) >= 3 {
        // C:\ or C:/
        if isLetter(p[0]) && p[1] == ':' && (p[2] == '/' || p[2] == '\\') {
            return true
        }
    }
    return false
}
```

This requires the `BaseImageOS` signal from the semantic model enhancement.

---

## Implementation Phases

### Phase 1: Semantic Model Foundation (Prerequisite for All)

1. Add `BaseImageOS` to `StageInfo` with heuristic detection (image name + platform + escape +
   SHELL patterns)
2. Add `DefaultShellForOS()` to infer correct default shell per stage
3. Add `ShellVariantAt(stage, line)` for ergonomic per-instruction queries
4. Add integration tests with the two Windows fixtures

### Phase 2: Suppress False Positives

1. Gate ShellCheck on effective shell (using `BaseImageOS`-aware defaults)
2. Gate `prefer-run-heredoc` and `prefer-copy-heredoc` on `BaseImageOS != Windows`
3. Gate `prefer-package-cache-mounts` on `BaseImageOS != Windows`
4. Fix `WorkdirRelativePath` to recognize Windows absolute paths
5. Update `TestFixWindowsContainer` snapshot — expect zero false positives

### Phase 3: Redesign `tally/platform-mismatch`

1. Change rule to only fire when `--platform` is explicitly set on FROM
2. Validate the explicit platform against registry-available platforms
3. Remove host-platform comparison entirely
4. Include available platforms in violation detail
5. Add test cases for cross-platform scenarios (ARM enforcement, Windows images)

### Phase 4: Windows Build Tool Recognition

1. Add `choco install`, `msbuild`, `nuget restore`, `dotnet build` to
   `prefer-multi-stage-build` detection
2. Case-insensitive matching for `msbuild` (often invoked as `MSBuild.exe`)
3. Basename extraction for full-path invocations

### Phase 5: Escape Character Audit

1. Grep all rules for hardcoded `\\` or `\n` assumptions
2. Add backtick-escape test cases for each affected rule
3. Verify fix generators respect the escape token

---

## References

- **Test fixtures:** `internal/integration/testdata/real-world-fix-ticketdesk/Dockerfile`,
  `internal/integration/testdata/real-world-fix-metalama/Dockerfile`
- **Semantic model:** `internal/semantic/semantic.go`, `stage_info.go`, `builder.go`,
  `platform.go`
- **Platform rule:** `internal/rules/buildkit/invalid_base_image_platform.go`
- **ShellCheck:** `internal/rules/shellcheck/shellcheck.go`, `internal/shell/shell.go`
- **BuildKit WCOW issues:** moby/buildkit#5678 (mount), #5264 (scratch), #5273 (secret),
  #4837 (ssh)
- **BuildKit WCOW docs:** <https://docs.docker.com/build/buildkit/wcow/>
- **Docker language server:** InvalidBaseImagePlatform check
