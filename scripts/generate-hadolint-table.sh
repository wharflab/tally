#!/usr/bin/env bash
#
# Generate Hadolint rules table for RULES.md
#
# Merges:
#   1. Hadolint rules (from hadolint repo extract script or cached JSON)
#   2. Implementation status (internal/rules/hadolint-status.json)
#
# Output: Markdown table for RULES.md
#
# Usage:
#   ./scripts/generate-hadolint-table.sh                           # Use cached rules
#   ./scripts/generate-hadolint-table.sh /path/to/hadolint-rules.json  # Use specific file
#   HADOLINT_REPO=/path/to/hadolint ./scripts/generate-hadolint-table.sh --extract  # Extract fresh
#
# Requirements: jq

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
STATUS_FILE="$ROOT_DIR/internal/rules/hadolint-status.json"
CACHED_RULES="$ROOT_DIR/internal/rules/hadolint-rules.json"

RULES_MD="$ROOT_DIR/RULES.md"
BEGIN_MARKER="<!-- BEGIN HADOLINT_DL_RULES -->"
END_MARKER="<!-- END HADOLINT_DL_RULES -->"

show_help() {
    cat << 'EOF'
Usage: ./scripts/generate-hadolint-table.sh [OPTIONS] [RULES_FILE]

Generate Hadolint rules table for RULES.md by merging:
  - Hadolint rule definitions (code, description, severity)
  - Tally implementation status (implemented, covered_by_buildkit, not_implemented)

Options:
  --extract     Extract fresh rules from HADOLINT_REPO (requires ast-grep)
  --update      Update RULES.md in place (replaces content between markers)
  --summary     Output summary statistics instead of table
  --json        Output merged data as JSON
  --help        Show this help

Arguments:
  RULES_FILE    Path to hadolint rules JSON (default: internal/rules/hadolint-rules.json)

Environment:
  HADOLINT_REPO   Path to hadolint repository (for --extract)

Examples:
  ./scripts/generate-hadolint-table.sh                    # Output table to stdout
  ./scripts/generate-hadolint-table.sh --update           # Update RULES.md in place
  ./scripts/generate-hadolint-table.sh --summary          # Show statistics
  HADOLINT_REPO=~/hadolint ./scripts/generate-hadolint-table.sh --extract --update
EOF
}

# Check dependencies
check_deps() {
    if ! command -v jq &>/dev/null; then
        echo "Error: jq is required" >&2
        exit 1
    fi
}

# Extract rules from hadolint repo
extract_rules() {
    local repo="${HADOLINT_REPO:-}"
    if [[ -z "$repo" ]]; then
        echo "Error: HADOLINT_REPO not set" >&2
        exit 1
    fi

    local script="$SCRIPT_DIR/extract-hadolint-rules.sh"
    if [[ ! -x "$script" ]]; then
        echo "Error: $script not found or not executable" >&2
        exit 1
    fi

    "$script" "$repo" --only-dl
}

