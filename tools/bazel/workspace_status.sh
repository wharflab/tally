#!/usr/bin/env bash

set -euo pipefail

version="${TALLY_VERSION:-${RELEASE_VERSION:-${BUILD_VERSION:-0.0.0-dev}}}"
version="${version#v}"
commit="${GITHUB_SHA:-}"
if [[ -z "${commit}" ]]; then
  commit="$(git rev-parse HEAD 2>/dev/null || true)"
fi

echo "STABLE_TALLY_VERSION ${version}"
echo "STABLE_TALLY_COMMIT ${commit}"
