FROM golang:1.21 AS builder
RUN go build -o /app
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
COPY --from=builder /app /app

ENTRYPOINT ["/app"]
CMD ["serve"]
