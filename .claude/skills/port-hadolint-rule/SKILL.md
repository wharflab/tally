---
name: port-hadolint-rule
description: Port a Hadolint rule from Haskell to Go implementation
argument-hint: rule-code (e.g. DL3022)
disable-model-invocation: true
allowed-tools: Read, Write, Edit, Grep, Glob, Bash(go *), Bash(git status), mcp__github__get_file_contents, mcp__github__search_code
---

# Port Hadolint Rule to Go

You are porting a Hadolint rule from Haskell to Go for the `tally` project.

## Rule to Port: $ARGUMENTS

## Step 1: Fetch Original Haskell Implementation

Use the GitHub MCP tools to fetch the original Haskell implementation:

1. First, try to get the rule implementation file directly:
   - Use `mcp__github__get_file_contents` with:
     - `owner`: "hadolint"
     - `repo`: "hadolint"
     - `path`: "src/Hadolint/Rule/$ARGUMENTS.hs"
     - `branch`: "master"

2. If that fails, use `mcp__github__search_code` to find the rule:
   - Search in `repo:hadolint/hadolint` for the rule code

3. Carefully analyze the Haskell implementation to understand:
   - What Dockerfile instructions it checks (RUN, COPY, FROM, etc.)
   - The exact conditions that trigger a violation
   - The error message format
   - Any edge cases handled

## Step 2: Fetch ALL Original Test Cases

Use GitHub MCP tools to get the test specification:

1. Try to get the test file directly:
   - Use `mcp__github__get_file_contents` with:
     - `owner`: "hadolint"
     - `repo`: "hadolint"
     - `path`: "test/Hadolint/Rule/$ARGUMENTSSpec.hs"
     - `branch`: "master"

2. If that fails, search for the test file:
   - Use `mcp__github__search_code` in `repo:hadolint/hadolint` for "$ARGUMENTSSpec"

3. Extract ALL test cases from the spec file - both passing and failing cases
   - `ruleCatches` indicates the rule SHOULD trigger (expect violation)
   - `ruleCatchesNot` indicates the rule should NOT trigger (expect no violation)

## Step 3: Analyze Existing Patterns

Before implementing, read these files to understand the patterns:

1. Read `internal/rules/hadolint/dl3004.go` - a standard rule implementation
2. Read `internal/rules/hadolint/dl3012.go` - a pointer file for semantic-based rules
3. Read `internal/shell/shell.go` - shell parsing utilities
4. Read `internal/shell/packages.go` - package manager parsing
5. Read `internal/semantic/semantic.go` - semantic model
6. Read `internal/semantic/builder.go` - semantic model builder
7. Read `internal/rules/rule.go` - Rule interface and LintInput

## Step 4: Determine Implementation Location

Decide where to implement based on rule nature:

### Option A: Standard Rule (internal/rules/hadolint/$ARGUMENTS.go)

- For rules checking specific instructions (RUN commands, COPY sources, etc.)
- For rules that can iterate through stages and commands

### Option B: Semantic Model (internal/semantic/builder.go) + Pointer File

- For rules requiring cross-instruction analysis
- For rules checking duplicate instructions (like DL3012 for HEALTHCHECK)
- For rules checking stage references
- Create pointer file at `internal/rules/hadolint/$ARGUMENTS.go` documenting the semantic implementation

## Step 5: Implementation Requirements

### CRITICAL: Shell Command Parsing

**NEVER parse shell commands using regex or string operations.**

Always use the `internal/shell` package:

```go
import "github.com/tinovyatkin/tally/internal/shell"

// To check if a command contains a specific command name:
if shell.ContainsCommandWithVariant(cmdStr, "sudo", shellVariant) {
    // violation
}

// To get all command names:
commands := shell.CommandNamesWithVariant(cmdStr, shellVariant)

// To extract package installations:
installs := shell.ExtractPackageInstalls(cmdStr, shellVariant)
```

### CRITICAL: Use Semantic Model

Always leverage the semantic model (`internal/semantic/`):

```go
// Get semantic model from input
sem, ok := input.Semantic.(*semantic.Model)
if !ok {
    sem = nil
}

// Use for shell variant detection
if sem != nil {
    if info := sem.StageInfo(stageIdx); info != nil {
        shellVariant = info.ShellSetting.Variant
        // Skip non-POSIX shells if rule is shell-specific
        if shellVariant.IsNonPOSIX() {
            continue
        }
    }
}

// Use for stage information
for info := range sem.ExternalImageStages() {
    // Check external image references
}
```

### Enhance Semantic Model If Needed

If the rule requires semantic information not yet tracked:

