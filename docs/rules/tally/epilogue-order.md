# tally/epilogue-order

Runtime-configuration instructions should appear at the end of each output stage in canonical order.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Enabled |
| Auto-fix | Yes (safe) |

## Description

Dockerfiles should end each output stage with runtime-configuration instructions in a canonical order:
**STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD**. These epilogue instructions configure how the container
runs rather than how the image is built, and placing them at the end of the stage makes the Dockerfile
easier to read and maintain.

This rule checks two conditions for each applicable stage:

1. **Position**: All epilogue instructions must appear at the end of the stage (no build instructions like RUN, COPY, ENV after them)
2. **Order**: Among the epilogue instructions, they must appear in canonical order

**Applicable stages**: The final stage and any stage with no dependents (not referenced by `COPY --from` or `FROM`). Intermediate builder stages are
skipped since they typically don't use epilogue instructions.

## Examples

### Bad

```dockerfile
FROM alpine:3.20
CMD ["/app", "serve"]
RUN apk add --no-cache ca-certificates
ENTRYPOINT ["/app"]
```

CMD appears before RUN (position violation), and CMD comes before ENTRYPOINT (order violation).

### Good

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
ENTRYPOINT ["/app"]
CMD ["serve"]
```

All build instructions come first, then epilogue instructions in canonical order.

### Multi-stage (builder skipped)

```dockerfile
FROM golang:1.21 AS builder
RUN go build -o /app
# No violation here - builder stage is skipped

FROM alpine:3.20
COPY --from=builder /app /app
ENTRYPOINT ["/app"]
CMD ["serve"]
```

## Auto-fix

This rule provides a safe auto-fix that moves epilogue instructions to the end of the stage in canonical order:

```bash
tally lint --fix Dockerfile
```

The fix:

- Removes each epilogue instruction from its current position
- Inserts all epilogue instructions at the end of the stage in canonical order
- Preserves preceding comments and continuation lines

When duplicate epilogue instructions of the same type exist (e.g., two CMD instructions), the fix is skipped for safety. The
`MultipleInstructionsDisallowed` rule handles duplicate removal.
