FROM golang:1.22@sha256:1cf6c45ba39db9fd6db16922041d074a63c935556a05c5ccb62d181034df7f02 AS builder
RUN echo hello > /app

FROM scratch
COPY --chown=65532:65532 --from=builder /app /app
