FROM ubuntu:24.04
RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked <<EOF
apt-get update && apt-get install -y gcc
EOF
