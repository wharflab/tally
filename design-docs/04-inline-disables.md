# Inline Disables and Configuration

**Research Focus:** How modern linters implement inline suppression directives

Based on analysis of ruff, oxlint, golangci-lint, hadolint, and Docker buildx.

---

## Overview

Inline disables allow developers to suppress specific linter warnings directly in source code. This is essential for:

- **False positives** - When the linter is wrong
- **Intentional violations** - When breaking the rule is the right choice
- **Gradual adoption** - Suppress existing violations while fixing incrementally
- **Third-party code** - Disable checks for code you don't control

---

## Common Patterns

### 1. Line-Level Suppression

Suppress warnings on the current or next line:

**ESLint/oxlint:**

```javascript
// eslint-disable-next-line no-console
console.log('debug');

alert('test');  // eslint-disable-line no-alert
```

**Python/ruff:**

```python
# noqa: E501
very_long_line_that_exceeds_the_line_length_limit_but_thats_okay()

bad_function()  # noqa: F401
```

**Go/golangci-lint:**

```go
//nolint:errcheck
doSomething()

doSomethingElse() //nolint:golint,unused
```

**Hadolint:**

```dockerfile
# hadolint ignore=DL3006,DL3008
FROM ubuntu
```

### 2. Block-Level Suppression

Disable rules for a range of lines:

**ESLint/oxlint:**

```javascript
/* eslint-disable no-console */
console.log('debug 1');
console.log('debug 2');
/* eslint-enable no-console */
```

**Ruff (preview feature):**

```python
# ruff: noqa: START F401
import unused1
import unused2
# ruff: noqa: END F401
```

**golangci-lint:**

```go
//nolint:all
func legacyCode() {
    // All checks disabled
}
```

### 3. File-Level Suppression

Disable rules for entire file:

**ESLint/oxlint:**

```javascript
/* eslint-disable */
// Entire file ignored
```

**Python/ruff:**

```python
# ruff: noqa
# or
# flake8: noqa
```

**Hadolint:**

```dockerfile
# hadolint global ignore=DL3003,DL3008
# Applies to entire file
```

### 4. All Rules vs Specific Rules

**All rules:**

```dockerfile
# Suppress all rules
# tally ignore=all
```

**Specific rules:**

```dockerfile
# Suppress only DL3006 and DL3008
# tally ignore=DL3006,DL3008
```

---

## Implementation Approaches

### Approach 1: Comment Parsing (Hadolint, ruff, oxlint)

**Parse inline directives during initial pass:**

```go
type InlineDirective struct {
    Type       DirectiveType  // Ignore, Global, etc.
    Rules      []string       // Specific rules, or ["all"]
    Line       int            // Line where directive appears
    AppliesTo  LineRange      // Which lines are affected
}

type DirectiveType int

const (
    DirectiveIgnoreNextLine DirectiveType = iota
    DirectiveIgnoreLine
    DirectiveGlobalIgnore
    DirectiveRangeStart
    DirectiveRangeEnd
)

func ParseInlineDirectives(ast *parser.Result) []InlineDirective {
    var directives []InlineDirective

    for _, node := range ast.AST.Children {
        // Check comments preceding this node
        for _, comment := range node.PrevComment {
            if directive := parseDirective(comment, node.StartLine); directive != nil {
                directives = append(directives, *directive)
            }
        }
    }

    return directives
}

func parseDirective(comment string, nextLine int) *InlineDirective {
    // Match patterns:
    // # tally ignore=DL3006,DL3008
    // # tally global ignore=DL3003
    // # tally ignore=all

    if strings.Contains(comment, "tally ignore=") {
        rules := extractRules(comment)
        isGlobal := strings.Contains(comment, "global")

        return &InlineDirective{
            Type: ternary(isGlobal, DirectiveGlobalIgnore, DirectiveIgnoreNextLine),
            Rules: rules,
            Line: nextLine - 1,  // Comment line
            AppliesTo: LineRange{
                Start: nextLine,
                End: ternary(isGlobal, math.MaxInt, nextLine),
            },
        }
    }

    return nil
}

func extractRules(comment string) []string {
    // Extract comma-separated rule codes
    // "# tally ignore=DL3006,DL3008" -> ["DL3006", "DL3008"]
    // "# tally ignore=all" -> ["all"]

    pattern := regexp.MustCompile(`ignore=([A-Za-z0-9,]+)`)
    matches := pattern.FindStringSubmatch(comment)
    if len(matches) < 2 {
        return []string{"all"}
    }

    return strings.Split(matches[1], ",")
}
```

