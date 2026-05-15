FROM ubuntu:24.04@sha256:c4a8d5503dfb2a3eb8ab5f807da5bc69a85730fb49b5cfca2330194ebcc41c7b
RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked <<EOF
apt-get update && apt-get install -y gcc
EOF
