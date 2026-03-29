# tally/prefer-systemd-sigrtmin-plus-3

systemd/init containers should use STOPSIGNAL SIGRTMIN+3 for clean shutdown.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | Yes (safe) |

## Description

When a container runs `systemd` or `/sbin/init` as PID 1, the container
runtime's default stop signal (`SIGTERM`, signal 15) does not trigger a clean
shutdown. systemd interprets `SIGTERM` as an "isolate to rescue mode" request,
not a halt.

The correct signal is `SIGRTMIN+3`, which tells systemd to perform a clean
manager shutdown — analogous to running `systemctl halt` inside the container.

This rule fires when:

- the stage's effective PID 1 is a recognized systemd/init binary, **and**
- `STOPSIGNAL` is either missing or set to a signal other than `SIGRTMIN+3`.

### Recognized systemd/init executables

- `/sbin/init`
- `/usr/sbin/init`
- `/lib/systemd/systemd`
- `/usr/lib/systemd/systemd`
- `systemd` (bare name, with or without arguments)

### When the rule does not fire

- **Shell-form** `ENTRYPOINT` or `CMD` — the shell becomes PID 1, not systemd,
  so the signal mapping is unreliable.
- **Non-systemd** executables (nginx, postgres, etc.) — other daemon-specific
  rules handle those.
- **Windows stages** — `STOPSIGNAL` has no effect on Windows containers.
- **Environment variable** signals (e.g. `STOPSIGNAL $MY_SIGNAL`) — the value
  cannot be determined statically.

## References

- [Dockerfile reference -- STOPSIGNAL](https://docs.docker.com/reference/dockerfile/#stopsignal)
- [systemd signals — systemd(1)](https://www.freedesktop.org/software/systemd/man/latest/systemd.html#Signals)

## Examples

### Bad

```dockerfile
FROM fedora:40
# Default SIGTERM makes systemd switch to rescue mode, not shut down
ENTRYPOINT ["/sbin/init"]
```

```dockerfile
FROM centos:stream9
# SIGTERM is wrong for systemd
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/sbin/init"]
```

```dockerfile
FROM fedora:40
# SIGKILL prevents any cleanup
STOPSIGNAL SIGKILL
CMD ["/usr/lib/systemd/systemd"]
```

### Good

```dockerfile
FROM fedora:40
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/sbin/init"]
```

```dockerfile
FROM almalinux:9
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/usr/lib/systemd/systemd", "--system"]
```

## Auto-fix

The rule provides two fix modes depending on the violation:

**Wrong signal** — replaces the signal token with `SIGRTMIN+3`:

```bash
tally lint --fix Dockerfile
```

**Missing STOPSIGNAL** — inserts `STOPSIGNAL SIGRTMIN+3` before the
ENTRYPOINT/CMD instruction:

```bash
tally lint --fix Dockerfile
```

Both fixes use `FixSafe` safety because the PID 1 process is confirmed as
systemd/init and `SIGRTMIN+3` is the definitively correct signal.

## Cross-rule interactions

- **tally/prefer-canonical-stopsignal**: Handles `RTMIN+3` → `SIGRTMIN+3`
  normalization. This rule checks the normalized value, so `RTMIN+3` is
  accepted as correct.
- **tally/no-ungraceful-stopsignal**: Both may fire on the same `STOPSIGNAL`
  (e.g. `SIGKILL` on a systemd stage). This rule's fix takes precedence,
  replacing with `SIGRTMIN+3` instead of the generic `SIGTERM`.

## Configuration

```toml
[rules.tally.prefer-systemd-sigrtmin-plus-3]
severity = "warning"  # Options: "off", "error", "warning", "info", "style"
```
