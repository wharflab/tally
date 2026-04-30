# PSScriptAnalyzer Integration: Sidecar Design

> **Date**: 2026-04-21
> **Windows verification**: 2026-04-30
> **Status**: Design note — macOS host checks and Windows sidecar smoke tests
> reproduced.
> **Purpose**: Decide how tally should invoke PowerShell's PSScriptAnalyzer to
> lint `.ps1` / `.psm1` content — both standalone and inside Dockerfile
> `RUN pwsh -Command ...` / `SHELL ["pwsh", ...]` contexts.

## Goal

Give tally a PSScriptAnalyzer capability comparable to its existing ShellCheck
integration:

- Embedded or bundled — no mandatory "install a second toolchain" step for users
  who already run tally.
- Single long-lived process for the linter session (amortize cold start).
- Structured, line-/column-accurate diagnostics usable by existing
  `internal/lint` and `internal/lsp` pipelines.
- Cross-platform: Windows, macOS, Linux — the three targets tally already
  supports.

The existing ShellCheck design is a useful reference point: a GHC-compiled
reactor WASM module, `_initialize`-d once, reused for every script in the
session (see `shellcheck-reactor-single-instance.md`). The question answered
below is whether anything remotely similar is viable for PSScriptAnalyzer, and
if not, what ships instead.

## TL;DR

**Ship a PowerShell sidecar**, not a WASM module and not a Go↔CLR FFI bridge.

- PSScriptAnalyzer is a .NET library, but it **cannot be used standalone**: it
  requires a live `System.Management.Automation.Runspaces.Runspace`, i.e. the
  full PowerShell engine.
- PowerShell-on-WASM does not exist. `PowerShell.Create()` uses process
  creation, assembly-load contexts, and PInvoke paths that the browser WASM
  sandbox blocks. WASI is marginally more permissive but still non-functional;
  no working PoC exists in the wild.
- The only officially supported embedding path is **host the .NET runtime
  yourself**. On Windows you can do that in-process via `hostfxr.dll` + cgo; on
  macOS/Linux the same path works but adds ~100 MB of shipped runtime and
  kills Go's cross-compilation story. For a linter, the juice isn't worth the
  squeeze.
- A long-running `pwsh` 7.x subprocess speaking JSON-framed IPC gets us
  everything we need with a single code path across all three OSes. Windows
  PowerShell 5.1 is **not** a supported host fallback: tally's Dockerfile
  PowerShell target is `pwsh` 7.x syntax, and PSScriptAnalyzer hosted in 5.1
  uses the 5.1 parser. The cold-start tax amortizes over a session, and
  PSScriptAnalyzer's batch throughput on real fixtures is acceptable.

## What we verified on macOS (2026-04-21)

Host: `darwin 25.4.0` arm64, `pwsh 7.6.0` from Homebrew.

### Install and import

```text
Install-Module -Name PSScriptAnalyzer -Scope CurrentUser -Force \
  -AcceptLicense -SkipPublisherCheck
```

- Clean install, no errors. Landed at
  `~/.local/share/powershell/Modules/PSScriptAnalyzer/1.25.0/`.
- Module on disk: **286 MB**. The bulk is `compatibility_profiles/` (the JSON
  databases that power `PSUseCompatibleCmdlets` / `...Types`). The core DLLs
  (`Microsoft.Windows.PowerShell.ScriptAnalyzer.dll`,
  `...BuiltinRules.dll`, `Microsoft.PowerShell.CrossCompatibility.dll`) are
  ~5 MB combined.
- `Get-ScriptAnalyzerRule | Measure-Object` → **75** built-in rules. All load
  without errors on non-Windows; no Windows-only assumptions in the default
  ruleset.

### Functional check against repo fixtures

Fixtures in `tree-sitter-powershell/test/fixtures/`. Sample results:

