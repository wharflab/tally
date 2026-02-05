# tally/no-unreachable-stages

Warns about build stages that don't contribute to the final image.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Best Practice |
| Default | Enabled |

## Description

Detects stages that are defined but never used. A stage is considered "unreachable" if it:

- Is not the final stage in the Dockerfile
- Is not referenced by `COPY --from=<stage>`
- Is not the target of a `--target` build argument

Unreachable stages add complexity and confusion without providing value. They may be:

- Leftover from refactoring
- Copy-paste artifacts
- Forgotten experimental code

## Examples

### Bad

```dockerfile
# This stage is never used
FROM golang:1.21 AS unused-builder
RUN go build -o /app .

# This is the actual build
FROM golang:1.21 AS builder
COPY . .
RUN go build -o /app .

FROM alpine:3.18
COPY --from=builder /app /app
CMD ["/app"]
```

### Good

```dockerfile
# All stages contribute to the final image
FROM golang:1.21 AS builder
COPY . .
RUN go build -o /app .

FROM alpine:3.18
COPY --from=builder /app /app
CMD ["/app"]
```

### Also Good (targeted builds)

```dockerfile
# Both stages are valid targets
FROM golang:1.21 AS dev
RUN go install github.com/cosmtrek/air@latest
CMD ["air"]

FROM golang:1.21 AS builder
RUN go build -o /app .

FROM alpine:3.18 AS prod
COPY --from=builder /app /app
CMD ["/app"]
```

```bash
# Build for development
docker build --target dev -t myapp:dev .

# Build for production
docker build --target prod -t myapp:prod .
```

## Configuration

```toml
[rules.tally.no-unreachable-stages]
severity = "warning"  # Options: "off", "error", "warning", "info", "style"
```