### Approach 2: Interval Tree (oxlint)

**Use efficient data structure for range queries:**

```go
import "github.com/brentp/go-lapper"

type DisableDirectives struct {
    // Efficient interval overlap queries
    intervals *lapper.Lapper[uint32, DisabledRule]

    // Track unused directives
    usedDirectives map[int]bool
}

type DisabledRule struct {
    RuleName string  // or "all"
    Span     Span
}

type Span struct {
    Start int  // Line number
    End   int
}

func BuildDisableDirectives(directives []InlineDirective) *DisableDirectives {
    var intervals []lapper.Interval[uint32, DisabledRule]

    for _, d := range directives {
        intervals = append(intervals, lapper.Interval[uint32, DisabledRule]{
            Start: uint32(d.AppliesTo.Start),
            Stop:  uint32(d.AppliesTo.End),
            Val: DisabledRule{
                RuleName: joinRules(d.Rules),
                Span: Span{Start: d.Line, End: d.Line},
            },
        })
    }

    return &DisableDirectives{
        intervals: lapper.New(intervals),
        usedDirectives: make(map[int]bool),
    }
}

// O(log n) query for whether rule is disabled at line
func (d *DisableDirectives) IsDisabled(ruleName string, line int) bool {
    // Query intervals that overlap this line
    for _, interval := range d.intervals.Find(uint32(line), uint32(line+1)) {
        rule := interval.Val
        if rule.RuleName == "all" || rule.RuleName == ruleName {
            d.usedDirectives[rule.Span.Start] = true
            return true
        }
    }
    return false
}

// Check for unused disable directives
func (d *DisableDirectives) GetUnusedDirectives() []int {
    var unused []int
    for line, used := range d.usedDirectives {
        if !used {
            unused = append(unused, line)
        }
    }
    return unused
}
```

### Approach 3: Post-Filtering (golangci-lint)

**Filter violations after rule execution:**

```go
type ViolationFilter struct {
    directives []InlineDirective
}

func (f *ViolationFilter) Filter(violations []Violation) []Violation {
    var filtered []Violation

    for _, v := range violations {
        if !f.shouldSuppress(v) {
            filtered = append(filtered, v)
        }
    }

    return filtered
}

func (f *ViolationFilter) shouldSuppress(v Violation) bool {
    for _, d := range f.directives {
        if v.Line >= d.AppliesTo.Start && v.Line <= d.AppliesTo.End {
            // Check if this rule is disabled
            if contains(d.Rules, "all") || contains(d.Rules, v.RuleCode) {
                return true
            }
        }
    }
    return false
}
```

---

## Recommended Implementation for Tally

### Syntax Design

**Prioritize simplicity and familiarity:**

```dockerfile
# Single-line ignore (next line)
# tally ignore=DL3006
FROM ubuntu

# Inline ignore (same line - not recommended for Dockerfiles)
FROM ubuntu  # tally ignore=DL3006

# Multiple rules
# tally ignore=DL3006,DL3008,DL3013
RUN apt-get update && apt-get install -y curl

# Ignore all rules
# tally ignore=all
COPY . /app

# File-level ignore
# tally global ignore=DL3003

# Alternative: use buildx-style syntax
# check=skip=DL3006,DL3008
```

**Rationale:**

