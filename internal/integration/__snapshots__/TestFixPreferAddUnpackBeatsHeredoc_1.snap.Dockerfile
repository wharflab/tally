FROM ubuntu:22.04

ADD --unpack https://go.dev/dl/go1.22.0.linux-amd64.tar.gz /usr/local
ADD --unpack https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz /usr/local
ADD --unpack https://example.com/app.tar.gz /opt

RUN curl -fsSL https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz | tar -xJ -C /usr/local

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt
