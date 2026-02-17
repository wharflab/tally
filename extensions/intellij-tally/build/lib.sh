#!/usr/bin/env bash

read_version() {
  local key="$1"
  local line
  line="$(grep -E "^${key}[[:space:]]*=" "${VERSIONS_FILE}" | head -n 1 || true)"
  if [[ -z "${line}" ]]; then
    echo "missing required key in versions.toml: ${key}" >&2
    exit 1
  fi
  echo "${line#*=}" | sed -E 's/^[[:space:]]*//; s/[[:space:]]*$//; s/^"//; s/"$//'
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
    return 0
  fi
  echo "required command not found: sha256sum or shasum" >&2
  exit 1
}

verify_sha256() {
  local file="$1"
  local expected="$2"
  if [[ -z "${expected}" ]]; then
    return 0
  fi
  local actual
  actual="$(sha256_file "${file}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "checksum mismatch for ${file}: expected ${expected}, got ${actual}" >&2
    return 1
  fi
}

download_if_missing() {
  local url="$1"
  local out="$2"
  local expected_sha256="${3:-}"
  if [[ -f "${out}" ]]; then
    verify_sha256 "${out}" "${expected_sha256}"
    return 0
  fi
  local tmp="${out}.tmp.$$"
  echo "downloading ${url}" >&2
  rm -f "${tmp}"
  if ! curl \
    --fail \
    --location \
    --silent \
    --show-error \
    --retry 5 \
    --retry-delay 2 \
    --connect-timeout "${TALLY_INTELLIJ_CURL_CONNECT_TIMEOUT:-30}" \
    --max-time "${TALLY_INTELLIJ_CURL_MAX_TIME:-1800}" \
    "${url}" \
    -o "${tmp}"; then
    rm -f "${tmp}"
    return 1
  fi
  if ! verify_sha256 "${tmp}" "${expected_sha256}"; then
    rm -f "${tmp}"
    return 1
  fi
  mv "${tmp}" "${out}"
}
