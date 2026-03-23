# tally/windows/no-run-mounts

`RUN --mount` flags are not supported on Windows containers and will fail at runtime.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | No |

## Description

All `RUN --mount` types (`cache`, `secret`, `ssh`, `bind`, `tmpfs`) fail at runtime on Windows containers.
BuildKit's Dockerfile frontend has no platform guard for mount flags — `dispatchRunMounts()` processes every
mount type identically regardless of OS. The build starts successfully, pulls the base image, and begins
executing layers, only to fail at the containerd/HCS runtime layer when the mount is set up.

On large Windows images (5+ GB base layers), this means the user may wait minutes before hitting the error.
This rule catches the problem immediately at lint time.

## Why this matters

- **Guaranteed build failure** — this is not a style issue; the build *will* break
- **Late failure** — BuildKit validates mounts without error; the failure only surfaces at container runtime
- **Expensive retry** — Windows base image pulls are large (ServerCore ~5 GB); catching early saves minutes

## Examples

### Violation

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022

# cache mount: fails at HCS runtime (moby/buildkit#5678)
RUN --mount=type=cache,target=C:\Users\ContainerUser\.nuget\packages dotnet restore

# secret mount: tmpfs-based secrets unsupported (moby/buildkit#5273)
RUN --mount=type=secret,id=nuget_token cmd /C type C:\run\secrets\nuget_token

# ssh mount: Unix socket forwarding unavailable (moby/buildkit#4837)
RUN --mount=type=ssh git clone git@github.com:org/repo.git
```

### No violation

```dockerfile
# Linux stages can use mounts normally
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
RUN --mount=type=cache,target=/root/.nuget/packages dotnet restore

# Windows stages without mounts are fine
FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command Invoke-WebRequest https://example.com/file.zip -OutFile C:\temp\file.zip
```

## Affected mount types

| Mount type | BuildKit issue | Runtime behavior |
|------------|---------------|-----------------|
| `--mount=type=cache` | moby/buildkit#5678 | HCS error setting up cache volume |
| `--mount=type=secret` | moby/buildkit#5273 | tmpfs-based secrets not supported |
| `--mount=type=ssh` | moby/buildkit#4837 | Unix socket forwarding unavailable |
| `--mount=type=bind` | — | Bind mount semantics differ on HCS |
| `--mount=type=tmpfs` | — | tmpfs not a Windows concept |

## Configuration

This rule has no rule-specific options.

```toml
[rules.tally."windows/no-run-mounts"]
severity = "error"
```

## References

- [Optimize Windows Dockerfiles](https://learn.microsoft.com/en-us/virtualization/windowscontainers/manage-docker/optimize-windows-dockerfile)
- [Windows and PowerShell Rules design notes](../../../../design-docs/27-windows-container-rules.md)
