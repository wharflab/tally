#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSIONS_FILE="${SCRIPT_DIR}/versions.toml"
CACHE_DIR="${EXT_DIR}/.cache"
DIST_DIR="${EXT_DIR}/dist"
OUT_DIR="${EXT_DIR}/out"

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

read_optional_version() {
  local key="$1"
  local line
  line="$(grep -E "^${key}[[:space:]]*=" "${VERSIONS_FILE}" | head -n 1 || true)"
  if [[ -z "${line}" ]]; then
    return 0
  fi
  echo "${line#*=}" | sed -E 's/^[[:space:]]*//; s/[[:space:]]*$//; s/^"//; s/"$//'
}

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "required command not found: ${cmd}" >&2
    exit 1
  fi
}

download_if_missing() {
  local url="$1"
  local out="$2"
  if [[ -f "${out}" ]]; then
    return 0
  fi
  local tmp="${out}.tmp.$$"
  echo "downloading ${url}"
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
  mv "${tmp}" "${out}"
}

extract_archive() {
  local archive="$1"
  local target_dir="$2"
  rm -rf "${target_dir}"
  mkdir -p "${target_dir}"
  case "${archive}" in
    *.tar.gz | *.tgz)
      tar -xzf "${archive}" -C "${target_dir}"
      ;;
    *.zip)
      unzip -q "${archive}" -d "${target_dir}"
      ;;
    *)
      echo "unsupported archive format: ${archive}" >&2
      exit 1
      ;;
  esac
}

join_by() {
  local delimiter="$1"
  shift
  local first=1
  local out=""
  local value
  for value in "$@"; do
    if [[ ${first} -eq 1 ]]; then
      out="${value}"
      first=0
    else
      out="${out}${delimiter}${value}"
    fi
  done
  echo "${out}"
}

find_ide_home() {
  local extract_dir="$1"
  local lib_dir
  while IFS= read -r lib_dir; do
    local ide_home
    ide_home="$(dirname "${lib_dir}")"
    if [[ -d "${ide_home}/bin" ]] && [[ -d "${ide_home}/plugins" ]]; then
      echo "${ide_home}"
      return 0
    fi
  done < <(find "${extract_dir}" -maxdepth 6 -type d -name lib | sort)

  echo "unable to locate IntelliJ home under ${extract_dir}" >&2
  exit 1
}

find_kotlin_home() {
  local extract_dir="$1"
  local kotlinc
  kotlinc="$(find "${extract_dir}" -maxdepth 4 -type f -name 'kotlinc' | head -n 1 || true)"
  if [[ -z "${kotlinc}" ]]; then
    echo "unable to locate kotlinc under ${extract_dir}" >&2
    exit 1
  fi
  dirname "$(dirname "${kotlinc}")"
}

prepare_toolchains() {
  mkdir -p "${CACHE_DIR}" "${DIST_DIR}" "${OUT_DIR}"

  IDE_ARCHIVE_URL="$(read_version ide_archive_url)"
  KOTLIN_COMPILER_URL="$(read_version kotlin_compiler_url)"

  IDE_ARCHIVE="${CACHE_DIR}/$(basename "${IDE_ARCHIVE_URL}")"
  KOTLIN_ARCHIVE="${CACHE_DIR}/$(basename "${KOTLIN_COMPILER_URL}")"
  IDE_EXTRACT_DIR="${CACHE_DIR}/ide"
  KOTLIN_EXTRACT_DIR="${CACHE_DIR}/kotlin"

  download_if_missing "${IDE_ARCHIVE_URL}" "${IDE_ARCHIVE}"
  download_if_missing "${KOTLIN_COMPILER_URL}" "${KOTLIN_ARCHIVE}"

  if [[ ! -f "${IDE_EXTRACT_DIR}/.source" ]] || [[ "$(cat "${IDE_EXTRACT_DIR}/.source")" != "${IDE_ARCHIVE}" ]]; then
    extract_archive "${IDE_ARCHIVE}" "${IDE_EXTRACT_DIR}"
    printf '%s' "${IDE_ARCHIVE}" > "${IDE_EXTRACT_DIR}/.source"
  fi

  if [[ ! -f "${KOTLIN_EXTRACT_DIR}/.source" ]] || [[ "$(cat "${KOTLIN_EXTRACT_DIR}/.source")" != "${KOTLIN_ARCHIVE}" ]]; then
    extract_archive "${KOTLIN_ARCHIVE}" "${KOTLIN_EXTRACT_DIR}"
    printf '%s' "${KOTLIN_ARCHIVE}" > "${KOTLIN_EXTRACT_DIR}/.source"
  fi

  IDE_HOME="$(find_ide_home "${IDE_EXTRACT_DIR}")"
  KOTLIN_HOME="$(find_kotlin_home "${KOTLIN_EXTRACT_DIR}")"
  KOTLINC="${KOTLIN_HOME}/bin/kotlinc"

  if [[ ! -x "${KOTLINC}" ]]; then
    echo "kotlinc is not executable: ${KOTLINC}" >&2
    exit 1
  fi
}

