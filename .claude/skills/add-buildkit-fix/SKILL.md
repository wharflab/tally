---
name: add-buildkit-fix
description: Add auto-fix support to an existing BuildKit linter rule
argument-hint: rule-name (e.g. StageNameCasing, FromAsCasing, NoEmptyContinuation)
disable-model-invocation: true
---

# Add Auto-Fix to BuildKit Rule

You are adding auto-fix support to an existing BuildKit linter rule for the `tally` project.

## Rule to Add Fix: $ARGUMENTS

## Step 1: Verify the Rule is Being Captured

**First, confirm tally is already reporting this rule as a violation.**

```bash
# Create a test Dockerfile that should trigger the rule
# (adjust content based on the specific rule)
echo 'FROM alpine AS Builder' > /tmp/test.dockerfile
go run . check --format json /tmp/test.dockerfile 2>&1 | jq '.files[0].violations[] | {rule, message}'
```

If the rule doesn't appear:

- Check `internal/rules/buildkit/registry.go` to see if the rule is registered
- Some rules come from **parser-level warnings** (`ast.Warnings`) rather than the linter callback
- Parser warnings may need to be captured in `internal/dockerfile/parser.go` first

## Step 2: Understand the Rule

1. **Fetch Docker documentation:**

   ```text
   https://docs.docker.com/reference/build-checks/$ARGUMENTS-in-kebab-case/
   ```

   Example: `StageNameCasing` → `stage-name-casing`

2. **Check existing snapshots** for the message format:

   ```bash
   grep -r "$ARGUMENTS" internal/integration/__snapshots__/
   ```

3. **Read the enricher pattern** in `internal/rules/buildkit/fixes/enricher.go`

## Step 3: Implement the Fix

### 3a: Create the Enricher Function

Create `internal/rules/buildkit/fixes/$ARGUMENTS_snake_case.go`:

```go
package fixes

import (
    "github.com/wharflab/tally/internal/rules"
)

// enrich${ARGUMENTS}Fix adds auto-fix for BuildKit's $ARGUMENTS rule.
func enrich${ARGUMENTS}Fix(v *rules.Violation, source []byte) {
    // 1. Get the source line (getLine uses 0-based index)
    lineIdx := v.Location.Start.Line - 1
    line := getLine(source, lineIdx)
    if line == nil {
        return
    }

    // 2. Find what needs to change (use position helpers or tokenizer)
    // ...

    // 3. Create the fix
    v.SuggestedFix = &rules.SuggestedFix{
        Description: "Description of what the fix does",
        Safety:      rules.FixSafe,
        Edits: []rules.TextEdit{{
            // createEditLocation takes 1-based line numbers
            Location: createEditLocation(v.Location.File, v.Location.Start.Line, startCol, endCol),
            NewText:  "replacement",
        }},
        IsPreferred: true,
    }
}
```

**If the fix needs the semantic model** (for cross-instruction references):

```go
func enrich${ARGUMENTS}Fix(v *rules.Violation, sem *semantic.Model, source []byte) {
    if sem == nil {
        return
    }
    // Use sem.StageIndexByName(), sem.StageInfo(), etc.
}
```

### 3b: Register in Enricher

Add to the switch in `internal/rules/buildkit/fixes/enricher.go`:

```go
case "$ARGUMENTS":
    enrich${ARGUMENTS}Fix(v, source)
    // Or with semantic model:
    // enrich${ARGUMENTS}Fix(v, sem, source)
```

## Step 4: Handle Special Edit Types

### Text Replacement (most common)

```go
Location: createEditLocation(file, lineNum, startCol, endCol),
NewText:  "replacement",
```

### Line Deletion

To delete an entire line, span from line N to line N+1:

```go
// Delete line 3 (including its newline)
Location: rules.NewRangeLocation(file, lineNum, 0, lineNum+1, 0),
NewText:  "",
```

### Multi-line Edits

The fixer applies edits from end to start, so line shifts are handled automatically.

## Step 5: Line Number Conventions

