#!/bin/bash
# Generate Homebrew formula from template with actual checksums
# Usage: ./generate-formula.sh <version> <output-file>
#
# Expects environment variables or fetches checksums from GitHub release:
# - SHA256_DARWIN_ARM64
# - SHA256_DARWIN_AMD64
# - SHA256_LINUX_ARM64
# - SHA256_LINUX_AMD64

set -euo pipefail

VERSION="${1:?Version required (e.g., 0.3.0)}"
OUTPUT_FILE="${2:?Output file path required}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_FILE="${SCRIPT_DIR}/tally.rb.template"

if [[ ! -f "$TEMPLATE_FILE" ]]; then
    echo "Error: Template file not found: $TEMPLATE_FILE" >&2
    exit 1
fi

# Fetch checksums from GitHub release if not provided via env vars
BASE_URL="https://github.com/wharflab/tally/releases/download/v${VERSION}"

fetch_checksum() {
    local archive_name="$1"
    local url="${BASE_URL}/${archive_name}"
    echo "Fetching checksum for ${archive_name}..." >&2

    # Download and compute SHA256 (-f fails on HTTP errors)
    curl -sfL "$url" | shasum -a 256 | cut -d' ' -f1
}

# Use environment variables if set, otherwise fetch from release
SHA256_DARWIN_ARM64="${SHA256_DARWIN_ARM64:-$(fetch_checksum "tally_${VERSION}_MacOS_arm64.tar.gz")}"
SHA256_DARWIN_AMD64="${SHA256_DARWIN_AMD64:-$(fetch_checksum "tally_${VERSION}_MacOS_x86_64.tar.gz")}"
SHA256_LINUX_ARM64="${SHA256_LINUX_ARM64:-$(fetch_checksum "tally_${VERSION}_Linux_arm64.tar.gz")}"
SHA256_LINUX_AMD64="${SHA256_LINUX_AMD64:-$(fetch_checksum "tally_${VERSION}_Linux_x86_64.tar.gz")}"

echo "Generating formula for tally v${VERSION}..." >&2
echo "  Darwin ARM64: ${SHA256_DARWIN_ARM64}" >&2
echo "  Darwin AMD64: ${SHA256_DARWIN_AMD64}" >&2
echo "  Linux ARM64:  ${SHA256_LINUX_ARM64}" >&2
echo "  Linux AMD64:  ${SHA256_LINUX_AMD64}" >&2

# Generate formula from template
sed -e "s/{{VERSION}}/${VERSION}/g" \
    -e "s/{{SHA256_DARWIN_ARM64}}/${SHA256_DARWIN_ARM64}/g" \
    -e "s/{{SHA256_DARWIN_AMD64}}/${SHA256_DARWIN_AMD64}/g" \
    -e "s/{{SHA256_LINUX_ARM64}}/${SHA256_LINUX_ARM64}/g" \
    -e "s/{{SHA256_LINUX_AMD64}}/${SHA256_LINUX_AMD64}/g" \
    "$TEMPLATE_FILE" > "$OUTPUT_FILE"

echo "Formula generated: ${OUTPUT_FILE}" >&2