| Fixture            | Diagnostics | Example rules fired                              |
| ------------------ | ----------: | ------------------------------------------------ |
| `EchoArgs.ps1`     |           7 | `PSPossibleIncorrectComparisonWithNull`, `PSAvoidUsingCmdletAliases`, `PSUseBOMForUnicodeEncodedFile` |
| `PSService.ps1`    |          13 | `PSUseShouldProcessForStateChangingFunctions`, `PSAvoidUsingPlainTextForPassword`, `PSAvoidAssignmentToAutomaticVariable`, `PSUseDeclaredVarsMoreThanAssignments` |
| All 9 fixtures     |         435 | (mixed, single `pwsh` invocation)                |

Diagnostics include line + column + rule name + severity + message — the exact
shape we need for `internal/lint` violation records.

### Output is structured and JSON-safe

`Invoke-ScriptAnalyzer ... | ConvertTo-Json -Depth 3` produces:

```json
{
  "RuleName": "PSPossibleIncorrectComparisonWithNull",
  "Severity": 1,
  "Line": 50,
  "Column": 7,
  "Message": "$null should be on the left side of equality comparisons.",
  "ScriptPath": "/abs/path/EchoArgs.ps1"
}
```

Severity is an integer enum: `0 = Information`, `1 = Warning`, `2 = Error`,
`3 = ParseError`. `Line` / `Column` are nullable (some rules, e.g.
`PSUseBOMForUnicodeEncodedFile`, target the whole file). Directly consumable
by Go with `encoding/json/v2` — no custom parsing.

### Timing

| Scenario                                                        | Wall time |
| --------------------------------------------------------------- | --------: |
| Cold `pwsh -NoProfile` + `Import-Module` + 1 fixture (3 runs)   | 0.72–0.79 s |
| All 9 fixtures analyzed inside **one** `pwsh` invocation (435 diagnostics) | 7.08 s total |

The ~0.75 s is dominated by `Import-Module PSScriptAnalyzer`. Amortized
across a session it's free; per-file `pwsh` spawn is a non-starter for
LSP-scale use.

## What we verified on Windows (2026-04-30)

Host: `Microsoft Windows 10.0.26200` from PowerShell's `$PSVersionTable.OS`,
`pwsh 7.6.x`, and Windows PowerShell `5.1.26100.7462`.

### Host discovery and environment

- `pwsh.exe` was available at
  `C:\Program Files\PowerShell\7\pwsh.exe`.
- `powershell.exe` was available at
  `C:\WINDOWS\System32\WindowsPowerShell\v1.0\powershell.exe`, but it is not a
  viable sidecar host for tally's PowerShell-in-Dockerfile target.
- In the Codex desktop process environment, `WINDIR`, `SystemRoot`,
  `APPDATA`, and `LOCALAPPDATA` were missing even though `PATH`, `TEMP`,
  `TMP`, and `USERPROFILE` were present.
- With that sanitized environment, Windows PowerShell 5.1 failed before
  executing user code:

```text
Internal Windows PowerShell error. Loading managed Windows PowerShell failed
with error 8009001d.
```

- Setting `WINDIR=C:\WINDOWS` and `SystemRoot=C:\WINDOWS` fixed 5.1 startup.
  For module bootstrap, setting `APPDATA=%USERPROFILE%\AppData\Roaming` and
  `LOCALAPPDATA=%USERPROFILE%\AppData\Local` also avoided local module tooling
  failures.

This remains useful as an environment lesson: the Go runner should not blindly
inherit a stripped Windows environment. Even though `powershell.exe` is not a
supported analyzer host, the runner and bootstrap path should preserve or
repair the standard Windows profile/app-data variables before spawning `pwsh`.

### Install and import

`Install-Module -Name PSScriptAnalyzer -Scope CurrentUser -Force` failed in
the stripped environment because PowerShellGet had no registered repositories.
The modern PSResourceGet path worked after repairing the app-data variables:

