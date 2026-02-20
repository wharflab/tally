FROM ubuntu:22.04

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ADD --unpack https://go.dev/dl/go1.22.0.linux-amd64.tar.gz /usr/local

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ADD --unpack https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz /usr/local

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ADD --unpack https://example.com/app.tar.gz /opt
