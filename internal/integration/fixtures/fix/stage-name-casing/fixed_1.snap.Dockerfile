FROM alpine:3.18 AS builder
RUN echo hello
FROM alpine:3.18
COPY --from=builder /app /app
