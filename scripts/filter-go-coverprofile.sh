#!/usr/bin/env bash
#
# Filter a Go coverage profile (go tool cover / covdata textfmt format),
# dropping entries for generated files to avoid skewing Codecov project
# coverage metrics.
#
# Usage:
#   scripts/filter-go-coverprofile.sh <input> <output>

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <input-coverprofile> <output-coverprofile>" >&2
  exit 2
fi

IN="$1"
OUT="$2"

# Always keep the first "mode:" line. Filter the rest.
awk '
NR==1 { print; next }
$0 !~ /_generated\.go:/ { print }
' "$IN" > "$OUT"

if ! head -n 1 "$OUT" | grep -q '^mode:'; then
  echo "filtered coverprofile is missing the \"mode:\" header line" >&2
  exit 1
fi