- Consistent with Hadolint (`hadolint ignore=`)
- Similar to ESLint/ruff patterns
- Clear intent with `ignore=` keyword
- Supports Docker buildx `check=` for compatibility

### Implementation Strategy

**Three-phase approach:**

```go
// Phase 1: Parse directives during AST traversal
func ParseDirectives(ast *parser.Result) []InlineDirective {
    var directives []InlineDirective

    for _, node := range ast.AST.Children {
        for _, comment := range node.PrevComment {
            if d := parseComment(comment, node.StartLine); d != nil {
                directives = append(directives, *d)
            }
        }
    }

    return directives
}

// Phase 2: Run rules (collect all violations)
violations := RunAllRules(ast, semantic, config)

// Phase 3: Filter violations based on directives
filtered := FilterViolations(violations, directives)
```

**Why this approach?**

- Simple to implement and understand
- Works with existing AST parser
- No complex coordination between rules and directives
- Easy to add "unused directive" warnings later

### Full Implementation

```go
package inline

import (
    "regexp"
    "strings"
)

// Directive types
const (
    IgnoreNextLine = iota
    IgnoreGlobal
)

// InlineDirective represents a parsed suppression comment
type InlineDirective struct {
    Type      int
    Rules     []string  // Rule codes to suppress
    LineNo    int       // Line where directive appears
    AppliesTo LineRange
    Used      bool      // Track if directive was actually used
}

type LineRange struct {
    Start int
    End   int
}

// Pattern matchers
var (
    // # tally ignore=DL3006,DL3008
    tallyIgnorePattern = regexp.MustCompile(`#\s*tally\s+(global\s+)?ignore=([A-Za-z0-9,]+)`)

    // # check=skip=DL3006,DL3008 (buildx compatibility)
    checkSkipPattern = regexp.MustCompile(`#\s*check=skip=([A-Za-z0-9,]+)`)
)

// Parse extracts inline directives from AST comments
func Parse(ast *parser.Result) []InlineDirective {
    var directives []InlineDirective

    for _, node := range ast.AST.Children {
        for i, comment := range node.PrevComment {
            commentLine := node.StartLine - len(node.PrevComment) + i

            if d := parseComment(comment, commentLine, node.StartLine); d != nil {
                directives = append(directives, *d)
            }
        }
    }

    return directives
}

func parseComment(comment string, commentLine, nextLine int) *InlineDirective {
    // Try tally syntax first
    if matches := tallyIgnorePattern.FindStringSubmatch(comment); matches != nil {
        isGlobal := strings.TrimSpace(matches[1]) == "global"
        rules := strings.Split(matches[2], ",")

        endLine := nextLine
        if isGlobal {
            endLine = 1<<31 - 1  // Max int
        }

        return &InlineDirective{
            Type:   ternary(isGlobal, IgnoreGlobal, IgnoreNextLine),
            Rules:  rules,
            LineNo: commentLine,
            AppliesTo: LineRange{
                Start: nextLine,
                End:   endLine,
            },
        }
    }

    // Try buildx check= syntax
    if matches := checkSkipPattern.FindStringSubmatch(comment); matches != nil {
        rules := strings.Split(matches[1], ",")
        return &InlineDirective{
            Type:   IgnoreNextLine,
            Rules:  rules,
            LineNo: commentLine,
            AppliesTo: LineRange{
                Start: nextLine,
                End:   nextLine,
            },
        }
    }

    return nil
}

// Filter removes violations that are suppressed by inline directives
func Filter(violations []Violation, directives []InlineDirective) []Violation {
    var filtered []Violation

    for _, v := range violations {
        if !shouldSuppress(v, directives) {
            filtered = append(filtered, v)
        }
    }

    return filtered
}

