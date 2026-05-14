FROM golang:1.22-alpine AS builder
COPY --from=runtime /usr/local/bin/tini /usr/local/bin/tini
RUN go build -o /app ./cmd/server

FROM alpine:3.19 AS runtime
COPY --from=builder /app /usr/local/bin/app
RUN apk add --no-cache ca-certificates

FROM runtime
ENTRYPOINT ["/usr/local/bin/app"]