patch_plugin_xml() {
  local plugin_xml="$1"
  local plugin_id="$2"
  local plugin_version="$3"
  local since_build="$4"
  local until_build="$5"
  sed -i.bak \
    -e "s/@PLUGIN_ID@/${plugin_id}/g" \
    -e "s/@PLUGIN_VERSION@/${plugin_version}/g" \
    -e "s/@SINCE_BUILD@/${since_build}/g" \
    -e "s/@UNTIL_BUILD@/${until_build}/g" \
    "${plugin_xml}"
  rm -f "${plugin_xml}.bak"
}

build_plugin() {
  require_cmd curl
  require_cmd unzip
  require_cmd tar
  require_cmd zip
  require_cmd jar
  require_cmd java

  prepare_toolchains

  local plugin_id plugin_name plugin_version plugin_since_build plugin_until_build jvm_target
  plugin_id="$(read_version plugin_id)"
  plugin_name="$(read_version plugin_name)"
  plugin_version="$(read_version plugin_version)"
  plugin_since_build="$(read_version plugin_since_build)"
  plugin_until_build="$(read_version plugin_until_build)"
  jvm_target="$(read_version jvm_target)"

  local classes_dir package_root plugin_dir plugin_jar plugin_zip
  classes_dir="${OUT_DIR}/classes"
  package_root="${OUT_DIR}/package"
  plugin_dir="${package_root}/${plugin_name}"
  plugin_jar="${plugin_dir}/lib/tally-intellij-plugin.jar"
  plugin_zip="${DIST_DIR}/tally-intellij-plugin-${plugin_version}.zip"

  rm -rf "${classes_dir}" "${package_root}" "${plugin_zip}"
  mkdir -p "${classes_dir}" "${plugin_dir}/lib"

  local -a classpath_entries
  mapfile -t classpath_entries < <(find "${IDE_HOME}/lib" -maxdepth 1 -type f -name '*.jar' | sort)
  if [[ -d "${IDE_HOME}/plugins/lsp/lib" ]]; then
    local -a lsp_jars
    mapfile -t lsp_jars < <(find "${IDE_HOME}/plugins/lsp/lib" -maxdepth 1 -type f -name '*.jar' | sort)
    classpath_entries+=("${lsp_jars[@]}")
  fi
  classpath_entries+=("${KOTLIN_HOME}/lib/kotlin-stdlib.jar")
  classpath_entries+=("${KOTLIN_HOME}/lib/kotlin-stdlib-jdk8.jar")

  local classpath
  classpath="$(join_by ":" "${classpath_entries[@]}")"

  "${KOTLINC}" "${EXT_DIR}/src/main/kotlin" \
    -classpath "${classpath}" \
    -d "${classes_dir}" \
    -jvm-target "${jvm_target}" \
    -no-stdlib \
    -no-reflect

  if [[ -d "${EXT_DIR}/src/main/resources" ]]; then
    cp -R "${EXT_DIR}/src/main/resources/." "${classes_dir}/"
  fi

  patch_plugin_xml \
    "${classes_dir}/META-INF/plugin.xml" \
    "${plugin_id}" \
    "${plugin_version}" \
    "${plugin_since_build}" \
    "${plugin_until_build}"

  jar --create --file "${plugin_jar}" -C "${classes_dir}" .

  if [[ -d "${EXT_DIR}/bundled/bin" ]]; then
    mkdir -p "${plugin_dir}/bin"
    cp -R "${EXT_DIR}/bundled/bin/." "${plugin_dir}/bin/"
  fi

  (
    cd "${package_root}"
    zip -qr "${plugin_zip}" "${plugin_name}"
  )

  echo "built plugin zip: ${plugin_zip}"
}

verify_plugin() {
  build_plugin

  local verifier_url
  verifier_url="$(read_optional_version plugin_verifier_url)"
  if [[ -z "${verifier_url}" ]]; then
    echo "plugin_verifier_url is not set; skipping plugin verifier."
    return 0
  fi

  local plugin_version verifier_jar plugin_zip
  plugin_version="$(read_version plugin_version)"
  plugin_zip="${DIST_DIR}/tally-intellij-plugin-${plugin_version}.zip"
  verifier_jar="${CACHE_DIR}/$(basename "${verifier_url}")"

  download_if_missing "${verifier_url}" "${verifier_jar}"
  java -jar "${verifier_jar}" check-plugin "${plugin_zip}" "${IDE_HOME}"
}

main() {
  local cmd="${1:-build}"
  case "${cmd}" in
    build)
      build_plugin
      ;;
    verify)
      verify_plugin
      ;;
    *)
      echo "usage: $0 [build|verify]" >&2
      exit 2
      ;;
  esac
}

main "${1:-build}"
