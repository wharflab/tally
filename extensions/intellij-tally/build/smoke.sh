#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSIONS_FILE="${SCRIPT_DIR}/versions.toml"
LIB_FILE="${SCRIPT_DIR}/lib.sh"

SMOKE_DIR="${EXT_DIR}/.cache/smoke"
REPORTS_DIR="${SMOKE_DIR}/reports"
LOG_FILE="${SMOKE_DIR}/verifier-ic.log"

DEFAULT_IDE_ARCHIVE_URL="https://download.jetbrains.com/idea/ideaIC-2025.2.3.tar.gz"
DEFAULT_PLUGIN_VERIFIER_URL="https://github.com/JetBrains/intellij-plugin-verifier/releases/download/1.400/verifier-cli-1.400-all.jar"

if [[ ! -f "${LIB_FILE}" ]]; then
  echo "required helper script not found: ${LIB_FILE}" >&2
  exit 1
fi
# shellcheck source=lib.sh
source "${LIB_FILE}"

ensure_valid_jar() {
  local jar_path="$1"
  if [[ ! -f "${jar_path}" ]]; then
    return 1
  fi
  if ! jar tf "${jar_path}" >/dev/null 2>&1; then
    rm -f "${jar_path}"
    return 1
  fi
  return 0
}

ensure_valid_targz() {
  local archive_path="$1"
  if [[ ! -f "${archive_path}" ]]; then
    return 1
  fi
  if ! tar -tzf "${archive_path}" >/dev/null 2>&1; then
    rm -f "${archive_path}"
    return 1
  fi
  return 0
}

ensure_plugin_zip() {
  local plugin_version plugin_zip
  plugin_version="$(read_version plugin_version)"
  plugin_zip="${EXT_DIR}/dist/tally-intellij-plugin-${plugin_version}.zip"
  if [[ ! -f "${plugin_zip}" ]]; then
    bash "${SCRIPT_DIR}/build.sh" build >&2
  fi
  echo "${plugin_zip}"
}

prepare_idea_community() {
  local archive_url="$1"
  local archive="${SMOKE_DIR}/$(basename "${archive_url}")"
  local extract_dir="${SMOKE_DIR}/idea-ic"

  ensure_valid_targz "${archive}" || download_if_missing "${archive_url}" "${archive}"
  ensure_valid_targz "${archive}" || {
    echo "unable to obtain a valid IntelliJ IDEA Community archive: ${archive}" >&2
    exit 1
  }

  if [[ ! -f "${extract_dir}/.source" ]] || [[ "$(cat "${extract_dir}/.source")" != "${archive}" ]]; then
    rm -rf "${extract_dir}"
    mkdir -p "${extract_dir}"
    tar -xzf "${archive}" -C "${extract_dir}"
    printf '%s' "${archive}" > "${extract_dir}/.source"
  fi

  local ide_home
  ide_home="$(find "${extract_dir}" -maxdepth 2 -type d -name 'idea-IC-*' | head -n 1 || true)"
  if [[ -z "${ide_home}" ]]; then
    echo "unable to locate IntelliJ IDEA Community directory under ${extract_dir}" >&2
    exit 1
  fi
  echo "${ide_home}"
}

main() {
  command -v jar >/dev/null 2>&1 || {
    echo "required command not found: jar" >&2
    exit 1
  }
  command -v tar >/dev/null 2>&1 || {
    echo "required command not found: tar" >&2
    exit 1
  }

  mkdir -p "${SMOKE_DIR}" "${REPORTS_DIR}"

  local plugin_zip ide_archive_url verifier_url verifier_jar ide_home
  plugin_zip="$(ensure_plugin_zip)"
  ide_archive_url="${TALLY_INTELLIJ_SMOKE_IDE_URL:-${DEFAULT_IDE_ARCHIVE_URL}}"
  verifier_url="${TALLY_INTELLIJ_PLUGIN_VERIFIER_URL:-${DEFAULT_PLUGIN_VERIFIER_URL}}"
  verifier_jar="${SMOKE_DIR}/$(basename "${verifier_url}")"

  ensure_valid_jar "${verifier_jar}" || download_if_missing "${verifier_url}" "${verifier_jar}"
  ensure_valid_jar "${verifier_jar}" || {
    echo "unable to obtain a valid plugin verifier jar: ${verifier_jar}" >&2
    exit 1
  }
  ide_home="$(prepare_idea_community "${ide_archive_url}")"

  rm -f "${LOG_FILE}"
  local verifier_rc=0
  java -jar "${verifier_jar}" check-plugin \
    -verification-reports-dir "${REPORTS_DIR}" \
    "${plugin_zip}" \
    "${ide_home}" > "${LOG_FILE}" 2>&1 || verifier_rc=$?
  echo "plugin verifier exited with code ${verifier_rc}" >> "${LOG_FILE}"

  if ! grep -q "Scheduled verifications (1):" "${LOG_FILE}"; then
    echo "smoke check failed: plugin verifier did not schedule a CE verification" >&2
    tail -n 120 "${LOG_FILE}" >&2
    exit 1
  fi

  if grep -q "Plugin is invalid" "${LOG_FILE}"; then
    echo "smoke check failed: plugin is invalid for verification" >&2
    tail -n 120 "${LOG_FILE}" >&2
    exit 1
  fi

  if grep -E -q "missing mandatory dependency|Compatibility problems \([1-9]" "${LOG_FILE}"; then
    echo "smoke check failed: unexpected compatibility issues were reported" >&2
    tail -n 120 "${LOG_FILE}" >&2
    exit 1
  fi

  if [[ "${verifier_rc}" -ne 0 ]]; then
    echo "smoke check failed: plugin verifier exited with code ${verifier_rc}" >&2
    tail -n 120 "${LOG_FILE}" >&2
    exit "${verifier_rc}"
  fi

  echo "smoke check passed against IntelliJ IDEA Community Edition"
  echo "details: ${LOG_FILE}"
}

main "$@"
