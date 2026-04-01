FROM ubuntu:22.04
COPY <<EOF /etc/nginx.conf
server {}
EOF
RUN <<EOF
set -e
apt-get update
apt-get install -y curl
apt-get install -y git
EOF
