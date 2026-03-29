# tally/copy-after-user-without-chown

COPY/ADD without --chown after USER creates root-owned files.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | Yes (two alternatives) |

## Description

Docker's `COPY` and `ADD` instructions always create files owned by `root:root`
(UID 0, GID 0), regardless of the active `USER` instruction. This is a common
source of confusion: authors set `USER nonroot` and then expect subsequent
`COPY`/`ADD` to create files owned by that user.

This rule detects `COPY` or `ADD` instructions that follow a non-root `USER`
without an explicit `--chown` flag. The mismatch means the non-root process may
not be able to write to or modify the copied files at runtime.

## Suppression

The rule is automatically suppressed when:

- The `COPY`/`ADD` instruction already has `--chown` set.
- The effective `USER` at that point in the stage is root (or no `USER` has been
  set).
- A subsequent `RUN chown` command in the same stage targets the same
  destination path (or a parent directory), indicating the author is managing
  ownership explicitly.

## Cross-stage inheritance

The rule walks the `FROM <stage>` ancestry chain. If a parent stage sets a
non-root `USER` that flows into a child stage via `FROM`, `COPY`/`ADD` in the
child stage without `--chown` will trigger the rule.

## Relationship to other rules

| | `tally/copy-after-user-without-chown` | `tally/prefer-copy-chmod` |
|---|---|---|
| Fires when | COPY/ADD after non-root USER without --chown | COPY followed by RUN chmod |
| Fix | Adds `--chown=<user>` or moves USER | Merges into `COPY --chmod` |
| Scope | All stages | All stages |

Both rules can fire on the same `COPY` instruction. Their fixes compose
correctly: the result is `COPY --chown=user --chmod=mode file /dest`.

## Auto-fix

Two fix alternatives are offered:

1. **Add `--chown=<user>`** (preferred, safe): inserts `--chown=<user>` to
   match the active `USER`, fixing the ownership mismatch directly.

2. **Move `USER` after COPY/ADD** (safe): relocates the `USER` instruction to
   just before the first `RUN` or `WORKDIR` that follows. This is a semantic
   no-op because `COPY`/`ADD` ownership is always `root:root` regardless of
   `USER`. It clarifies that `USER` only affects `RUN`, `WORKDIR`, and runtime
   identity. This alternative is only offered when no `RUN` or `WORKDIR` exists
   between the `USER` and the `COPY`/`ADD`.

## References

- [Dockerfile reference -- USER](https://docs.docker.com/reference/dockerfile/#user)
- [Dockerfile reference -- COPY --chown](https://docs.docker.com/reference/dockerfile/#copy---chown)
- [Dockerfile reference -- ADD --chown](https://docs.docker.com/reference/dockerfile/#add---chown)
- [Docker Blog -- Understanding the Docker USER Instruction](https://www.docker.com/blog/understanding-the-docker-user-instruction/)

## Examples

### Bad

```dockerfile
# COPY after USER without --chown: files are root-owned
FROM ubuntu:22.04
RUN useradd -r appuser
USER appuser
COPY app /app
CMD ["/app"]
```

```dockerfile
# ADD after USER without --chown
FROM ubuntu:22.04
USER 1000:1000
ADD config.tar.gz /etc/app/
RUN setup.sh
```

### Good

```dockerfile
# Explicit --chown matches the active USER
FROM ubuntu:22.04
RUN useradd -r appuser
USER appuser
COPY --chown=appuser app /app
CMD ["/app"]
```

```dockerfile
# USER placed after COPY, before RUN (clarifies intent)
FROM ubuntu:22.04
RUN useradd -r appuser
COPY app /app
USER appuser
RUN setup.sh
CMD ["/app"]
```

```dockerfile
# Ownership managed via RUN chown (suppressed)
FROM ubuntu:22.04
RUN useradd -r appuser
USER appuser
COPY app /app
RUN chown -R appuser:appuser /app
CMD ["/app"]
```

```dockerfile
# Numeric UID with --chown
FROM ubuntu:22.04
USER 1000:1000
COPY --chown=1000:1000 app /app
CMD ["/app"]
```

## Configuration

```toml
[rules.tally.copy-after-user-without-chown]
severity = "warning"  # Options: "off", "error", "warning", "info", "style"
```
