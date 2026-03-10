# 27. Windows and PowerShell Rules (`tally/windows/*`, `tally/powershell/*`)

**Status:** Proposed (rule ideas — placeholder for future implementation)
**Prerequisite:** [26. Windows Container Support](26-windows-container-support.md) (platform detection in semantic model)

---

## Why a Dedicated Rule Namespace

Windows container Dockerfiles differ from Linux ones in fundamental ways:

- **Default shell** is `cmd /S /C`, not `/bin/sh -c`
- **Escape character** is commonly backtick (`` ` ``) via `# escape=` directive
- **Path separators** are `\` (also the default escape character — hence the backtick convention)
- **No POSIX toolchain** — no `apt-get`, `sh`, `bash`, `tar`, `curl` in base images
- **Layer sizes** are much larger (ServerCore base is ~5 GB vs ~80 MB for Alpine)
- **BuildKit features** like `COPY --chown` are unsupported; `RUN --mount` passes through the
  entire BuildKit pipeline but fails at the containerd/HCS runtime (see source-confirmed details
  below); heredocs are partial (work with PowerShell shell, not with `cmd.exe`)
- **Package managers** are Chocolatey, NuGet, and direct PowerShell downloads — not apt/apk/dnf

These differences mean many Linux-oriented best practices don't apply, and Windows containers have
their own set of anti-patterns and optimization opportunities.

**BuildKit source-confirmed details:**

- **Default shell** is defined in `frontend/dockerfile/dockerfile2llb/defaultshell.go`:
  `defaultShell("windows")` returns `["cmd", "/S", "/C"]`, while all other OS values return
  `["/bin/sh", "-c"]`. This is called from `withShell()` (`convert.go:2172`) which wraps
  shell-form RUN/CMD/ENTRYPOINT. A custom `SHELL` instruction takes priority over the default.
- **`STOPSIGNAL` is silently accepted** on Windows — BuildKit's `StopSignalCommand` implements
  `CheckPlatform()` to reject it on Windows, but this method is **never called** in the dispatch
  path (dead code since at least the BuildKit fork from classic Docker). The instruction is written
  to the image config but has no effect at runtime since Windows containers don't support POSIX
  signals.
- **`RUN --mount` is not blocked by BuildKit** — `dispatchRunMounts()` in
  `convert_runmount.go:64-149` processes all mount types (cache, secret, ssh, bind, tmpfs) with
  zero OS/platform checks. Mounts pass through the entire Dockerfile frontend, LLB layer, and OCI
  spec generation without any Windows guard. The failures tracked in moby/buildkit#5678 (cache),

  #5273 (secret), #4837 (ssh) are **runtime errors** at the containerd/HCS layer — the build
  starts but errors during the RUN step execution. This makes it an ideal lint target: we can
  catch a guaranteed build failure before the user waits for image pulls and prior layers.

The `tally/windows/*` namespace
makes it clear that these rules only fire on Windows stages (detected via the `BaseImageOS`
semantic model field from [design-docs/26](26-windows-container-support.md)).

**Reference:**
[Optimize Windows Dockerfiles](https://learn.microsoft.com/en-us/virtualization/windowscontainers/manage-docker/optimize-windows-dockerfile)

---

## Two Orthogonal Dimensions: OS and Shell

The key insight is that **OS** and **shell** are independent axes. PowerShell runs on Linux:

```dockerfile
FROM mcr.microsoft.com/powershell:ubuntu-22.04
RUN Install-Module PSReadLine -Force
RUN Invoke-WebRequest https://example.com/file.zip -OutFile /tmp/file.zip
```

This is a **Linux** container with a **PowerShell** shell. The old `VariantNonPOSIX` enum
lumped `cmd.exe` and `powershell` together, but they're very different:

| | cmd.exe | PowerShell | sh/bash |
|---|---------|-----------|---------|
| **OS** | Windows only | Cross-platform | Linux/macOS |
| **Error handling** | `&&` chaining | `$ErrorActionPreference` | `set -e` |
| **Progress bars** | N/A | `$ProgressPreference` | N/A |
| **Command chaining** | `&&`, `&` | `;`, pipeline | `&&`, `;`, pipeline |
| **ShellCheck applicable** | No | No | Yes |
| **Heredoc applicable** | No | Partial (see note) | Yes (BuildKit) |
| **Script parsing** | No parser available | tree-sitter-backed command parsing | mvdan.cc/sh |

**Note on PowerShell heredoc:** BuildKit's Dockerfile heredoc (`RUN <<EOF`) can work with PowerShell
as the active shell. For a single heredoc without a shebang, BuildKit passes the heredoc content
(with newlines preserved) as the last argument to the configured shell — so `pwsh -Command` receives
a multi-line script. This was **tested and confirmed** to work, including PowerShell here-strings
(`@"..."@`) which require their delimiters on separate lines. However, `cmd.exe` cannot handle
multi-line heredoc content, and the shebang path (`/dev/pipes/`) is explicitly disabled for Windows
in BuildKit source (`convert.go:1251`). For now, we suppress heredoc suggestions on all Windows
stages and revisit per-shell gating later.

This means rules split into two namespaces:

### `tally/windows/*` — OS-gated rules

Fire only on Windows stages. About container platform limitations:

- Layer size optimization (NTFS layers are ~100x larger)
- BuildKit feature support (`--chown` unsupported; heredoc partial — see note above)
- `RUN --mount` passes BuildKit validation but fails at containerd/HCS runtime (new `no-run-mounts` rule)
- `STOPSIGNAL` is silently accepted but has no effect (new `no-stopsignal` rule)
- Base image choices (ServerCore vs NanoServer)
- Path validation (drive letters, backslash separators)
- Windows-only anti-patterns

### `tally/powershell/*` — Shell-gated rules

Fire whenever PowerShell is the effective shell, **on any OS**. About PowerShell scripting
best practices in Dockerfile `RUN` instructions:

- Error handling (`$ErrorActionPreference`)
- Progress suppression (`$ProgressPreference`)
- Shell instruction recommendation (avoid `RUN powershell -Command`)

### The Matrix

```text
                        Shell
                 cmd    PowerShell    sh/bash
          ┌──────────┬─────────────┬──────────┐
 Windows  │ windows/ │ windows/ +  │ (rare)   │
   OS     │          │ powershell/ │          │
          ├──────────┼─────────────┼──────────┤
 Linux    │ (N/A)    │ powershell/ │ (default)│
          └──────────┴─────────────┴──────────┘
```

A Windows stage with PowerShell gets both `tally/windows/*` AND `tally/powershell/*` rules.
A Linux PowerShell stage (e.g. `mcr.microsoft.com/powershell:ubuntu-22.04`) gets only
`tally/powershell/*` rules.

### Refining `shell.Variant`

`VariantNonPOSIX` has been split (implemented). Previously in `internal/shell/shell.go`:

```go
// BEFORE (removed):
const (
    VariantBash     Variant = iota  // bash, zsh
    VariantPOSIX                     // sh, dash, ash
    VariantMksh                      // mksh, ksh
    VariantNonPOSIX                  // powershell, pwsh, cmd (EVERYTHING non-POSIX)
)
```

Proposed:

```go
const (
    VariantBash       Variant = iota  // bash, zsh
    VariantPOSIX                       // sh, dash, ash
    VariantMksh                        // mksh, ksh
    VariantPowerShell                  // powershell, pwsh (cross-platform)
    VariantCmd                         // cmd.exe (Windows only)
    VariantUnknown                     // unrecognized shells
)

// Positive intent-based queries — each caller states what it actually needs.

// IsShellCheckCompatible returns true for shells that ShellCheck can analyze.
func (v Variant) IsShellCheckCompatible() bool {
    return v == VariantPOSIX || v == VariantBash || v == VariantMksh
}

// IsParseable returns true for shells that mvdan.cc/sh can parse.
// Same set as ShellCheck for now, but semantically distinct.
func (v Variant) IsParseable() bool {
    return v == VariantPOSIX || v == VariantBash || v == VariantMksh
}

// SupportsHeredoc returns true for shells compatible with BuildKit heredoc syntax.
func (v Variant) SupportsHeredoc() bool {
    return v == VariantPOSIX || v == VariantBash || v == VariantMksh
}

// IsPowerShell returns true for PowerShell variants (pwsh, powershell).
func (v Variant) IsPowerShell() bool {
    return v == VariantPowerShell
}
```

**`IsNonPOSIX()` is removed, not deprecated.** The vague negative test hides what the caller
actually needs and has already caused real bugs — ShellCheck fires on Windows `cmd.exe` commands
because the default shell assumption is wrong and the gate says "is it non-POSIX?" instead of
"can ShellCheck handle this?"

The 30 call sites using `IsNonPOSIX()` get updated in one pass. Each migration forces the
developer to choose the correct positive query:

| Call site | Current: `IsNonPOSIX()` | Migrated to |
|-----------|------------------------|-------------|
| `shellcheck.go:dialectForShellName` | skip if NonPOSIX | `!IsShellCheckCompatible()` |
| `prefer_heredoc.go` per-stage skip | skip if NonPOSIX | `!SupportsHeredoc()` |
| `prefer_copy_heredoc.go` per-stage skip | skip if NonPOSIX | `!SupportsHeredoc()` |
| `prefer_package_cache_mounts.go` | skip if NonPOSIX | `!IsParseable()` |
| `prefer_add_unpack.go` | skip if NonPOSIX | `!IsParseable()` |
| `newline_per_chained_call.go` | skip if NonPOSIX | `!IsParseable()` |
| `shell/count.go` (~7 sites) | skip if NonPOSIX | `!IsParseable()` |
| `shell/file_creation.go` (~3 sites) | skip if NonPOSIX | `!IsParseable()` |
| `shell/chain_format.go` (~3 sites) | skip if NonPOSIX | `!IsParseable()` |
| `hadolint/dl3010.go` | skip if NonPOSIX | `!IsParseable()` |
| `hadolint/dl4001.go` | skip if NonPOSIX | `!IsParseable()` |
| `hadolint/dl4006.go` | skip if NonPOSIX | `!IsParseable()` |
| `hadolint/helpers.go` | skip if NonPOSIX | `!IsParseable()` |

This is a mechanical refactor (~30 sites) but each one is a deliberate choice, not a
find-and-replace. The compiler catches any missed sites since `IsNonPOSIX()` is deleted.

---

## Rules: `tally/windows/*` (OS-Gated)

These rules check `StageInfo.BaseImageOS == Windows`.

---

## Rules: `tally/powershell/*` (Shell-Gated)

These rules check `StageInfo.ShellSetting.Variant.IsPowerShell()` or detect PowerShell
invocations in RUN commands regardless of the default shell.

### `tally/powershell/prefer-shell-instruction`

**Severity:** style
**Trigger:** Multiple `RUN powershell ...` / `RUN pwsh ...` / `RUN @powershell ...` invocations
without a preceding `SHELL` instruction setting PowerShell as the default

This rule fires on **any OS** — the pattern is identical on Windows (`powershell`) and Linux
(`pwsh`). The fix adapts the executable name based on what the Dockerfile uses:

```dockerfile
# Anti-pattern (Windows)
FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command Invoke-WebRequest ...
RUN powershell -Command Start-Process ...

# Anti-pattern (Linux)
FROM mcr.microsoft.com/powershell:ubuntu-22.04
RUN pwsh -Command Install-Module ...
RUN pwsh -Command Invoke-WebRequest ...
```

```dockerfile
# Recommended (adapts executable name)
SHELL ["powershell", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
# or on Linux:
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
```

### `tally/powershell/error-action-preference`

(Moved from `tally/windows/*` — applies on any OS with PowerShell)

**Severity:** warning
**Trigger:** Multi-command PowerShell `RUN` without `$ErrorActionPreference = 'Stop'`

### `tally/powershell/progress-preference`

(Moved from `tally/windows/*` — applies on any OS with PowerShell)

**Severity:** style
**Trigger:** PowerShell `Invoke-WebRequest` without `$ProgressPreference = 'SilentlyContinue'`

---

## Rules: `tally/windows/*` (OS-Gated, Remaining)

### `tally/windows/no-stopsignal`

**Severity:** warning
**Trigger:** `STOPSIGNAL` instruction in a Windows stage

**Problem:**

Windows containers do not support POSIX signals. BuildKit defines a `PlatformSpecific` interface
(`frontend/dockerfile/instructions/commands.go:109-112`) with `StopSignalCommand.CheckPlatform()`
returning an error for `"windows"`:

```go
func (c *StopSignalCommand) CheckPlatform(platform string) error {
    if platform == "windows" {
        return errors.New("The daemon on this platform does not support the command stopsignal")
    }
    return nil
}
```

However, this `CheckPlatform()` is **never called** — `dispatchStopSignal()` (`convert.go:1858`)
directly sets the signal without any platform guard. The build succeeds silently, but `STOPSIGNAL`
has no effect on Windows containers at runtime. This is a latent bug in BuildKit (the check exists
but is dead code), and a good lint opportunity — warn the user that the instruction is meaningless.

```dockerfile
# Anti-pattern: STOPSIGNAL has no effect on Windows
FROM mcr.microsoft.com/windows/servercore:ltsc2022
STOPSIGNAL SIGTERM
```

**Detection:**

- Check for `STOPSIGNAL` instruction in a stage where `BaseImageOS == Windows`
- Simple single-instruction pattern match, no command parsing needed

---

### `tally/windows/no-run-mounts`

**Severity:** error
**Trigger:** `RUN --mount=type=*` in a Windows stage

**Problem:**

All `RUN --mount` types (`cache`, `secret`, `ssh`, `bind`) fail at runtime on Windows containers.
BuildKit's Dockerfile frontend has **no platform guard** — `dispatchRunMounts()`
(`convert_runmount.go:64-149`) processes every mount type identically regardless of OS. Mounts
pass through the full pipeline (Dockerfile frontend → LLB → OCI spec generation → executor) and
only fail at the containerd/HCS layer when the container runtime attempts to set them up.

This means a Dockerfile like:

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=cache,target=C:\Users\ContainerUser\.nuget\packages \
    dotnet restore
```

will successfully parse, pull the base image, and begin building — only to fail with a runtime
error at the `RUN` step. On large Windows images (5+ GB base layers), the user may wait minutes
before hitting the error. A lint rule catches this immediately.

**Source-verified behavior:**

- `convert_runmount.go:64` — no `if windows` / `if d.image.OS == "windows"` check anywhere
- `executor/oci/spec.go:213-218` — mounts are added to OCI spec with `normalizeMountType()`
  which returns `""` on Windows (avoids `"bind"` type that HCS doesn't understand), but this
  only affects the root filesystem mount, not `--mount` mounts
- `executor/containerdexecutor/executor_windows.go:88` — `mounts` passed directly to
  `oci.GenerateSpec` without filtering

**All mount types affected:**

| Mount type | Issue | Runtime behavior |
|------------|-------|------------------|
| `--mount=type=cache` | moby/buildkit#5678 | HCS error setting up cache volume |
| `--mount=type=secret` | moby/buildkit#5273 | tmpfs-based secrets not supported on Windows |
| `--mount=type=ssh` | moby/buildkit#4837 | Unix socket forwarding not available on Windows |
| `--mount=type=bind` | — | Bind mount semantics differ on HCS |
| `--mount=type=tmpfs` | — | tmpfs not a Windows concept |

**Detection:**

- Check for `RUN` instructions with any `--mount` flag in a stage where `BaseImageOS == Windows`
- Parse the mount flag to extract the type for the violation message
- Severity is `error` (not `warning`) because the build **will** fail — this is not a style
  issue but a guaranteed build break

```dockerfile
# All of these will fail at runtime on Windows:
RUN --mount=type=cache,target=C:\cache dotnet restore
RUN --mount=type=secret,id=my_secret cmd /C type C:\run\secrets\my_secret
RUN --mount=type=ssh git clone git@github.com:org/repo.git
```

---

### `tally/windows/group-run-layers`

**Severity:** info
**Trigger:** Multiple consecutive `RUN` instructions that could be combined

**Problem:**

Windows container layers are significantly larger than Linux ones due to NTFS copy-on-write
semantics. Each `RUN` instruction creates a new layer. The Microsoft optimization guide explicitly
recommends grouping related actions:

```dockerfile
# Anti-pattern: 3 layers (~330 MB total)
RUN powershell Invoke-WebRequest ... -OutFile c:\python.exe
RUN powershell Start-Process c:\python.exe -Wait
RUN powershell Remove-Item c:\python.exe -Force
```

```dockerfile
# Recommended: 1 layer (~216 MB total)
RUN powershell -Command \
  $ErrorActionPreference = 'Stop'; \
  Invoke-WebRequest ... -OutFile c:\python.exe ; \
  Start-Process c:\python.exe -Wait ; \
  Remove-Item c:\python.exe -Force
```

**Note:** This is related to `tally/prefer-run-heredoc` but since heredocs don't work on Windows,
this rule uses the Windows-native chaining pattern (`;` in PowerShell or `&&` in cmd).

**Detection:**

- Find consecutive RUN instructions in a Windows stage
- Check if they could be logically grouped (e.g. download → install → cleanup)
- Score based on count of consecutive RUNs (3+ triggers the rule)

---

### `tally/windows/cleanup-in-same-layer`

**Severity:** warning
**Trigger:** File download in one RUN, deletion in a separate RUN

**Problem:**

On Windows, deleting a file in a later layer does not reduce the image size — the file persists
in the earlier layer. This is especially costly given Windows layer sizes. The download + install +
cleanup must happen in the same `RUN` instruction.

```dockerfile
# Anti-pattern: installer persists in layer 1 even after deletion in layer 2
RUN powershell Invoke-WebRequest ... -OutFile c:\installer.exe
RUN powershell Start-Process c:\installer.exe -Wait
RUN powershell Remove-Item c:\installer.exe    # does NOT reduce image size
```

**Detection:**

- Track files created by `Invoke-WebRequest -OutFile`, `curl -o`, `wget -O` patterns
- Check if `Remove-Item` / `del` of the same file happens in a different RUN
- Suggest combining into a single RUN

---

### `tally/windows/prefer-nanoserver`

**Severity:** info
**Trigger:** Single-stage build using `servercore` as the final image when `nanoserver` might suffice

**Problem:**

ServerCore images are ~5 GB. NanoServer images are ~300 MB. If the final application doesn't
need the full ServerCore API surface (e.g. it's a compiled .NET Core application), using
NanoServer as the runtime stage dramatically reduces image size.

```dockerfile
# Could benefit from multi-stage
FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN dotnet publish -c Release -o /app
ENTRYPOINT ["dotnet", "/app/MyApp.dll"]
```

```dockerfile
# Better: build on servercore, run on nanoserver
FROM mcr.microsoft.com/dotnet/sdk:8.0-windowsservercore-ltsc2022 AS build
RUN dotnet publish -c Release -o /app

FROM mcr.microsoft.com/dotnet/runtime:8.0-nanoserver-ltsc2022
COPY --from=build /app /app
ENTRYPOINT ["dotnet", "/app/MyApp.dll"]
```

**Note:** This overlaps with `tally/prefer-multi-stage-build` but is Windows-specific because the
size difference between ServerCore and NanoServer is dramatic (~17x).

**Detection:**

- Final stage uses a `servercore` base image
- Stage contains `dotnet publish`, `dotnet build`, or similar compiled-language build commands
- No `nanoserver` stage exists in the Dockerfile

---

---

## Future Ideas (Not Yet Designed)

- **`tally/windows/prefer-servercore-ltsc`**: Recommend using LTSC (Long-Term Servicing Channel)
  tags over SAC (Semi-Annual Channel) for stability in production.
- **`tally/windows/no-chown-flag`**: Warn that `COPY --chown` and `ADD --chown` are not
  supported on Windows and will be silently ignored.
- **`tally/windows/escape-directive`**: Suggest `# escape=` `` ` `` at the top of Windows
  Dockerfiles to avoid backslash conflicts with Windows paths.
- ~~**`tally/windows/no-unsupported-mount`**~~: Promoted to fully designed rule as
  `tally/windows/no-run-mounts` (see above).

---

## Implementation: How Rules Fire

### Activation Model

`tally/windows/*` rules are **enabled by default** (not experimental) but **only produce
violations on Windows stages**. This is the same pattern as ShellCheck — it's always "enabled" but
skips non-POSIX shells. Users never need to opt in; the rules are invisible on Linux Dockerfiles.

The gate is **not** in the config system. It's inside each rule's `Check()` method, using the
semantic model to determine whether the current stage targets Windows.

### The Single Gate: `BaseImageOS` on `StageInfo`

The prerequisite ([design-docs/26](26-windows-container-support.md)) adds a `BaseImageOS` field to
`StageInfo` in `internal/semantic/stage_info.go`:

```go
type BaseImageOS int

const (
    BaseImageOSUnknown BaseImageOS = iota
    BaseImageOSLinux
    BaseImageOSWindows
)

type StageInfo struct {
    // ... existing fields ...
    BaseImageOS BaseImageOS  // Detected OS for this stage's base image
}
```

This field is populated during semantic model construction in `internal/semantic/builder.go`, using
the heuristics described in design-docs/26 (image name patterns, `--platform`, `# escape=`
directive, `SHELL` instruction).

### Where the Gate Lives in Code

**NOT in the linter dispatch loop.** The dispatch loop in `internal/linter/linter.go` stays
unchanged:

```go
// linter.go:171-178 — NO CHANGES HERE
for _, rule := range rules.All() {
    if isSkipped(rule.Metadata().Code, skipSet) {
        continue
    }
    ruleInput := baseInput
    ruleInput.Config = cfg.Rules.GetOptions(rule.Metadata().Code)
    violations = append(violations, rule.Check(ruleInput)...)
}
```

**Each rule gates itself** at the top of its `Check()` method. This is the existing pattern —
see `CopyFromEmptyScratchStageRule`, `FromPlatformFlagConstDisallowedRule`, and
`PreferMultiStageBuildRule` which all do early returns based on stage properties.

### Shared Helpers

To avoid duplicating the semantic model access + OS/shell checks, add helpers per package:

```go
// internal/rules/tally/windows/gate.go
package windows

// windowsStages returns StageInfo entries for stages targeting Windows.
func windowsStages(input rules.LintInput) []*semantic.StageInfo {
    sem, ok := input.Semantic.(*semantic.Model)
    if !ok || sem == nil {
        return nil
    }
    var stages []*semantic.StageInfo
    for i := range sem.StageCount() {
        info := sem.StageInfo(i)
        if info != nil && info.BaseImageOS == semantic.BaseImageOSWindows {
            stages = append(stages, info)
        }
    }
    return stages
}
```

```go
// internal/rules/tally/powershell/gate.go
package powershell

// powershellStages returns StageInfo entries where PowerShell is the effective
// shell OR where PowerShell is explicitly invoked in RUN instructions.
func powershellStages(input rules.LintInput) []*semantic.StageInfo {
    sem, ok := input.Semantic.(*semantic.Model)
    if !ok || sem == nil {
        return nil
    }
    var stages []*semantic.StageInfo
    for i := range sem.StageCount() {
        info := sem.StageInfo(i)
        if info == nil {
            continue
        }
        if info.ShellSetting.Variant.IsPowerShell() {
            stages = append(stages, info)
            continue
        }
        // Also match stages where RUN explicitly invokes powershell/pwsh
        // even though the default shell is cmd or sh
        if stageInvokesPowerShell(info) {
            stages = append(stages, info)
        }
    }
    return stages
}
```

Every `tally/windows/*` rule starts with:

```go
func (r *GroupRunLayersRule) Check(input rules.LintInput) []rules.Violation {
    stages := windowsStages(input)
    if len(stages) == 0 {
        return nil  // Not a Windows Dockerfile — nothing to do
    }
    // ... Windows-specific checks ...
}
```

Every `tally/powershell/*` rule starts with:

```go
func (r *ErrorActionPreferenceRule) Check(input rules.LintInput) []rules.Violation {
    stages := powershellStages(input)
    if len(stages) == 0 {
        return nil  // No PowerShell usage — nothing to do
    }
    // ... PowerShell-specific checks ...
}
```

### Unified Routing with Shell Detection

The user asks: isn't this the same gate as shell detection? Yes — `BaseImageOS` and shell variant
are two facets of the same signal. They should be computed together and exposed together.

Currently, shell variant resolution responsibilities are split across two layers:

1. **Semantic model** (`internal/semantic/builder.go`): Computes `StageInfo.ShellSetting` from
   `SHELL` instructions and `# hadolint shell=` directives, and provides the stage-level default
   shell variant via `StageInfo`.

2. **ShellCheck rule** (`internal/rules/shellcheck/shellcheck.go:704`): Calls
   `initialShellNameForStage()` for fallback/default behavior, then tracks `SHELL` transitions
   during stage traversal for per-instruction dialect selection. Gating uses
   `variant.IsShellCheckCompatible()` (via `dialectForShellName()`).

The fix is to make the semantic model the **single source of truth** for both OS and shell:

```text
semantic/builder.go
  ↓
  1. Detect BaseImageOS from FROM instruction (heuristic)
  2. If BaseImageOS == Windows && no SHELL instruction:
       ShellSetting.Shell = ["cmd", "/S", "/C"]
       ShellSetting.Variant = VariantCmd
     else:
       (existing logic — default /bin/sh or explicit SHELL)
  3. Store both on StageInfo
```

Then consumers just read from `StageInfo`:

| Consumer | What it reads | Current source | After refactor |
|----------|--------------|----------------|----------------|
| ShellCheck rule | Shell variant per instruction | StageInfo for initial shell + local in-rule tracking for mid-stage `SHELL` changes | `sem.StageInfo(i).ShellSetting.Variant` + shared helper for per-instruction updates |
| `tally/windows/*` rules | Is this Windows? | (doesn't exist) | `sem.StageInfo(i).BaseImageOS` |
| `prefer-run-heredoc` | Should suggest heredoc? | Always yes | Skip if `BaseImageOS == Windows` |
| `prefer-package-cache-mounts` | Should suggest cache mount? | Always yes | Skip if `BaseImageOS == Windows` |
| `buildkit/WorkdirRelativePath` | Is `c:/path` absolute? | Hardcoded `/` check | Check `BaseImageOS` for drive-letter paths |

**The ShellCheck rule's `initialShellNameForStage()` function can be simplified** to read
from `StageInfo.ShellSetting` first and treat directive fallback as a legacy-only path. The
`collectTasksForStage()` method should keep per-instruction `SHELL` tracking so dialect selection
remains correct after mid-stage shell changes.

### Package and Directory Structure

```text
internal/rules/tally/
├── windows/                     # NEW: Windows OS-gated rules
│   ├── gate.go                  # windowsStages() helper
│   ├── no_stopsignal.go
│   ├── no_run_mounts.go
│   ├── group_run_layers.go
│   ├── cleanup_in_same_layer.go
│   └── prefer_nanoserver.go
├── powershell/                  # NEW: PowerShell shell-gated rules
│   ├── gate.go                  # powershellStages() helper
│   ├── prefer_shell_instruction.go
│   ├── error_action_preference.go
│   └── progress_preference.go
├── max_lines.go
├── prefer_heredoc.go
├── ...
```

Both are separate Go packages requiring their own import lines:

```go
// internal/rules/all/all.go
import (
    _ "github.com/wharflab/tally/internal/rules/buildkit"
    _ "github.com/wharflab/tally/internal/rules/hadolint"
    _ "github.com/wharflab/tally/internal/rules/shellcheck"
    _ "github.com/wharflab/tally/internal/rules/tally"
    _ "github.com/wharflab/tally/internal/rules/tally/windows"    // NEW
    _ "github.com/wharflab/tally/internal/rules/tally/powershell" // NEW
)
```

### Rule Code Convention

```go
// internal/rules/tally/windows/no_stopsignal.go
const NoStopSignalCode = rules.TallyRulePrefix + "windows/no-stopsignal"

// internal/rules/tally/windows/no_run_mounts.go
const NoRunMountsCode = rules.TallyRulePrefix + "windows/no-run-mounts"

// internal/rules/tally/windows/group_run_layers.go
const GroupRunLayersCode = rules.TallyRulePrefix + "windows/group-run-layers"

// internal/rules/tally/powershell/error_action_preference.go
const ErrorActionPreferenceCode = rules.TallyRulePrefix + "powershell/error-action-preference"
```

This produces codes like `tally/windows/group-run-layers` and
`tally/powershell/error-action-preference` which:

- Work with `--select tally/windows/*` (all Windows rules)
- Work with `--select tally/powershell/*` (all PowerShell rules)
- Work with `--select tally/*` (all tally rules including both)
- Work with `--ignore tally/windows/*` (disable all Windows rules)
- Are discoverable in `tally lint --list-rules` output

### Config Integration

Both namespaces follow the standard config pattern. Users can configure them in `.tally.toml`:

```toml
# Disable all Windows OS rules
[rules."tally/windows/*"]
severity = "off"

# Disable all PowerShell rules
[rules."tally/powershell/*"]
severity = "off"

# Adjust specific rule
[rules."tally/powershell/error-action-preference"]
severity = "error"    # escalate from warning to error
```

No special config surface needed — the existing rule config system handles namespaced rules.

### Suppression of Incompatible Rules

Some existing rules must **suppress themselves** based on OS or shell. This is the reverse
gate — existing rules adding early returns for incompatible stages:

| Rule | Suppress when | Reason | Where to add gate |
|------|--------------|--------|-------------------|
| `tally/prefer-run-heredoc` | OS=Windows OR shell=PowerShell/Cmd | Heredoc doesn't work | `prefer_heredoc.go` per-stage loop |
| `tally/prefer-copy-heredoc` | OS=Windows OR shell=PowerShell/Cmd | Same | `prefer_copy_heredoc.go` per-stage loop |
| `tally/prefer-package-cache-mounts` | OS=Windows | `--mount=type=cache` fails at runtime (complemented by `no-run-mounts`) | `prefer_package_cache_mounts.go` per-stage loop |
| `shellcheck/SC*` | shell=PowerShell or Cmd | Gated via `!IsShellCheckCompatible()` in the ShellCheck rule path | Works today (no change needed) |
| `hadolint/DL4006` (pipefail) | shell=PowerShell or Cmd | POSIX-only concept | `dl4006.go` (already gated) |

The gate is a per-stage `continue` in the iteration loop:

```go
func (r *PreferHeredocRule) Check(input rules.LintInput) []rules.Violation {
    // ... existing setup ...
    for i := 1; i < len(input.Stages); i++ {
        // NEW: skip stages where heredoc is not applicable
        if sem != nil {
            if info := sem.StageInfo(i); info != nil {
                if info.BaseImageOS == semantic.BaseImageOSWindows ||
                    !info.ShellSetting.Variant.SupportsHeredoc() {
                    continue
                }
            }
        }
        // ... existing per-stage logic ...
    }
}
```

Note: the gate uses `SupportsHeredoc()` (not `IsShellCheckCompatible()`) because heredoc
requires a POSIX-compatible shell — PowerShell on Linux also can't use heredocs.

If selected `SC` codes are later implemented natively in Go, they should keep the same
`shellcheck/SC*` rule IDs and use this same shell-compatibility gate so behavior remains
source-agnostic (`WASM` vs native).

### Summary: Gate Architecture

```text
                    ┌─────────────────────────┐
                    │  semantic/builder.go     │
                    │  (single source of truth)│
                    └────────┬────────────────┘
                             │
              ┌──────────────┼──────────────────┐
              ▼              ▼                   ▼
     StageInfo.BaseImageOS  StageInfo.ShellSetting  (other fields)
              │              │
     ┌────────┴──────┐  ┌───┴──────────────────────────────┐
     │               │  │              │                    │
     ▼               ▼  ▼              ▼                    ▼
  tally/windows/*  Linux rule    tally/powershell/*    shellcheck/SC*
  (OS==Windows)    suppression   (IsPowerShell())      (IsShellCheck
                   (OS==Windows                         Compatible())
                    or !POSIX)
                                                    WorkdirRelativePath
                                                    (OS==Windows →
                                                     drive letters OK)
```

All paths read from `StageInfo` — no rule computes OS or shell independently.
`BaseImageOS` and `ShellSetting.Variant` are the two orthogonal gates.
