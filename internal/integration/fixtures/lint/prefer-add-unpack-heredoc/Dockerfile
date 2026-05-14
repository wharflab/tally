FROM ubuntu:22.04
RUN apt-get update && \
    apt-get install -y curl && \
    curl -fsSL https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | tar -xz -C /usr/local && \
    rm -rf /var/lib/apt/lists/*
