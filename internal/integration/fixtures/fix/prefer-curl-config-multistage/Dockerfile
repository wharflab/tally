# Stage 1: curl used directly — should trigger
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS downloader
RUN apt-get update && apt-get install -y ca-certificates curl
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash -

# Stage 2: curl config already present — should NOT trigger
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS configured
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
FROM golang:1.22@sha256:1cf6c45ba39db9fd6db16922041d074a63c935556a05c5ccb62d181034df7f02 AS builder
RUN go build -o /app ./cmd/app
