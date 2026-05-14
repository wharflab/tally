FROM golang:1.21 AS builder
RUN go build -o /app
FROM alpine:3.20
COPY --from=builder /app /app

ENTRYPOINT ["/app"]
CMD ["serve"]
