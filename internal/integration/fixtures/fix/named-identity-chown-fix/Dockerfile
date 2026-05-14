FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY --chown=appuser:appgroup --from=builder /app /app
