# Stage 1: curl used directly — should trigger
FROM ubuntu:22.04 AS downloader
# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
RUN apt-get update && apt-get install -y ca-certificates curl
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash -

# Stage 2: curl config already present — should NOT trigger
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS configured
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
