FROM golang:1.22-alpine AS builder
COPY --from=runtime /usr/local/bin/tini /usr/local/bin/tini
RUN go build -o /app ./cmd/server

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS runtime
COPY --from=builder /app /usr/local/bin/app
RUN apk add --no-cache ca-certificates

FROM runtime
ENTRYPOINT ["/usr/local/bin/app"]
