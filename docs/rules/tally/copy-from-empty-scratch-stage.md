# tally/copy-from-empty-scratch-stage

Detects COPY --from referencing a scratch stage with no file-producing instructions.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |

## Description

Detects `COPY --from=<stage>` instructions where the source stage is `FROM scratch` and contains
no `ADD`, `COPY`, or `RUN` instructions. Since scratch stages start with an empty filesystem,
any `COPY --from` referencing such a stage is guaranteed to fail at build time.

Common causes:

- A stage was renamed or deleted during a refactor, leaving an empty placeholder
- An AI patch accidentally removed the instructions that populated the stage
- A `COPY`/`RUN` was moved to a different stage but the `COPY --from` reference wasn't updated

Instructions like `ENV`, `LABEL`, `EXPOSE`, `WORKDIR`, and `USER` do not produce filesystem
content and are not considered file-producing for this check.

## Examples

### Bad

```dockerfile
# "artifacts" stage is scratch with no file-producing instructions
FROM scratch AS artifacts

FROM alpine:3.19
COPY --from=artifacts /out/app /usr/local/bin/app
```

### Good

```dockerfile
FROM golang:1.22 AS builder
RUN go build -o /out/app ./...

# "artifacts" stage has a COPY that populates it
FROM scratch AS artifacts
COPY --from=builder /out/app /out/app

FROM alpine:3.19
COPY --from=artifacts /out/app /usr/local/bin/app
```

## Configuration

```toml
[rules.tally.copy-from-empty-scratch-stage]
severity = "error"  # Options: "off", "error", "warning", "info", "style"
```
