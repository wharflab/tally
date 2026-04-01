FROM ubuntu:22.04
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
