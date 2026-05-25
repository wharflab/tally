RUN <<EOF
set -e
set -o pipefail
apt-get update
curl -s https://example.com | bash
apt-get clean
EOF