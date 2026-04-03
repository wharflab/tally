FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY --chown=65532:65532 --from=builder /app /app
