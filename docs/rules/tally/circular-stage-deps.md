# tally/circular-stage-deps

Detects circular dependencies between build stages.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |

## Description

Detects cycles in the stage dependency graph. A cycle occurs when stages form mutual dependencies
through `COPY --from=<stage>`, `RUN --mount from=<stage>`, or `FROM <stage>` references.

Circular dependencies always cause build failures because no stage in the cycle can finish
building before the others — each waits for output from another.

Common causes:

- A refactoring accidentally swaps stage references
- An AI-generated patch links stages in both directions
- A copy-paste introduces a forward `COPY --from` that mirrors an existing backward reference

## Examples

### Bad

```dockerfile
# builder copies from runtime, runtime copies from builder → cycle
FROM golang:1.22-alpine AS builder
COPY --from=runtime /usr/local/bin/tini /usr/local/bin/tini
RUN go build -o /app ./cmd/server

FROM alpine:3.19 AS runtime
COPY --from=builder /app /usr/local/bin/app
RUN apk add --no-cache ca-certificates

FROM runtime
ENTRYPOINT ["/usr/local/bin/app"]
```

### Good

```dockerfile
# Dependencies flow in one direction: deps → builder → runtime
FROM golang:1.22-alpine AS builder
RUN go build -o /app ./cmd/server

FROM alpine:3.19 AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=builder /app /usr/local/bin/app

FROM runtime
ENTRYPOINT ["/usr/local/bin/app"]
```

## Configuration

```toml
[rules.tally.circular-stage-deps]
severity = "error"  # Options: "off", "error", "warning", "info", "style"
```
