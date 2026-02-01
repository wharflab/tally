#!/usr/bin/env bash
#
# Extract Hadolint rule metadata from source code.
#
# Uses:
#   - jq for JSON parsing (contrib/hadolint.json)
#   - ast-grep for Haskell AST parsing (src/Hadolint/Rule/*.hs)
#
# Output: JSON array of rules with code, description, severity, wiki_url
#
# Usage:
#   ./scripts/extract-hadolint-rules.sh /path/to/hadolint  # Extract from repo
#   HADOLINT_REPO=/path/to/hadolint ./scripts/extract-hadolint-rules.sh
#
# Options:
#   --md         Output as Markdown table
#   --summary    Statistics only
#   --only-dl    DL rules only
#   --only-sc    SC rules only
#   --help       Show help
#
# Requirements: jq, ast-grep

set -euo pipefail

# Hadolint repo can be set via env or first positional arg
HADOLINT_REPO="${HADOLINT_REPO:-}"
SCHEMA_FILE=""
RULES_DIR=""

init_paths() {
    if [[ -z "$HADOLINT_REPO" ]]; then
        echo "Error: HADOLINT_REPO not set and no path provided" >&2
        echo "Usage: $0 /path/to/hadolint [OPTIONS]" >&2
        exit 1
    fi
    if [[ ! -d "$HADOLINT_REPO" ]]; then
        echo "Error: Directory not found: $HADOLINT_REPO" >&2
        exit 1
    fi
    SCHEMA_FILE="$HADOLINT_REPO/contrib/hadolint.json"
    RULES_DIR="$HADOLINT_REPO/src/Hadolint/Rule"

    if [[ ! -f "$SCHEMA_FILE" ]]; then
        echo "Error: Schema not found: $SCHEMA_FILE" >&2
        exit 1
    fi
    if [[ ! -d "$RULES_DIR" ]]; then
        echo "Error: Rules dir not found: $RULES_DIR" >&2
        exit 1
    fi
}

show_help() {
    cat << 'EOF'
Usage: ./scripts/extract-hadolint-rules.sh [HADOLINT_REPO] [OPTIONS]

Extract Hadolint rule metadata from source code.

Arguments:
  HADOLINT_REPO   Path to hadolint repository (or set via env var)

Options:
  --json       Output as JSON (default)
  --md         Output as Markdown table
  --summary    Output summary statistics
  --only-dl    Include only DL (Dockerfile) rules
  --only-sc    Include only SC (ShellCheck) rules
  --help       Show this help message

Examples:
  ./scripts/extract-hadolint-rules.sh /tmp/hadolint           # All rules as JSON
  ./scripts/extract-hadolint-rules.sh /tmp/hadolint --only-dl # DL rules only
  HADOLINT_REPO=/tmp/hadolint ./scripts/extract-hadolint-rules.sh --summary

Requirements:
  - jq (JSON processor)
  - ast-grep (AST-based code search)
EOF
}

# Check dependencies
check_deps() {
    local missing=()
    command -v jq &>/dev/null || missing+=("jq")
    command -v ast-grep &>/dev/null || missing+=("ast-grep")

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "Error: Missing required dependencies: ${missing[*]}" >&2
        echo "Install with: brew install ${missing[*]}" >&2
        exit 1
    fi
}

