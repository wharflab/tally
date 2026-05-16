#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: $0 [bazel cquery options] <target>" >&2
  exit 2
fi

bazel cquery \
  --noshow_progress \
  --ui_event_filters=-info \
  --output=files \
  "$@" |
  sed '/^[[:space:]]*$/d' |
  tail -n 1
