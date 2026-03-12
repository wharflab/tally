#!/usr/bin/env bash
#
# Merge multiple Go coverprofiles with the same coverage mode into one profile.
#
# Usage:
#   scripts/merge-go-coverprofiles.sh <output> <input1> <input2> [input3...]

set -euo pipefail

if [[ $# -lt 3 ]]; then
  echo "usage: $0 <output-coverprofile> <input1> <input2> [inputN...]" >&2
  exit 2
fi

OUT="$1"
shift

for in_file in "$@"; do
  if [[ ! -s "$in_file" ]]; then
    echo "coverprofile $in_file is empty or missing" >&2
    exit 1
  fi
done

awk '
BEGIN {
  mode = ""
  n = 0
}

FNR == 1 {
  if ($1 != "mode:") {
    printf("coverprofile %s is missing mode header\n", FILENAME) > "/dev/stderr"
    exit 1
  }
  if (mode == "") {
    mode = $2
  } else if ($2 != mode) {
    printf("coverprofile mode mismatch: expected %s, got %s in %s\n", mode, $2, FILENAME) > "/dev/stderr"
    exit 1
  }
  next
}

{
  key = $1 " " $2
  if (!(key in seen)) {
    order[n++] = key
    seen[key] = 1
  }
  if (mode == "set") {
    if ($3 > counts[key]) {
      counts[key] = $3
    }
  } else {
    counts[key] += $3
  }
}

END {
  if (mode == "") {
    print "no coverprofiles were read" > "/dev/stderr"
    exit 1
  }
  print "mode:", mode
  for (i = 0; i < n; i++) {
    key = order[i]
    print key, counts[key]
  }
}
' "$@" > "$OUT"
