# tally/windows/no-chown-flag

`COPY --chown` and `ADD --chown` are silently ignored on Windows containers.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | Yes (removes the `--chown` flag) |

## Description

Windows containers do not use POSIX file ownership (uid:gid). The `--chown` flag on `COPY` and
`ADD` instructions is silently ignored when building for Windows — BuildKit accepts the flag
without error, but the resulting files have no ownership change applied.

Users who add `--chown=user:group` on a Windows stage expect ownership to be set, but the flag
has no effect. This rule catches the dead flag at lint time so authors can remove it or understand
that it is a no-op.

## Why this matters

- **Silent no-op** — the build succeeds but `--chown` does nothing on Windows
- **Misleading intent** — other maintainers may assume file ownership is being managed
- **Cross-platform confusion** — multi-stage Dockerfiles with both Linux and Windows stages may
  copy patterns from Linux stages where `--chown` is meaningful

## Examples

### Violation

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022

# --chown is silently ignored on Windows
COPY --chown=ContainerUser app/ C:/app/
ADD --chown=1000:1000 config.tar.gz C:/config/
```

### After fix (`--fix`)

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022

# --chown removed — it had no effect
COPY app/ C:/app/
ADD config.tar.gz C:/config/
```

### No violation

```dockerfile
# Linux stages can use --chown normally
FROM alpine:3.20
COPY --chown=app:app . /app/

# Windows stages without --chown are fine
FROM mcr.microsoft.com/windows/servercore:ltsc2022
COPY app/ C:/app/
```

## Related rules

- [`tally/copy-after-user-without-chown`](../copy-after-user-without-chown.md) — suggests adding
  `--chown` on Linux stages after a non-root `USER` (complementary; fires on opposite condition)
- [`tally/windows/no-stopsignal`](./no-stopsignal.md) — another Windows-specific correctness rule
  for silently ignored instructions
- [`tally/windows/no-run-mounts`](./no-run-mounts.md) — Windows-specific correctness rule for
  unsupported `RUN --mount` flags

## Configuration

This rule has no rule-specific options.

```toml
[rules.tally."windows/no-chown-flag"]
severity = "warning"
```

## References

- [Optimize Windows Dockerfiles](https://learn.microsoft.com/en-us/virtualization/windowscontainers/manage-docker/optimize-windows-dockerfile)
- [Windows and PowerShell Rules design notes](../../../../design-docs/27-windows-container-rules.md)
