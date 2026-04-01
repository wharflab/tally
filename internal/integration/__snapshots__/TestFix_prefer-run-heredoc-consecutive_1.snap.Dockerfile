FROM ubuntu:22.04
RUN <<EOF
set -e
apt-get update
apt-get install -y curl
apt-get install -y git
EOF
