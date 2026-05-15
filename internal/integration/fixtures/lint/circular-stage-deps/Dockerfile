FROM golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052 AS builder
COPY --from=runtime /usr/local/bin/tini /usr/local/bin/tini
RUN go build -o /app ./cmd/server

FROM alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1 AS runtime
COPY --from=builder /app /usr/local/bin/app
RUN apk add --no-cache ca-certificates

FROM runtime
ENTRYPOINT ["/usr/local/bin/app"]
