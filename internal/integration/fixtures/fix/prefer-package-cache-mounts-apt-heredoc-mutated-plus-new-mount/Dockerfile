FROM ubuntu:24.04
RUN --mount=type=cache,target=/var/cache/apt,id=apt <<EOF
apt-get update && apt-get install -y gcc && apt-get clean
EOF
