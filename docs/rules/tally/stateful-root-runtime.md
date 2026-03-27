# tally/stateful-root-runtime

Final stage runs as root and signals mutable/persistent state.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Security |
| Default | Enabled |
| Auto-fix | No |

## Description

This rule detects Dockerfiles where the final stage runs as root (either
explicitly via `USER root`/`USER 0` or implicitly by having no `USER`
instruction) **and** the stage positively signals mutable or persistent
state through `VOLUME` instructions or data/state directory patterns.

Running as root in a container that manages persistent state is a
higher-risk combination than running root without state. Root access over
writable volumes, mounted sockets, log directories, and database storage
increases the blast radius of a compromise: an attacker can corrupt
persistent data, tamper with host-mounted files, or escalate via
root-owned resources that outlive the container.

This rule is more targeted than `hadolint/DL3002` ("last USER should not be
root"). DL3002 warns whenever `USER root` appears, regardless of what the
container does. This rule only fires when root **intersects** with mutable
state.

## Stateful signals detected

- **`VOLUME`** instructions (highest confidence)
- **`WORKDIR`** paths matching data/state directories
- **`COPY`/`ADD`** destinations to data/state directories
- **`RUN mkdir`** creating data/state directories

Data/state directory patterns: `/data`, `/srv`, `/var/lib/*`, `/var/log/*`,
`/var/cache/*`, `/var/run/*`, `/var/spool/*`.

## Suppression

The rule is automatically suppressed when:

- A **privilege-drop tool** is referenced in ENTRYPOINT or CMD (`gosu`,
  `su-exec`, `suexec`, `setpriv`). These are unambiguous privilege-drop
  executables. Generic script names like `docker-entrypoint.sh` or
  `entrypoint.sh` are **not** treated as suppression signals because they
  could do anything without inspecting their content.
- The **base image is known to default to non-root**: Distroless `:nonroot`
  tags, Chainguard/cgr.dev images.
- The **effective final `USER`** is non-root.
- The final stage **inherits from a local stage** whose last `USER` is
  non-root.

## Relationship to hadolint/DL3002

| | `hadolint/DL3002` | `tally/stateful-root-runtime` |
|---|---|---|
| Fires when | Last `USER` is explicitly `root` | Effective user is root (explicit or implicit) **and** stateful signal exists |
| Scope | Any root USER in final stage | Root + state combination only |
| Privilege-drop aware | No | Yes (suppresses for gosu/su-exec patterns) |
| Non-root base aware | N/A (only checks explicit USER) | Yes (suppresses for distroless:nonroot, chainguard) |

The two rules are complementary and both may fire on the same Dockerfile (e.g.,
`USER root` + `VOLUME /data` triggers both). This is intentional:

- **DL3002** gives a broad "consider non-root" nudge.
- **This rule** highlights the specific elevated-risk combination of root + state.

Neither rule suppresses the other via `EnabledRules` coordination because they
serve different purposes and neither has fixes that could overlap. If you want
only the targeted warning, disable DL3002 and keep this rule.

## References

- [Dockerfile reference -- USER](https://docs.docker.com/reference/dockerfile/#user)
- [Dockerfile reference -- VOLUME](https://docs.docker.com/reference/dockerfile/#volume)
- [Docker Blog -- Understanding the Docker USER Instruction](https://www.docker.com/blog/understanding-the-docker-user-instruction/)
- [Chainguard Best Practices](https://github.com/chainguard-images/images/blob/main/BEST_PRACTICES.md)

## Examples

### Bad

```dockerfile
# Implicit root + VOLUME: high risk
FROM ubuntu:22.04
VOLUME /var/lib/data
CMD ["app"]
```

```dockerfile
# Explicit root + data directory
FROM ubuntu:22.04
USER root
WORKDIR /var/lib/mysql
CMD ["mysqld"]
```

### Good

```dockerfile
# Non-root user with VOLUME
FROM ubuntu:22.04
RUN useradd -r -u 1000 appuser
USER appuser
VOLUME /data
CMD ["app"]
```

```dockerfile
# Privilege-drop entrypoint (official image pattern)
FROM ubuntu:22.04
VOLUME /var/lib/postgresql
ENTRYPOINT ["gosu", "postgres", "docker-entrypoint.sh"]
CMD ["postgres"]
```

```dockerfile
# Distroless nonroot base
FROM gcr.io/distroless/static:nonroot
VOLUME /data
CMD ["/app"]
```

```dockerfile
# Numeric non-root UID
FROM ubuntu:22.04
USER 65532:65532
VOLUME /data
CMD ["/app"]
```

## Configuration

```toml
[rules.tally.stateful-root-runtime]
severity = "warning"  # Options: "off", "error", "warning", "info", "style"
```