# Merge rules with status
merge_rules() {
    local rules_file="$1"

    jq -n \
        --slurpfile rules "$rules_file" \
        --slurpfile status "$STATUS_FILE" \
    '
        ($status[0].rules // {}) as $impl_status |

        $rules[0] | map(
            .code as $code |
            ($impl_status[$code] // {status: "not_implemented"}) as $status |

            . + {
                impl_status: $status.status,
                tally_rule: ($status.tally_rule // null),
                buildkit_rule: ($status.buildkit_rule // null),
                fixable: ($status.fixable // false)
            }
        )
    '
}

# Format as markdown table
format_markdown() {
    jq -r '
        ["| Rule | Description | Severity | Status |",
         "|------|-------------|----------|--------|"] +
        [.[] |
            # Format status column with optional fixable indicator
            (if .impl_status == "implemented" then
                if .fixable then "✅🔧 `\(.tally_rule)`"
                else "✅ `\(.tally_rule)`"
                end
            elif .impl_status == "covered_by_buildkit" then
                "🔄 `buildkit/\(.buildkit_rule)`"
            elif .impl_status == "covered_by_tally" then
                (.tally_rule | split("/") | .[1]) as $slug |
                "🔄 [`\(.tally_rule)`](docs/rules/tally/\($slug).md)"
            else
                "⏳"
            end) as $status_str |

            # Format rule link
            "[\(.code)](\(.wiki_url))" as $rule_link |

            # Escape pipes in description
            (.description | gsub("\\|"; "\\|")) as $desc |

            "| \($rule_link) | \($desc) | \(.severity) | \($status_str) |"
        ]
        | .[]
    '
}

# Update RULES.md between markers
update_rules_md() {
    local new_table="$1"

    if [[ ! -f "$RULES_MD" ]]; then
        echo "Error: RULES.md not found at $RULES_MD" >&2
        return 1
    fi

    # Check markers exist
    if ! grep -q "$BEGIN_MARKER" "$RULES_MD"; then
        echo "Error: Begin marker not found in RULES.md" >&2
        echo "Add '$BEGIN_MARKER' before the DL rules table" >&2
        return 1
    fi
    if ! grep -q "$END_MARKER" "$RULES_MD"; then
        echo "Error: End marker not found in RULES.md" >&2
        echo "Add '$END_MARKER' after the DL rules table" >&2
        return 1
    fi

    # Write new table to temp file
    local tmp_table
    tmp_table=$(mktemp)
    echo "$new_table" > "$tmp_table"

    # Extract parts: before marker, after marker
    local tmp_before tmp_after
    tmp_before=$(mktemp)
    tmp_after=$(mktemp)

    # Get content before and including BEGIN marker
    sed -n "1,/$BEGIN_MARKER/p" "$RULES_MD" > "$tmp_before"

    # Get content from END marker onwards
    sed -n "/$END_MARKER/,\$p" "$RULES_MD" > "$tmp_after"

    # Combine: before + table + after
    cat "$tmp_before" "$tmp_table" "$tmp_after" > "$RULES_MD"

    rm -f "$tmp_table" "$tmp_before" "$tmp_after"
    echo "Updated $RULES_MD"
}

# Format summary
format_summary() {
    jq -r '
        (length) as $total |
        ([.[] | select(.impl_status == "implemented")] | length) as $implemented |
        ([.[] | select(.impl_status == "covered_by_buildkit")] | length) as $covered_bk |
        ([.[] | select(.impl_status == "covered_by_tally")] | length) as $covered_tally |
        ($covered_bk + $covered_tally) as $covered |
        ($total - $implemented - $covered) as $pending |

        "Hadolint DL Rules Status:",
        "  Total: \($total)",
        "  Implemented by tally: \($implemented)",
        "  Covered by BuildKit: \($covered_bk)",
        "  Covered by tally rule: \($covered_tally)",
        "  Not yet implemented: \($pending)",
        "",
        "Coverage: \((($implemented + $covered) * 100 / $total) | floor)%"
    '
}

# Main
main() {
    local mode="table"
    local rules_file="$CACHED_RULES"
    local do_extract=false
    local do_update=false

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --help|-h)
                show_help
                exit 0
                ;;
            --extract)
                do_extract=true
                shift
                ;;
            --update)
                do_update=true
                shift
                ;;
            --summary)
                mode="summary"
                shift
                ;;
            --json)
                mode="json"
                shift
                ;;
            -*)
                echo "Unknown option: $1" >&2
                exit 1
                ;;
            *)
                rules_file="$1"
                shift
                ;;
        esac
    done

    check_deps

    # Extract or use cached
    local rules_data
    if [[ "$do_extract" == true ]]; then
        rules_data=$(extract_rules)
    elif [[ -f "$rules_file" ]]; then
        rules_data=$(cat "$rules_file")
    else
        echo "Error: Rules file not found: $rules_file" >&2
        echo "Run with --extract to fetch from hadolint repo, or provide a rules file" >&2
        exit 1
    fi

    # Write to temp file for jq --slurpfile
    local tmp_rules
    tmp_rules=$(mktemp)
    echo "$rules_data" > "$tmp_rules"
    trap "rm -f '$tmp_rules'" EXIT

    # Merge and format
    local merged
    merged=$(merge_rules "$tmp_rules")

    # Handle update mode
    if [[ "$do_update" == true ]]; then
        local table
        table=$(echo "$merged" | format_markdown)
        update_rules_md "$table"
        echo "$merged" | format_summary
        return
    fi

    # Output in requested format
    case "$mode" in
        table)
            echo "$merged" | format_markdown
            ;;
        summary)
            echo "$merged" | format_summary
            ;;
        json)
            echo "$merged" | jq '.'
            ;;
    esac
}

main "$@"
