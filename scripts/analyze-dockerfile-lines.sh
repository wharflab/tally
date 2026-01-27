#!/bin/bash
# Analyze Dockerfile line counts from GitHub to determine a good default for max-lines
# Usage: ./scripts/analyze-dockerfile-lines.sh [count]
# Default: 500 files

set -euo pipefail

COUNT=${1:-500}
TEMP_FILE=$(mktemp)
trap 'rm -f "$TEMP_FILE"' EXIT

echo "Searching for Dockerfiles on GitHub (target: $COUNT files)..."
echo "This may take a few minutes due to API rate limits."
echo

# GitHub search API returns max 100 results per page, max 1000 total
# We'll paginate and collect line counts
collected=0
page=1
per_page=100

while [ $collected -lt $COUNT ]; do
    remaining=$((COUNT - collected))
    fetch_count=$((remaining < per_page ? remaining : per_page))

    echo "Fetching page $page ($collected/$COUNT collected)..."

    # Search for Dockerfiles, sorted by recently indexed for variety
    # Using code search to find actual Dockerfile content
    results=$(gh api "search/code?q=FROM+filename:Dockerfile+language:Dockerfile&per_page=$fetch_count&page=$page" \
        --jq '.items[] | "\(.repository.full_name) \(.path)"' 2>/dev/null || echo "")

    if [ -z "$results" ]; then
        echo "No more results or rate limited. Collected $collected files."
        break
    fi

    # Process each result
    while IFS=' ' read -r repo path; do
        if [ $collected -ge $COUNT ]; then
            break
        fi

        # Fetch file content and count lines
        lines=$(gh api "repos/$repo/contents/$path" --jq '.content' 2>/dev/null | base64 -d 2>/dev/null | wc -l | tr -d ' ' || echo "0")

        if [ "$lines" -gt 0 ]; then
            echo "$lines" >> "$TEMP_FILE"
            collected=$((collected + 1))

            # Progress indicator every 50 files
            if [ $((collected % 50)) -eq 0 ]; then
                echo "  Progress: $collected/$COUNT files analyzed"
            fi
        fi

        # Small delay to avoid rate limiting
        sleep 0.1
    done <<< "$results"

    page=$((page + 1))

    # GitHub search API limit is 10 pages (1000 results)
    if [ $page -gt 10 ]; then
        echo "Reached GitHub search API limit (1000 results max)."
        break
    fi
done

echo
echo "=== Results ==="
echo "Files analyzed: $(wc -l < "$TEMP_FILE" | tr -d ' ')"
echo

if [ ! -s "$TEMP_FILE" ]; then
    echo "No data collected. Check your GitHub authentication (gh auth status)."
    exit 1
fi

# Sort numerically for percentile calculation
sort -n "$TEMP_FILE" > "${TEMP_FILE}.sorted"
mv "${TEMP_FILE}.sorted" "$TEMP_FILE"

total=$(wc -l < "$TEMP_FILE" | tr -d ' ')

# Calculate statistics
min=$(head -1 "$TEMP_FILE")
max=$(tail -1 "$TEMP_FILE")
median_idx=$(( (total + 1) / 2 ))
median=$(sed -n "${median_idx}p" "$TEMP_FILE")

# P90 = 90th percentile (90% of files have this many lines or fewer)
p90_idx=$(( (total * 90 + 99) / 100 ))
p90=$(sed -n "${p90_idx}p" "$TEMP_FILE")

# P95 and P99 for reference
p95_idx=$(( (total * 95 + 99) / 100 ))
p95=$(sed -n "${p95_idx}p" "$TEMP_FILE")

p99_idx=$(( (total * 99 + 99) / 100 ))
p99=$(sed -n "${p99_idx}p" "$TEMP_FILE")

# Mean
sum=$(awk '{s+=$1} END {print s}' "$TEMP_FILE")
mean=$(( sum / total ))

echo "Line count statistics:"
echo "  Min:    $min"
echo "  Max:    $max"
echo "  Mean:   $mean"
echo "  Median: $median"
echo
echo "Percentiles:"
echo "  P90:    $p90 (90% of Dockerfiles have $p90 lines or fewer)"
echo "  P95:    $p95"
echo "  P99:    $p99"
echo
echo "Recommendation: Use $p90 as the default max-lines value"
echo "This allows 90% of typical Dockerfiles while flagging unusually long ones."

# Save raw data for further analysis
echo
echo "Raw line counts saved to: dockerfile-lines.txt"
cp "$TEMP_FILE" dockerfile-lines.txt