# Extract rules from JSON schema (descriptions + wiki URLs)
# Output: JSON object keyed by rule code
extract_from_schema() {
    jq -r '
        .properties.ignored.items.oneOf // []
        | map(select(.const and .description))
        | map({
            key: .const,
            value: {
                code: .const,
                description: .description,
                wiki_url: (."$comment" // null)
            }
        })
        | from_entries
    ' "$SCHEMA_FILE"
}

# Extract all DL rules from Haskell source files using ast-grep (optimized)
# Runs ast-grep once on entire directory for each pattern, then joins results
# Output: JSON object keyed by rule code
extract_from_source() {
    # Run ast-grep once for severity across all files
    local sev_json
    sev_json=$(ast-grep --lang haskell --pattern 'severity = $SEV' "$RULES_DIR" --json 2>/dev/null || echo "[]")

    # Run ast-grep once for message across all files
    local msg_json
    msg_json=$(ast-grep --lang haskell --pattern 'message = $MSG' "$RULES_DIR" --json 2>/dev/null || echo "[]")

    # Process and merge results using jq
    jq -n \
        --argjson sev "$sev_json" \
        --argjson msg "$msg_json" \
        --argjson sev_map '{"DLErrorC":"Error","DLWarningC":"Warning","DLInfoC":"Info","DLStyleC":"Style","DLIgnoreC":"Ignore"}' \
    '
        # Build severity map: code -> severity
        ($sev | map(
            (.file | capture("/(?<code>DL[0-9]{4})\\.hs$").code) as $code |
            (.metaVariables.single.SEV.text) as $sev_raw |
            {key: $code, value: $sev_map[$sev_raw] // "Unknown"}
        ) | from_entries) as $severities |

        # Build message map: code -> message (cleaned)
        ($msg | map(
            (.file | capture("/(?<code>DL[0-9]{4})\\.hs$").code) as $code |
            (.metaVariables.single.MSG.text) as $msg_raw |
            # Clean Haskell string: remove quotes, fix multiline continuation
            ($msg_raw |
                ltrimstr("\"") | rtrimstr("\"") |
                gsub("\\\\\n\\s*\\\\"; " ") |
                gsub("\\\\\""; "\"") |
                gsub("\\s+"; " ") |
                gsub("^ +| +$"; "")
            ) as $clean_msg |
            {key: $code, value: $clean_msg}
        ) | from_entries) as $messages |

        # Merge into result object
        ($severities | keys) | map(. as $code | {
            key: $code,
            value: {
                severity: $severities[$code],
                message: $messages[$code] // ""
            }
        }) | from_entries
    '
}

# Merge schema and source data into final rules array
merge_rules() {
    local schema_rules="$1"
    local source_rules="$2"

    jq -n --argjson schema "$schema_rules" --argjson source "$source_rules" '
        # Start with source rules (authoritative for DL rule list)
        ($source | keys) as $source_codes |

        # Build DL rules array
        [
            $source_codes[] | . as $code |
            {
                code: $code,
                description: (
                    $schema[$code].description //
                    $source[$code].message //
                    "No description available"
                ),
                wiki_url: (
                    $schema[$code].wiki_url //
                    "https://github.com/hadolint/hadolint/wiki/\($code)"
                ),
                severity: (
                    $source[$code].severity |
                    if . == "" then "Unknown" else . end
                )
            }
        ] +
        # Add any DL rules from schema not in source (safety)
        [
            $schema | to_entries[] |
            select(.key | startswith("DL")) |
            select(.key | IN($source_codes[]) | not) |
            .value + {severity: "Unknown"}
        ] +
        # Add SC rules from schema
        [
            $schema | to_entries[] |
            select(.key | startswith("SC")) |
            .value + {severity: "Varies"}
        ]
        | sort_by(.code | capture("^(?<prefix>[A-Z]+)(?<num>[0-9]+)$") | [.prefix, (.num | tonumber)])
    '
}

# Format as JSON
format_json() {
    jq '.'
}

# Format as Markdown table
format_markdown() {
    jq -r '
        ["| Rule | Description | Severity |", "|------|-------------|----------|"] +
        [.[] | "| [\(.code)](\(.wiki_url)) | \(.description | gsub("\\|"; "\\|")) | \(.severity) |"]
        | .[]
    '
}

# Format as summary
format_summary() {
    jq -r '
        . as $all |
        [.[] | select(.code | startswith("DL"))] as $dl |
        [.[] | select(.code | startswith("SC"))] as $sc |
        ($dl | group_by(.severity) | map({key: .[0].severity, value: length}) | from_entries) as $sev_counts |

        "Total rules: \($all | length)",
        "  DL rules: \($dl | length)",
        "  SC rules: \($sc | length)",
        "",
        "DL rules by severity:",
        ($sev_counts | to_entries | sort_by(.key) | .[] | "  \(.key): \(.value)")
    '
}

# Main
main() {
    local format="json"
    local filter="all"

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --help|-h)
                show_help
                exit 0
                ;;
            --json)
                format="json"
                shift
                ;;
            --md)
                format="md"
                shift
                ;;
            --summary)
                format="summary"
                shift
                ;;
            --only-dl)
                filter="dl"
                shift
                ;;
            --only-sc)
                filter="sc"
                shift
                ;;
            -*)
                echo "Unknown option: $1" >&2
                show_help >&2
                exit 1
                ;;
            *)
                # Positional arg = hadolint repo path
                HADOLINT_REPO="$1"
                shift
                ;;
        esac
    done

    check_deps
    init_paths

    # Extract from both sources
    local schema_rules source_rules
    schema_rules=$(extract_from_schema)
    source_rules=$(extract_from_source)

    # Merge into final rules
    local rules
    rules=$(merge_rules "$schema_rules" "$source_rules")

    # Apply filter
    case "$filter" in
        dl)
            rules=$(echo "$rules" | jq '[.[] | select(.code | startswith("DL"))]')
            ;;
        sc)
            rules=$(echo "$rules" | jq '[.[] | select(.code | startswith("SC"))]')
            ;;
    esac

    # Output in requested format
    case "$format" in
        json)
            echo "$rules" | format_json
            ;;
        md)
            echo "$rules" | format_markdown
            ;;
        summary)
            echo "$rules" | format_summary
            ;;
    esac
}

main "$@"
