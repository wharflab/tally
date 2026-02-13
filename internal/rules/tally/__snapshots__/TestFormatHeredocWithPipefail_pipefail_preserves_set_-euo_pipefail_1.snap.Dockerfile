RUN <<EOF
set -e
set -o pipefail
set -euo pipefail
curl -s https://example.com | bash
EOF