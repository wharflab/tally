FROM ubuntu:22.04
RUN <<EOF
set -e
set -o pipefail
apt-get update
apt-get install -y curl
curl -fsSL https://example.com/setup.sh | bash
EOF
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l > /number
