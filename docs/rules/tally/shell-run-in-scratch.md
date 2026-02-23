# tally/shell-run-in-scratch

Detects shell-form RUN instructions in scratch stages where no shell exists.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |

## Description

Detects shell-form `RUN` instructions (e.g., `RUN echo "hello"`) in `FROM scratch` stages.
Shell-form RUN requires a shell (`/bin/sh` by default) in the container's root filesystem.
Since `scratch` is an empty image with no shell, these instructions will always fail at build time.

If you explicitly set a `SHELL` instruction in the scratch stage, this rule is suppressed because
it assumes you have bootstrapped a shell binary into the stage (e.g., via `COPY --from` or `ADD`).

Common causes:

- Changing `FROM alpine` to `FROM scratch` to shrink the image without reworking `RUN` instructions
- An AI patch replacing the base image without adjusting command forms

## Examples

### Bad

```dockerfile
FROM scratch
RUN echo "hello"
```

### Good (exec-form)

```dockerfile
FROM scratch
RUN ["/myapp", "--init"]
```

### Good (explicit SHELL after bootstrapping)

```dockerfile
FROM scratch
COPY --from=builder /bin/sh /bin/sh
SHELL ["/bin/sh", "-c"]
RUN echo "hello"
```

### Good (different base image)

```dockerfile
FROM alpine:3.19
RUN echo "hello"
```

## Configuration

```toml
[rules.tally.shell-run-in-scratch]
severity = "warning"  # Options: "off", "error", "warning", "info", "style"
```