```text
Install-PSResource -Name PSScriptAnalyzer -Scope CurrentUser \
  -TrustRepository -Reinstall -Quiet
```

Installed module:

```text
C:\Users\tino\Documents\PowerShell\Modules\PSScriptAnalyzer\1.25.0
```

Measured size: **285.5 MiB**, 50 files. This matches the macOS result: the
module is too large to vendor casually.

PowerShell 7 found the module immediately and `Get-ScriptAnalyzerRule`
returned **75** rules.

### Sidecar protocol smoke test

A minimal `pwsh` 7 sidecar script was executed with newline-delimited JSON
requests over stdio:

- handshake line: `{"ready":true,"version":"1.25.0","ps":"..."}`
- file-backed `Invoke-ScriptAnalyzer -Path <Bad.ps1>`
- in-memory `Invoke-ScriptAnalyzer -ScriptDefinition <source>`
- shutdown request

PowerShell 7 succeeded. Example file-backed diagnostics included:

- `PSAvoidUsingWriteHost`
- `PSPossibleIncorrectComparisonWithNull`
- `PSUseApprovedVerbs`
- `PSAvoidUsingPlainTextForPassword`
- `PSReviewUnusedParameter`
- `PSAvoidUsingCmdletAliases`

The sidecar returned compact JSON for the ready handshake, both analysis
requests, and shutdown. File-backed diagnostics included line, column, rule
name, integer severity, message, and Windows `ScriptPath`. `ScriptDefinition`
diagnostics returned line/column data and an empty `ScriptPath`, as expected.

### PowerShell 7 syntax compatibility

tally's Windows container model treats PowerShell-in-Dockerfile snippets as
`pwsh` 7.x syntax. That target dialect must be separate from the host process
used to run PSScriptAnalyzer.

Probe source:

```powershell
param([string]$Name)
$value = $Name ?? 'fallback'
$mode = $value -eq 'fallback' ? 'defaulted' : 'provided'
Write-Output $mode && Write-Output 'continued'
```

Results:

| Host | Result |
| --- | --- |
| `pwsh 7.6.1` + PSScriptAnalyzer 1.25.0 | Analyzed successfully with no diagnostics; `Invoke-Formatter` preserved valid syntax. |
| Windows PowerShell 5.1 + PSScriptAnalyzer 1.25.0 | Reported parse errors for `??`, ternary `? :`, and `&&`; also misinterpreted `?` as the `Where-Object` alias. |

Conclusion: Windows PowerShell 5.1 must not be used as a host fallback for
PSScriptAnalyzer in tally. A 5.1 host runs the 5.1 parser and cannot reliably
analyze the `pwsh` 7.x syntax that tally supports inside Dockerfiles.

### Timing

Simple cold-process timings for `pwsh.exe -NoProfile` plus
`Import-Module PSScriptAnalyzer` and `Get-ScriptAnalyzerRule` on this Windows
host: **3.03 s, 2.07 s, 1.66 s**.

These numbers reinforce the long-lived sidecar requirement. Per-file process
startup would be too expensive; startup once per tally session is acceptable.

## Why not the alternatives

### Not WASM

- PowerShell's engine does process creation and dynamic code generation via
  `System.Management.Automation`. Both are prohibited in browser WASM.
- Microsoft's own guidance (MS Q&A forum thread, `.NET 8` Blazor WASM): *"WASM
  cannot host PowerShell. Under the covers PowerShell.Create() uses create
  process, which is not allowed by the WASM sandbox."*
- WASI has filesystem and clock access, but still no ability to load
  assemblies dynamically or run the PowerShell parser's runtime-generated
  types. No functional port exists.
- The `net*-browser` TFM compiles trivial .NET code to WASM, but
  `Microsoft.PowerShell.SDK` is not on the supported-for-AOT/trimming list
  and is heavily reflection-driven.

### Not a Go ↔ PSScriptAnalyzer native library bridge

