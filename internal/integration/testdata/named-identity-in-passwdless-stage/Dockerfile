# --- Violations: named identities in passwd-less stages ---

# Case 1: scratch with named USER (violation)
FROM golang:1.22 AS builder
RUN useradd -r appuser

FROM scratch AS runtime-bad
COPY --from=builder /myapp /myapp
USER appuser

# Case 2: scratch with named --chown (violation)
FROM scratch AS chown-bad
COPY --chown=appuser:appgroup --from=builder /myapp /myapp

# Case 3: named group only (violation)
FROM scratch AS group-bad
USER 1000:appgroup

# --- No violations: correct patterns ---

# Case 4: scratch with numeric USER (clean)
FROM scratch AS runtime-numeric
COPY --from=builder /myapp /myapp
USER 65532:65532

# Case 5: scratch with passwd copied from builder (clean)
FROM scratch AS runtime-passwd
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
USER appuser

# Case 6: non-scratch with named USER (clean)
FROM alpine:3.19 AS runtime-alpine
USER appuser

# Case 7: scratch with numeric --chown (clean)
FROM scratch AS chown-numeric
COPY --chown=1000:1000 --from=builder /myapp /myapp