1. Add fields to `StageInfo` in `internal/semantic/stage_info.go`
2. Populate fields in `internal/semantic/builder.go`
3. Document the enhancement

### Rule Structure Template

```go
package hadolint

import (
    "github.com/moby/buildkit/frontend/dockerfile/instructions"

    "github.com/tinovyatkin/tally/internal/rules"
    "github.com/tinovyatkin/tally/internal/semantic"
    "github.com/tinovyatkin/tally/internal/shell"
)

// $ARGUMENTSRule implements the $ARGUMENTS linting rule.
type $ARGUMENTSRule struct{}

// New$ARGUMENTSRule creates a new $ARGUMENTS rule instance.
func New$ARGUMENTSRule() *$ARGUMENTSRule {
    return &$ARGUMENTSRule{}
}

// Metadata returns the rule metadata.
func (r *$ARGUMENTSRule) Metadata() rules.RuleMetadata {
    return rules.RuleMetadata{
        Code:            rules.HadolintRulePrefix + "$ARGUMENTS",
        Name:            "...", // Short name from Hadolint wiki
        Description:     "...", // Description from Hadolint wiki
        DocURL:          "https://github.com/hadolint/hadolint/wiki/$ARGUMENTS",
        DefaultSeverity: rules.SeverityWarning, // or SeverityError based on original
        Category:        "...", // security, performance, style, etc.
        IsExperimental:  false,
    }
}

// Check runs the $ARGUMENTS rule.
func (r *$ARGUMENTSRule) Check(input rules.LintInput) []rules.Violation {
    var violations []rules.Violation
    meta := r.Metadata()

    // Get semantic model
    sem, ok := input.Semantic.(*semantic.Model)
    if !ok {
        sem = nil
    }

    // Implementation...

    return violations
}

// init registers the rule with the default registry.
func init() {
    rules.Register(New$ARGUMENTSRule())
}
```

## Step 6: Write Tests

Create test file `internal/rules/hadolint/$ARGUMENTS_test.go`:

1. Include ALL test cases from the original Hadolint spec
2. Follow the pattern in `internal/rules/hadolint/dl3004_test.go`
3. Use `testutil.MakeLintInput` to create test inputs

```go
func Test$ARGUMENTSRule_Check(t *testing.T) {
    tests := []struct {
        name       string
        dockerfile string
        wantCount  int
    }{
        // Add ALL cases from original Hadolint spec
        // ruleCatches cases -> wantCount: 1 (or more)
        // ruleCatchesNot cases -> wantCount: 0
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
            r := New$ARGUMENTSRule()
            violations := r.Check(input)

            if len(violations) != tt.wantCount {
                t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
            }
        })
    }
}
```

## Step 7: Verify Implementation

Run the tests to verify:

```bash
go test ./internal/rules/hadolint/... -run $ARGUMENTS -v
```

Ensure ALL original Hadolint test cases pass.

## Step 8: Add Integration Test

Add an integration test case for the new rule:

1. Create directory `internal/integration/testdata/$ARGUMENTS/`
2. Add a `Dockerfile` that triggers the rule
3. Add test case to `internal/integration/integration_test.go`
4. Run `UPDATE_SNAPS=true go test ./internal/integration/...`

## Step 9: Update Hadolint Status Tracking

After implementation is complete, update the tracking files:

1. **Update hadolint-status.json**:

   Add an entry to `internal/rules/hadolint-status.json`:

   ```json
   "$ARGUMENTS": {
     "status": "implemented",
     "tally_rule": "hadolint/$ARGUMENTS"
   }
   ```

   Place it in alphabetical order among the other rules.

2. **Regenerate documentation**:

   ```bash
   ./scripts/generate-hadolint-table.sh --update
   ```

   This updates the Hadolint compatibility table in the documentation.

3. **Update all integration test snapshots** (if needed):

   ```bash
   UPDATE_SNAPS=true go test ./internal/integration/...
   ```

   This updates the `rules_enabled` count in all snapshots.

## Checklist Before Completion

- [ ] Original Haskell implementation analyzed
- [ ] ALL test cases from Hadolint spec extracted
- [ ] Rule implemented using `internal/shell` for command parsing (no regex)
- [ ] Semantic model used where appropriate
- [ ] `init()` function registers the rule
- [ ] Unit tests cover ALL original test cases
- [ ] Tests pass: `go test ./internal/rules/hadolint/... -run $ARGUMENTS -v`
- [ ] Integration test added with testdata directory and snapshot
- [ ] `hadolint-status.json` updated with new rule
- [ ] Documentation regenerated with `generate-hadolint-table.sh --update`
- [ ] All integration snapshots updated
- [ ] Code follows existing patterns in the codebase