func shouldSuppress(v Violation, directives []InlineDirective) bool {
    for i := range directives {
        d := &directives[i]

        // Check if violation is in directive's range
        if v.Line >= d.AppliesTo.Start && v.Line <= d.AppliesTo.End {
            // Check if this rule is suppressed
            if contains(d.Rules, "all") || contains(d.Rules, v.RuleCode) {
                d.Used = true
                return true
            }
        }
    }
    return false
}

// GetUnused returns directives that didn't suppress any violations
func GetUnused(directives []InlineDirective) []InlineDirective {
    var unused []InlineDirective
    for _, d := range directives {
        if !d.Used {
            unused = append(unused, d)
        }
    }
    return unused
}

func contains(slice []string, item string) bool {
    for _, s := range slice {
        if s == item {
            return true
        }
    }
    return false
}

func ternary(cond bool, a, b int) int {
    if cond {
        return a
    }
    return b
}
```

### Usage Example

```go
package main

import (
    "github.com/tinovyatkin/tally/internal/inline"
    "github.com/tinovyatkin/tally/internal/lint"
)

func lintFile(path string, rules []lint.Rule) ([]lint.Violation, error) {
    // Parse Dockerfile
    ast, err := parse(path)
    if err != nil {
        return nil, err
    }

    // Extract inline directives
    directives := inline.Parse(ast)

    // Run all rules
    violations := lint.RunRules(ast, rules)

    // Filter based on directives
    filtered := inline.Filter(violations, directives)

    // Optionally warn about unused directives
    for _, unused := range inline.GetUnused(directives) {
        filtered = append(filtered, lint.Violation{
            RuleCode: "unused-disable-directive",
            Message:  fmt.Sprintf("Unused disable directive at line %d", unused.LineNo),
            Line:     unused.LineNo,
            Severity: lint.SeverityWarning,
        })
    }

    return filtered, nil
}
```

---

## Advanced Features

### 1. Unused Directive Detection

**Why it matters:**

- Prevents directive drift (code changes, directive stays)
- Encourages cleanup of unnecessary suppressions
- Helps maintain code quality

**Implementation:**

```go
// Track usage during filtering
func Filter(violations []Violation, directives []InlineDirective) ([]Violation, []UnusedDirective) {
    var filtered []Violation
    var unused []UnusedDirective

    // ... filter violations, mark directives as used ...

    // Check for unused directives
    for _, d := range directives {
        if !d.Used {
            unused = append(unused, UnusedDirective{
                Line:  d.LineNo,
                Rules: d.Rules,
            })
        }
    }

    return filtered, unused
}
```

**Output:**

```text
warning: Unused disable directive at line 5 (tally:unused-directive)
  5 | # tally ignore=DL3006
    |   ^ This directive doesn't suppress any violations
```

### 2. Directive Validation

**Check for invalid rule codes:**

```go
func ValidateDirectives(directives []InlineDirective, validRules []string) []ValidationError {
    var errors []ValidationError

    for _, d := range directives {
        for _, rule := range d.Rules {
            if rule == "all" {
                continue
            }

            if !isValidRule(rule, validRules) {
                errors = append(errors, ValidationError{
                    Line:    d.LineNo,
                    Message: fmt.Sprintf("Unknown rule code: %s", rule),
                })
            }
        }
    }

    return errors
}
```

**Output:**

```text
error: Unknown rule code 'DL999' at line 3
  3 | # tally ignore=DL999
    |                 ^^^^
```

### 3. Range Directives (Future)

**For suppressing multiple lines:**

```dockerfile
# tally-disable DL3008
RUN apt-get update && \
    apt-get install -y \
        package1 \
        package2 \
        package3
# tally-enable DL3008
```

**Implementation:**

```go
type RangeDirective struct {
    RuleCode string
    Start    int
    End      int
}

