# tally/windows/no-stopsignal

`STOPSIGNAL` has no effect on Windows containers because they do not support POSIX signals.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | Yes (comments out the instruction) |

## Description

Windows containers do not support POSIX signals. BuildKit defines a `CheckPlatform()` method on
`StopSignalCommand` that rejects it on Windows, but this check is **never called** in the dispatch
path (dead code). The `STOPSIGNAL` instruction is silently accepted and written to the image config,
but has no effect at runtime.

This rule catches the useless instruction at lint time so authors can remove it or understand that
it will be ignored.

## Why this matters

- **Silent no-op** — the build succeeds but the instruction does nothing on Windows
- **Misleading config** — other maintainers may assume the signal is in effect
- **Dead code in BuildKit** — the platform check exists but is never called, so there is no
  build-time warning from Docker itself

## Examples

### Violation

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022
STOPSIGNAL SIGTERM
CMD ["myapp.exe"]
```

### No violation

```dockerfile
# Linux stages can use STOPSIGNAL normally
FROM alpine:3.20
STOPSIGNAL SIGTERM
CMD ["myapp"]

# Windows stages without STOPSIGNAL are fine
FROM mcr.microsoft.com/windows/servercore:ltsc2022
CMD ["myapp.exe"]
```

## Auto-fix

The auto-fix comments out the instruction using the standard tally comment-out pattern:

```dockerfile
# Before:
STOPSIGNAL SIGTERM

# After:
# [commented out by tally - STOPSIGNAL has no effect on Windows containers]: STOPSIGNAL SIGTERM
```

## Related rules

- [`tally/no-ungraceful-stopsignal`](../no-ungraceful-stopsignal.md) — checks the signal value on
  Linux stages (skips Windows stages since this rule handles them)
- [`tally/windows/no-run-mounts`](./no-run-mounts.md) — another Windows-specific correctness rule

## Configuration

This rule has no rule-specific options.

```toml
[rules.tally."windows/no-stopsignal"]
severity = "warning"
```

## References

- [Optimize Windows Dockerfiles](https://learn.microsoft.com/en-us/virtualization/windowscontainers/manage-docker/optimize-windows-dockerfile)
- [Windows and PowerShell Rules design notes](../../../../design-docs/27-windows-container-rules.md)
