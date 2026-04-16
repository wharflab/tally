#!/usr/bin/env bash
#
# Validate that the BuildKit rule table in README.md is in sync
# with the current BuildKit dependency and tally's BuildKit implementation.
#
# Exit codes:
#   0 - Docs are up to date
#   1 - Docs or registry are out of date

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

echo "Validating BuildKit rule docs..."
: "${GOEXPERIMENT:=jsonv2}"
export GOEXPERIMENT
go run ./scripts/sync-buildkit-rules --check