| Context                                   | Convention              |
| ----------------------------------------- | ----------------------- |
| `v.Location.Start.Line`                   | 1-based (from BuildKit) |
| `getLine(source, idx)`                    | 0-based index           |
| `createEditLocation(file, line, ...)`     | 1-based line            |
| `rules.NewRangeLocation(file, line, ...)` | 1-based line            |

**Common pattern:**

```go
lineIdx := v.Location.Start.Line - 1  // Convert to 0-based for getLine
line := getLine(source, lineIdx)
// ... find positions within line ...
// Use v.Location.Start.Line (1-based) for createEditLocation
```

## Step 6: Write Tests

Add to `internal/rules/buildkit/fixes/fixes_test.go`:

```go
func Test${ARGUMENTS}Fix(t *testing.T) {
    tests := []struct {
        name      string
        source    string
        wantFix   bool
        wantEdits int
    }{
        {
            name:      "should fix",
            source:    "...",
            wantFix:   true,
            wantEdits: 1,
        },
        {
            name:    "already correct",
            source:  "...",
            wantFix: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            source := []byte(tt.source)
            v := rules.Violation{
                Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, len(tt.source)),
                RuleCode: rules.BuildKitRulePrefix + "$ARGUMENTS",
                Message:  "...", // Match BuildKit's actual message
            }

            enrich${ARGUMENTS}Fix(&v, source)

            if tt.wantFix {
                require.NotNil(t, v.SuggestedFix)
                assert.Len(t, v.SuggestedFix.Edits, tt.wantEdits)
            } else {
                assert.Nil(t, v.SuggestedFix)
            }
        })
    }
}
```

## Step 7: Add Integration Tests

### Create test fixture

```bash
mkdir -p internal/integration/testdata/$ARGUMENTS-kebab-case
# Create Dockerfile that triggers the rule
```

### Add to TestCheck (detection test)

In `internal/integration/integration_test.go`:

```go
{name: "$ARGUMENTS-kebab-case", dir: "$ARGUMENTS-kebab-case", args: []string{"--format", "json"}, wantExit: 1},
```

### Add to TestFix (fix test)

```go
{
    name:        "$ARGUMENTS-fix",
    input:       "...\n",
    want:        "...\n",
    args:        []string{"--fix"},
    wantApplied: 1,
},
```

## Step 8: Run All Checks

```bash
# Unit tests
go test ./internal/rules/buildkit/fixes/... -v

# All tests
go test ./...

# Linter
make lint

# Update snapshots
UPDATE_SNAPS=true go test ./internal/integration/...

# Manual verification
go run . check --fix /tmp/test.dockerfile && cat /tmp/test.dockerfile
```

## Step 9: Update Documentation

In `RULES.md`, add 🔧 emoji to the rule:

```markdown
| `buildkit/$ARGUMENTS` | Description | Warning | ✅🔧 Captured |
```

## Fix Safety Levels

| Level                 | When to Use                                     |
| --------------------- | ----------------------------------------------- |
| `rules.FixSafe`       | Casing changes, removing whitespace, formatting |
| `rules.FixSuggestion` | Semantic changes that are usually correct       |
| `rules.FixUnsafe`     | Changes that might alter behavior               |

## Position Helpers Available

- `getLine(source, lineIdx)` - Get line content (0-based index)
- `createEditLocation(file, line, startCol, endCol)` - Create edit location (1-based line)
- `ParseInstruction(line)` - Tokenizer for instruction parsing
  - `.FindKeyword("AS")` - Find keyword token
  - `.FindFlag("from")` - Find flag like `--from`
  - `.Arguments()` - Get argument tokens

## Checklist

- [ ] Rule violations appear in `go run . check --format json`
- [ ] Fix enricher created in `internal/rules/buildkit/fixes/`
- [ ] Enricher registered in `enricher.go` switch
- [ ] Unit tests in `fixes_test.go`
- [ ] Integration test fixture in `testdata/`
- [ ] Integration test cases in `integration_test.go` (TestCheck + TestFix)
- [ ] `go test ./...` passes
- [ ] `make lint` passes
- [ ] Manual `--fix` verification works
- [ ] RULES.md updated with 🔧 emoji
