FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
RUN apt-get update && \
    apt-get install -y curl && \
    curl -fsSL https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | tar -xz -C /usr/local && \
    rm -rf /var/lib/apt/lists/*
