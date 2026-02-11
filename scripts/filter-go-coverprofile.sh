#!/usr/bin/env bash
#
# Normalize a Go coverage profile (go tool cover / covdata textfmt format),
# forcing generated files to 100% coverage to avoid skewing Codecov project
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

# Always keep the first "mode:" line. Normalize the rest.
awk '
NR==1 { print; next }
{
  # Go cover profiles are space-separated:
  #   <file>:<range> <numStmts> <count>
  #
  # For generated files, force count to >0 so they show as 100% covered.
  if ($0 ~ /_generated\.go:/) {
    $NF = 1
  }
  print
}
' "$IN" > "$OUT"

if ! head -n 1 "$OUT" | grep -q '^mode:'; then
  echo "normalized coverprofile is missing the \"mode:\" header line" >&2
  exit 1
fi
