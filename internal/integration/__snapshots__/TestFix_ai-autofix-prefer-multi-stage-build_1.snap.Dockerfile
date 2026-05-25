FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY . .
RUN go build -o /out/app ./cmd/app

FROM alpine:3.20
WORKDIR /src
COPY --from=builder /out/app /usr/local/bin/app
CMD ["app"]
