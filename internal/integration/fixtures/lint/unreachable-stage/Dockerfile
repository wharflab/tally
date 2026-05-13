FROM golang:1.21 AS builder
RUN go build -o /app

FROM golang:1.21 AS unused
RUN echo "this stage is never used"

FROM alpine:3.18
COPY --from=builder /app /app
CMD ["/app"]
