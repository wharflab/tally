#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSIONS_FILE="${SCRIPT_DIR}/versions.toml"
LIB_FILE="${SCRIPT_DIR}/lib.sh"
CACHE_DIR="${EXT_DIR}/.cache"

if [[ ! -f "${LIB_FILE}" ]]; then
  echo "required helper script not found: ${LIB_FILE}" >&2
  exit 1
fi
# shellcheck source=lib.sh
source "${LIB_FILE}"

KTLINT_VERSION="$(read_version ktlint_version)"
KTLINT_URL="${KTLINT_URL:-https://github.com/pinterest/ktlint/releases/download/${KTLINT_VERSION}/ktlint}"
KTLINT_BIN="${CACHE_DIR}/ktlint-${KTLINT_VERSION}"

mkdir -p "${CACHE_DIR}"
download_if_missing "${KTLINT_URL}" "${KTLINT_BIN}"
chmod +x "${KTLINT_BIN}"

if [[ "$#" -eq 0 ]]; then
  set -- "${EXT_DIR}/src/main/kotlin"
fi

exec "${KTLINT_BIN}" "$@"
