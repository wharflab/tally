FROM golang:1.22 AS builder
RUN useradd -r appuser

FROM scratch
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
USER appuser
