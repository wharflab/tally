RUN <<EOF
set -e
apt-get update
curl -s https://example.com | bash
EOF