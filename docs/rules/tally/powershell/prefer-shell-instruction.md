# tally/powershell/prefer-shell-instruction

Prefer a `SHELL` instruction over repeating `pwsh` or `powershell` wrappers in `RUN`.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Enabled (experimental) |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

This rule detects repeated shell-form `RUN` instructions that invoke PowerShell explicitly, for example:

- `RUN pwsh -Command ...`
- `RUN powershell -Command ...`
- `RUN @powershell -Command ...`

When PowerShell is already the effective shell via a preceding `SHELL [...]` instruction, the rule does nothing.

The recommendation applies on both Windows and Linux. A Linux image such as `mcr.microsoft.com/powershell:ubuntu-22.04` still benefits from
switching to a PowerShell `SHELL` once multiple PowerShell `RUN` commands appear.

## Why this matters

Repeating the full wrapper on every `RUN` line adds noise and makes PowerShell-specific defaults easy to forget. A dedicated `SHELL` instruction:

- makes repeated PowerShell build steps easier to read
- centralizes the shell choice instead of duplicating it across `RUN`s
- lets tally inject sane build defaults once:
  - `$ErrorActionPreference = 'Stop'`
  - `$ProgressPreference = 'SilentlyContinue'`

## Examples

### Before (violation)

```dockerfile
FROM mcr.microsoft.com/powershell:ubuntu-22.04

RUN pwsh -NoLogo -NoProfile -Command Install-Module PSReadLine -Force
ENV POWERSHELL_TELEMETRY_OPTOUT=1
RUN pwsh -NoLogo -NoProfile -Command Invoke-WebRequest https://example.com/tools.zip -OutFile /tmp/tools.zip
```

### After (fixed with `--fix --fix-unsafe`)

```dockerfile
FROM mcr.microsoft.com/powershell:ubuntu-22.04

SHELL ["pwsh", "-NoLogo", "-NoProfile", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
RUN Install-Module PSReadLine -Force
ENV POWERSHELL_TELEMETRY_OPTOUT=1
RUN Invoke-WebRequest https://example.com/tools.zip -OutFile /tmp/tools.zip
```

### Windows example

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command Invoke-WebRequest https://example.com/file.zip -OutFile C:\temp\file.zip
RUN powershell -Command Expand-Archive C:\temp\file.zip -DestinationPath C:\tools
```

becomes:

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["powershell", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
RUN Invoke-WebRequest https://example.com/file.zip -OutFile C:\temp\file.zip
RUN Expand-Archive C:\temp\file.zip -DestinationPath C:\tools
```

## Configuration

This rule has no rule-specific options today.

```toml
[rules.tally."powershell/prefer-shell-instruction"]
severity = "style"
```

## Fix behavior

The fixer is intentionally conservative. It only rewrites repeated PowerShell wrappers when the repeated `RUN` instructions share the same executable
and the same arguments before `-Command` (for example, repeated `pwsh -NoProfile -Command ...`).

## References

- [PowerShell Docker image](https://mcr.microsoft.com/en-us/product/powershell/about)
- [Windows and PowerShell Rules design notes](../../../../design-docs/27-windows-container-rules.md)
