FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.20
COPY --from=builder /app /app
RUN apk add --no-cache ca-certificates
CMD ["/app", "serve"]
HEALTHCHECK CMD curl -f http://localhost/
ENTRYPOINT ["/app"]
