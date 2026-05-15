FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
RUN <<EOF
set -e
set -o pipefail
apt-get update
apt-get install -y curl
curl -fsSL https://example.com/setup.sh | bash
EOF
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l > /number
