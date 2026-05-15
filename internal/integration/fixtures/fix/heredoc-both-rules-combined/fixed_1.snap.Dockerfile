FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7

COPY <<EOF /etc/nginx.conf
server {}
EOF

RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked apt-get update
# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked apt-get install -y curl
RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked apt-get install -y git