- No NuGet package exposes PSScriptAnalyzer as a standalone analysis library.
  The PowerShell Gallery `.nupkg` bundles the DLLs but ships the module, not a
  dev SDK. Extracting `Microsoft.Windows.PowerShell.ScriptAnalyzer.dll` is
  possible but unsupported.
- The documented C# API (`Microsoft.Windows.PowerShell.ScriptAnalyzer.ScriptAnalyzer`)
  requires a live `Runspace` — constructing one pulls in
  `Microsoft.PowerShell.SDK` (~80–100 MB of dependencies). Upstream issue
  [PSScriptAnalyzer#1056] asks for an AST-only entry point; it's open and
  unimplemented.
- Many built-in rules are `.psm1` scripts executed inside the runspace. You
  cannot strip the engine and keep the ruleset.
- CLR in-process hosting from Go via `hostfxr.dll` is technically feasible on
  Windows (and works on macOS/Linux through `libhostfxr.dylib` / `.so`), but:
  - Requires cgo → loses Go cross-compilation.
  - Ships the .NET runtime + `Microsoft.PowerShell.SDK` payload anyway.
  - Managed exceptions unwinding through a Go goroutine stack are nasty.
  - You've built a sidecar either way — it's just now in-process.

### Not `dotnet tool install` / native AOT

- `PublishAot` explicitly doesn't support `Microsoft.PowerShell.SDK`. No
  single-file, trimmed, CLR-less binary.
- `PublishSingleFile=true --self-contained` produces a ~100 MB bundle per RID.
  This is the *minimum* distributable form of "PowerShell + PSScriptAnalyzer",
  and it is in fact a flavor of sidecar — just pre-packaged.

## Proposed design

### Architecture

```text
┌─────────────────────┐        JSON-over-stdio        ┌───────────────────────┐
│ tally Go process    │ ───────── request ─────────► │ pwsh sidecar          │
│ internal/psanalyzer │                                │   Import-Module PSSA  │
│   Runner            │ ◄──────── response ──────────│   Invoke-ScriptAnalyzer│
└─────────────────────┘                                └───────────────────────┘
```

One long-lived `pwsh` 7.x subprocess. Tally writes newline-delimited JSON
requests to stdin; the sidecar responds with newline-delimited JSON objects on
stdout. Stderr is reserved for fatal errors and log diagnostics (logged at
`debug` level by tally).

This matches the single-instance contract we already enforce for the
ShellCheck reactor: one init, many checks, one teardown on shutdown.

### Package layout (proposed)

```text
internal/
  psanalyzer/
    runner.go          # Go-side Runner: spawn, framing, lifecycle
    runner_test.go
    protocol.go        # request/response types (encoding/json/v2)
    sidecar/
      Tally.PSSA.Sidecar.ps1   # or .psm1 — the host script
      install.ps1              # optional: bootstrap module on first run
```

Only the Go side is linked into the tally binary. The `.ps1` sidecar is
embedded via `//go:embed` and written to a temp dir at first run.

### Sidecar protocol (first cut)

Request:

```json
{"id":"42","op":"analyze","path":"/abs/path/foo.ps1",
 "scriptDefinition":null,
 "settings":{"includeRules":[],"excludeRules":["PSAvoidUsingWriteHost"],
             "severity":["Error","Warning"]}}
```

- `path` **or** `scriptDefinition` — the latter lets tally pipe the script
  body directly without a temp file (needed for Dockerfile heredocs and LSP
  unsaved buffers).
- `settings` maps 1:1 to `Invoke-ScriptAnalyzer`'s settings hashtable.

Response:

```json
{"id":"42","ok":true,"diagnostics":[
  {"ruleName":"PSUseApprovedVerbs","severity":1,"line":12,"column":10,
   "message":"...","scriptPath":"/abs/path/foo.ps1"}
]}
```

Errors in `{"id":"42","ok":false,"error":"..."}` shape. Parse errors surface
as regular diagnostics with `severity:3`.

### Lifecycle

1. `NewRunner()` — no work yet; lazy.
2. First `Analyze()` call:
   - Locate `pwsh` 7.x on `PATH` (configurable via `TALLY_POWERSHELL` env +
     tally config).
   - Write `Tally.PSSA.Sidecar.ps1` to a per-session temp dir.
   - Spawn `pwsh -NoProfile -NonInteractive -File <script>`.
   - Read a `{"ready":true,"version":"1.25.0"}` handshake line (or bail with
     timeout).
3. Each subsequent call: write request, read response. Mutex-serialized —
   the sidecar handles one request at a time.
4. On `Close()` or process exit, send `{"op":"shutdown"}` and wait for the
   sidecar to drain; kill after timeout.

### Sidecar startup

```powershell
# Tally.PSSA.Sidecar.ps1 (abbreviated)
$ErrorActionPreference = 'Stop'
Import-Module PSScriptAnalyzer
[Console]::Out.WriteLine((@{ready=$true; version=(Get-Module PSScriptAnalyzer).Version.ToString()} | ConvertTo-Json -Compress))

while (($line = [Console]::In.ReadLine()) -ne $null) {
    $req = $line | ConvertFrom-Json
    try {
        $params = @{}
        if ($req.path)             { $params['Path']             = $req.path }
        if ($req.scriptDefinition) { $params['ScriptDefinition'] = $req.scriptDefinition }
        if ($req.settings)         { $params['Settings']         = $req.settings }
        $diags = Invoke-ScriptAnalyzer @params
        $resp  = @{id=$req.id; ok=$true; diagnostics=$diags}
    } catch {
        $resp  = @{id=$req.id; ok=$false; error=$_.Exception.Message}
    }
    [Console]::Out.WriteLine(($resp | ConvertTo-Json -Compress -Depth 5))
}
```

Real implementation will bucket parse errors separately, map severity enum
values explicitly, and pre-resolve rule names so invalid rule filters fail at
handshake rather than on first request.

### Discovery / bootstrap

Two levers:

| Concern                               | Strategy |
| ------------------------------------- | -------- |
| `pwsh` 7.x missing on PATH            | `tally doctor` surfaces it; lint flags `.ps1` files as "analyzer not available, pass `--no-psanalyzer` to silence" |
| PSScriptAnalyzer module not installed | Sidecar attempts PSResourceGet/PowerShellGet install on handshake failure, guarded by `--allow-module-install` config |
| Sanitized Windows environment         | Runner ensures standard Windows env vars are present before spawning `pwsh`; bootstrap also repairs `APPDATA` / `LOCALAPPDATA` when absent |

We do **not** bundle the 286 MB module in the tally release. The user's
`pwsh` already has access to PowerShell Gallery; first-run bootstrap is the
reasonable default.

## Platform notes

### macOS / Linux

- `pwsh` is a supplementary install (brew / apt / dnf / package from
  Microsoft). Tally detects it; if absent, PowerShell linting is a no-op
  with a clear diagnostic.
- The architecture above is the whole story — verified working on this
  machine today.

### Windows

- `pwsh` 7.x is required for PowerShell analysis on Windows hosts. It is not
  enough for `powershell.exe` 5.1 to be present.
- Windows PowerShell 5.1 is out of scope for both sides of this feature:
  tally's Dockerfile PowerShell target is `pwsh` 7.x syntax, and a 5.1 host
  cannot reliably parse that syntax through PSScriptAnalyzer.
- The runner should still repair standard Windows environment variables before
  spawning `pwsh`, because stripped environments can break module bootstrap and
  other Windows process behavior.
- Named-pipe IPC (`\\.\pipe\PSHost.*`) via `github.com/Microsoft/go-winio` is
  available if stdio framing becomes a bottleneck. Not proposed for v1;
  stdio keeps the codebase identical across OSes.

### Dockerfile context

- Inside a Dockerfile `RUN pwsh -Command '...'` body, tally already extracts
  the shell command. For PowerShell dialects we'd feed the command body as
  `scriptDefinition` to the sidecar. Same path as ShellCheck for `RUN` bash
  blocks.
- `SHELL ["pwsh", "-Command"]` flips the default shell for subsequent `RUN`s;
  tally's `internal/facts` already tracks `SHELL` for shellcheck dialect
  selection — extend that to route to psanalyzer when shell resolves to
  `pwsh` / `powershell`.

## Risks and open questions

1. **Module bootstrap UX.** 286 MB download on first use is a surprise.
   Options: (a) prompt / require explicit flag, (b) detect and print a clear
   message, (c) vendor a smaller subset. Recommended: start with (b),
   revisit if users complain. On Windows, prefer PSResourceGet when available;
   PowerShellGet can fail if repository state or app-data env vars are broken.
2. **Severity mapping.** PSScriptAnalyzer severity 0..3 vs tally's internal
   `error`/`warning`/`info`/`style`. We need to decide on one canonical
   mapping and document it in `_docs/`.
3. **Rule coverage vs. our own rules.** tally has Dockerfile-flavored rules;
   PSScriptAnalyzer has PowerShell-flavored ones. Where they overlap (e.g. a
   `CMD`/`ENTRYPOINT` that invokes `pwsh` with `-EncodedCommand`), decide
   whether to dedupe at the tally layer or emit both.
4. **Fix suggestions.** `SuggestedCorrections` are available on some rules —
   worth exposing through tally's `FixSafe` / `FixSuggestion` levels, but
   needs per-rule safety review.
5. **Windows environment normalization.** The runner should preserve or repair
   standard Windows environment variables before spawning `pwsh`, especially
   for module bootstrap paths that rely on user profile app-data locations.
6. **Sidecar crash recovery.** A ParseError or a rule with a bug can throw
   inside the runspace. The sidecar catches per-request; we should also
   detect sidecar-died-mid-request and respawn once with backoff.
7. **Goroutine concurrency.** PSScriptAnalyzer is not thread-safe inside a
   single Runspace. Our Runner serializes — acceptable for LSP/CLI, would
   need a pool for batch throughput (defer until we see it).

## Next steps

- [ ] Prototype `internal/psanalyzer/runner.go` + embedded sidecar script.
- [x] Reproduce the sidecar smoke test on a Windows host with `pwsh` 7.
- [x] Verify that Windows PowerShell 5.1 cannot host analysis for `pwsh` 7
      syntax and remove it as a supported fallback.
- [ ] Add Windows-specific runner tests for environment repair and rejection
      of non-`pwsh` hosts.
- [ ] Wire a `--powershell` enable flag into `cmd/tally/cmd/lint.go`.
- [ ] Decide severity mapping and document in `_docs/rules/powershell/`.
- [ ] Add integration fixture under `internal/integration/testdata/`
      covering `RUN pwsh -Command` and a `SHELL ["pwsh"]` form.
- [ ] Snapshot tests for the CLI output shape.

## References

- Microsoft Learn — "Using PSScriptAnalyzer", ScriptAnalyzer as a .NET library
  section: documents the `Initialize`/`AnalyzePath`/`GetRule` surface and its
  Runspace requirement.
- PowerShell Gallery — `PSScriptAnalyzer 1.25.0` (what we installed).
- GitHub `PowerShell/PSScriptAnalyzer#1056` — ongoing feature request to
  expose a hosted-scenario C# API that accepts a pre-parsed AST.
- MS Q&A — "Blazor WASM to execute PowerShell script" — authoritative "no,
  PowerShell cannot run in the WASM sandbox".
- `shellcheck-reactor-single-instance.md` — architectural
  precedent for the long-lived-instance pattern we're mirroring here.
