FROM alpine:3.20 AS builder
run echo build
FROM scratch
copy --from=builder /app /app
