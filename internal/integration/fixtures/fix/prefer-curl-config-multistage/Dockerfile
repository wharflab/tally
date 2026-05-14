# Stage 1: curl used directly — should trigger
FROM ubuntu:22.04 AS downloader
RUN apt-get update && apt-get install -y ca-certificates curl
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash -

# Stage 2: curl config already present — should NOT trigger
FROM alpine:3.20 AS configured
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF /etc/curl/.curlrc
--retry 5
--retry-connrefused
EOF
RUN curl -fsSL https://example.com/install.sh | sh

# Stage 3: inherits from downloader — should NOT trigger (parent gets the fix)
FROM downloader AS fetcher
RUN curl -fsSL https://example.com/data.json -o /tmp/data.json

# Stage 4: no curl usage — should NOT trigger
FROM golang:1.22 AS builder
RUN go build -o /app ./cmd/app
