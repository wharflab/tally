#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: $0 [bazel cquery options] <target>" >&2
  exit 2
fi

outputs=()
while IFS= read -r output; do
  outputs+=("$output")
done < <(
  bazel cquery \
    --noshow_progress \
    --ui_event_filters=-info \
    --output=files \
    "$@" |
    sed '/^[[:space:]]*$/d'
)

if [ "${#outputs[@]}" -ne 1 ]; then
  printf 'expected exactly one output for target query, got %d\n' "${#outputs[@]}" >&2
  printf '%s\n' "${outputs[@]}" >&2
  exit 1
fi

printf '%s\n' "${outputs[0]}"
