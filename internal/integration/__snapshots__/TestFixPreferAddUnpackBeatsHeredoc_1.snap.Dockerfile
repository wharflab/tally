FROM ubuntu:22.04

ENV CURL_HOME=/etc/curl

COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ADD --unpack https://go.dev/dl/go1.22.0.linux-amd64.tar.gz /usr/local
ADD --unpack https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz /usr/local
ADD --unpack https://example.com/app.tar.gz /opt
