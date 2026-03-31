# tally/world-writable-state-path-workaround

chmod 777/a+rwx sets world-writable permissions, a common ownership confusion workaround.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Security |
| Default | Enabled |
| Auto-fix | Suggestion (octal modes only) |

## Description

This rule detects `RUN` instructions that use `chmod 777`, `chmod a+rwx`,
`mkdir -m 777`, or similarly broad world-writable permissions on any path.

Setting world-writable permissions is almost always a workaround for
ownership confusion rather than an intentional security decision. Common
causes:

- The author does not know which user/group will run the process, so they
  open permissions to everyone.
- A `WORKDIR` was created as root, but the app runs as a non-root user.
- Files were `COPY`'d without `--chown` and the author used `chmod 777`
  instead of fixing ownership.

World-writable paths inside a container allow any process (including a
compromised one) to modify files, inject content, or corrupt data. This
matters especially for state directories (`/data`, `/var/lib/*`,
`/var/log/*`, `/var/cache/*`, `/var/run/*`, `/srv`) that may back
persistent volumes or host mounts.

The fix is usually one of:

- Set proper ownership with `USER`, `COPY --chown`, or `RUN chown`
- Use group permissions (`chmod g+w`, `chgrp 0 && chmod g=u`) for
  OpenShift-style arbitrary-UID containers
- Use tighter modes (`755`, `775`) that don't grant write to others

## Patterns detected

### Octal modes with others-write bit

Any octal mode where the last digit includes write (2, 3, 6, 7):

- `chmod 777 /path` (read+write+execute for all)
- `chmod 666 /path` (read+write for all)
- `chmod 776 /path` (others read+write)
- `mkdir -m 777 /path`
- `mkdir -pm 777 /path`
- `mkdir --mode=777 /path`

### Symbolic modes granting others-write

- `chmod a+rwx /path` (all: read+write+execute)
- `chmod o+w /path` (others: write)
- `chmod +w /path` (no who = all: write)
- `chmod a=rwx /path` (assign all rwx)

## Patterns NOT flagged

- `chmod 755`, `chmod 644`, `chmod 775`, `chmod 770` (no others-write)
- `chmod g+w`, `chmod g+rwx`, `chmod g=rwx` (group only, not others)
- `chmod g=u` (copy user permissions to group, an OpenShift pattern)
- `chmod u+x`, `chmod +x` (execute only, no write)
- `chmod o+r`, `chmod o+x` (read/execute only, no write)

## OpenShift and arbitrary-UID containers

Valid OpenShift patterns use group-only permission changes (`chgrp 0 && chmod g=u`,
`chmod g+rwx`, `chmod 775`) which do **not** set the others-write bit and therefore
do not trigger this rule. `chmod 777` is still flagged even when paired with `chgrp`,
because it grants write to all users, not just the intended group.

For OpenShift-compatible containers, prefer `chgrp 0 /path && chmod g=u /path`
over `chmod 777 /path`.

## Relationship to related rules

| Rule | Relationship |
|------|-------------|
| `tally/stateful-root-runtime` | Complementary. That rule flags root + state paths; this rule flags world-writable permissions on any path. Both can fire on the same Dockerfile. |
| `tally/prefer-copy-chmod` | Complementary. That rule suggests merging COPY + RUN chmod into COPY --chmod; this rule flags the permission mode itself. Different concerns (structure vs security). |
| `tally/copy-after-user-without-chown` | Same ownership confusion family. That rule detects missing --chown on COPY after USER; this rule detects chmod workarounds. |

## Examples

### Bad

```dockerfile
# World-writable state directory
FROM ubuntu:22.04
RUN mkdir -p /data && chmod 777 /data
CMD ["app"]
```

```dockerfile
# World-writable app directory
FROM ubuntu:22.04
COPY app /app
RUN chmod a+rwx /app
USER appuser
CMD ["/app/server"]
```

```dockerfile
# World-writable mkdir
FROM ubuntu:22.04
RUN mkdir -pm 777 /var/lib/myapp/logs
```

### Good

```dockerfile
# Proper ownership with USER and chown
FROM ubuntu:22.04
RUN useradd -r -u 1000 appuser
COPY --chown=appuser:appuser app /app
RUN chmod 755 /app
USER appuser
CMD ["/app/server"]
```

```dockerfile
# OpenShift-style group permissions (suppressed by this rule)
FROM ubuntu:22.04
RUN mkdir -p /data && \
    chgrp 0 /data && \
    chmod g=u /data
USER 1001
CMD ["app"]
```

```dockerfile
# Tight permissions without world-write
FROM ubuntu:22.04
RUN mkdir -p /data && chmod 775 /data
USER appuser
CMD ["app"]
```

## References

- [Dockerfile reference -- USER](https://docs.docker.com/reference/dockerfile/#user)
- [Dockerfile reference -- COPY --chown](https://docs.docker.com/reference/dockerfile/#copy---chown)
- [Red Hat -- A Guide to OpenShift and UIDs](https://www.redhat.com/en/blog/a-guide-to-openshift-and-uids)
- [Docker Blog -- Understanding the Docker USER Instruction](https://www.docker.com/blog/understanding-the-docker-user-instruction/)

## Configuration

```toml
[rules.tally.world-writable-state-path-workaround]
severity = "warning"  # Options: "off", "error", "warning", "info", "style"
```