func parseRangeDirectives(comments []string) []RangeDirective {
    var directives []RangeDirective
    var active map[string]int  // rule -> start line

    for i, comment := range comments {
        if matches := disablePattern.FindStringSubmatch(comment); matches != nil {
            rule := matches[1]
            active[rule] = i
        } else if matches := enablePattern.FindStringSubmatch(comment); matches != nil {
            rule := matches[1]
            if start, ok := active[rule]; ok {
                directives = append(directives, RangeDirective{
                    RuleCode: rule,
                    Start:    start,
                    End:      i,
                })
                delete(active, rule)
            }
        }
    }

    return directives
}
```

---

## Configuration Integration

### File-level Configuration

**In `.tally.toml`:**

```toml
[rules]
# Globally disable rules
disable = ["DL3003", "DL3008"]

# Enable experimental rules
enable-experimental = ["EX1001"]
```

### Precedence Order

Highest to lowest priority:

1. **Inline directives** (`# tally ignore=`)
2. **CLI flags** (`--disable=DL3006`)
3. **Config file** (`.tally.toml`)
4. **Default enabled rules**

### Implementation

```go
func shouldRunRule(rule Rule, config *Config, inlineDisabled map[string]bool) bool {
    // 1. Check inline directives (highest priority)
    if inlineDisabled[rule.Code] {
        return false
    }

    // 2. Check CLI flags
    if config.CLIDisabledRules[rule.Code] {
        return false
    }

    // 3. Check config file
    if config.FileDisabledRules[rule.Code] {
        return false
    }

    // 4. Check if rule is enabled by default (DefaultSeverity != off)
    return rule.DefaultSeverity != SeverityOff
}
```

---

## Testing Strategy

### Test Cases

```go
func TestInlineDirectives(t *testing.T) {
    tests := []struct {
        name       string
        dockerfile string
        want       []Violation
    }{
        {
            name: "suppress single rule next line",
            dockerfile: `
# tally ignore=DL3006
FROM ubuntu
`,
            want: []Violation{},
        },
        {
            name: "suppress multiple rules",
            dockerfile: `
# tally ignore=DL3006,DL3008
FROM ubuntu
RUN apt-get update
`,
            want: []Violation{},
        },
        {
            name: "global ignore",
            dockerfile: `
# tally global ignore=DL3008
FROM ubuntu:latest
RUN apt-get install curl
`,
            want: []Violation{},
        },
        {
            name: "unused directive",
            dockerfile: `
# tally ignore=DL9999
FROM ubuntu:22.04
`,
            want: []Violation{
                {RuleCode: "unused-directive", Line: 2},
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := lintString(tt.dockerfile)
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

## Key Takeaways

1. ✅ **Use post-filtering approach** - Simplest to implement and understand
2. ✅ **Support both `tally` and `check=` syntax** - Compatibility with buildx
3. ✅ **Track directive usage** - Detect unused suppressions
4. ✅ **Validate rule codes** - Catch typos in directives
5. ✅ **Clear precedence order** - Inline > CLI > config > default
6. 🔄 **Range directives are optional** - Can add later if needed

---

## Implementation Checklist

### Phase 1: Basic Suppression

- [ ] Parse `# tally ignore=RULE` syntax
- [ ] Parse `# tally global ignore=RULE` syntax
- [ ] Support `ignore=all` for all rules
- [ ] Post-filtering of violations
- [ ] Multiple rules in single directive

### Phase 2: Validation

- [ ] Validate rule codes in directives
- [ ] Detect unused directives
- [ ] Warning for invalid syntax
- [ ] Config file integration

### Phase 3: Advanced (Optional)

- [ ] Range directives (`tally-disable` ... `tally-enable`)
- [ ] Buildx `check=` compatibility
- [ ] Per-file configuration
- [ ] Auto-fix to add/remove directives

---

## References

- Ruff noqa: `crates/ruff_linter/src/checkers/noqa.rs`
- Oxlint directives: `crates/oxc_linter/src/disable_directives.rs`
- golangci-lint nolint: `pkg/result/processors/nolint_filter.go`
- Hadolint ignore: `src/Hadolint/Rule.hs`
- BuildKit check: `moby/buildkit/frontend/dockerfile/linter/`
