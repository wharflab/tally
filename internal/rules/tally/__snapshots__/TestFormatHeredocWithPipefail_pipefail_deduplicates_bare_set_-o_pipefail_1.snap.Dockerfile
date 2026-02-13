RUN <<EOF
set -e
set -o pipefail
curl -s https://example.com | bash
EOF