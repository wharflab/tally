# Code Organization for Scalability

**Research Focus:** Project structure for easy growth, navigation, and per-rule testing

Based on analysis of ruff, oxlint, golangci-lint, and hadolint.

---

## Recommended Project Structure

```text
tally/
├── cmd/tally/
│   └── main.go                    # CLI entry point
├── internal/
│   ├── config/                    # Configuration loading
│   │   ├── config.go
│   │   ├── config_test.go
│   │   └── discovery.go           # Config file discovery
│   │
│   ├── parser/                    # Dockerfile parsing (thin wrapper around buildkit)
│   │   ├── parser.go
│   │   ├── parser_test.go
│   │   └── semantic.go            # Semantic model builder
│   │
│   ├── rules/                     # **Rule implementations** (one file per rule)
│   │   ├── registry.go            # Rule registration and metadata
│   │   ├── rule.go                # Rule interface
│   │   │
│   │   ├── stage/                 # Stage management rules
│   │   │   ├── stage_name_casing.go
│   │   │   ├── stage_name_casing_test.go
│   │   │   ├── duplicate_stage.go
│   │   │   └── duplicate_stage_test.go
│   │   │
│   │   ├── base/                  # Base image rules
│   │   │   ├── pin_version.go
│   │   │   ├── pin_version_test.go
│   │   │   ├── trusted_registry.go
│   │   │   └── from_platform.go
│   │   │
│   │   ├── instruction/           # Instruction-level rules
│   │   │   ├── json_args_recommended.go
│   │   │   ├── maintainer_deprecated.go
│   │   │   └── workdir_relative.go
│   │   │
│   │   ├── security/              # Security-focused rules
│   │   │   ├── no_root_user.go
│   │   │   ├── secrets_in_env.go
│   │   │   └── exposed_secrets.go
│   │   │
│   │   ├── best_practices/        # Best practice rules
│   │   │   ├── layer_caching.go
│   │   │   ├── minimize_layers.go
│   │   │   └── apt_no_cache.go
│   │   │
│   │   └── style/                 # Style rules
│   │       ├── consistent_casing.go
│   │       ├── max_lines.go
│   │       └── empty_continuation.go
│   │
│   ├── linter/                    # Linting orchestration
│   │   ├── linter.go              # Main linter logic
│   │   ├── linter_test.go
│   │   ├── pipeline.go            # Processing pipeline
│   │   └── violation.go           # Violation struct
│   │
│   ├── inline/                    # Inline directive handling
│   │   ├── directive.go
│   │   ├── directive_test.go
│   │   └── filter.go              # Violation filtering
│   │
│   ├── reporter/                  # Output formatters
│   │   ├── reporter.go            # Reporter interface
│   │   ├── text.go
│   │   ├── text_test.go
│   │   ├── json.go
│   │   ├── sarif.go
│   │   └── github_actions.go
│   │
│   ├── integration/               # Integration tests
│   │   ├── integration_test.go
│   │   ├── __snapshots__/         # go-snaps snapshots
│   │   └── testdata/              # Test Dockerfiles
│   │       ├── basic/
│   │       │   └── Dockerfile
│   │       ├── multi_stage/
│   │       │   └── Dockerfile
│   │       └── with_errors/
│   │           └── Dockerfile
│   │
│   └── testutil/                  # Test utilities
│       ├── assert.go
│       └── fixtures.go
│
├── docs/                          # Documentation
│   ├── rules/                     # Rule documentation (auto-generated)
│   │   ├── DL3006.md
│   │   ├── DL3008.md
│   │   └── ...
│   └── architecture/              # Architecture docs (this folder)
│
├── scripts/                       # Build/development scripts
│   ├── generate_docs.go           # Generate rule docs
│   └── benchmark.sh
│
├── go.mod
├── go.sum
├── README.md
├── CLAUDE.md                      # Project guidance (already exists)
└── .tally.toml                    # Example config
```

---

## Key Organizational Principles

### 1. One File Per Rule

**Pattern observed in ruff, oxlint, hadolint:**

Each rule is a self-contained file with:

- Rule implementation
- Tests
- Example violations (in tests)

**Example: `internal/rules/base/pin_version.go`**

```go
package base

import (
    "strings"

    "github.com/tinovyatkin/tally/internal/linter"
    "github.com/tinovyatkin/tally/internal/parser"
)

// Metadata
const (
    PinVersionCode = "DL3006"
    PinVersionName = "Pin base image version"
)

var PinVersionRule = &linter.Rule{
    Code:        PinVersionCode,
    Name:        PinVersionName,
    Description: "Always tag the version of an image explicitly to ensure reproducible builds",
    Severity:    linter.SeverityWarning,
    URL:         "https://docs.tally.dev/rules/DL3006",
    Check:       checkPinVersion,
}

func checkPinVersion(ast *parser.AST, semantic *parser.SemanticModel) []linter.Violation {
    var violations []linter.Violation

    for _, stage := range semantic.Stages {
        // Check if base image has a tag
        if !hasExplicitTag(stage.BaseImage) {
            violations = append(violations, linter.Violation{
                RuleCode: PinVersionCode,
                Message:  "Always tag the version of an image explicitly",
                File:     ast.File,
                Line:     stage.LineRange.Start,
                Column:   1,
                Severity: linter.SeverityWarning,
                DocURL:   "https://docs.tally.dev/rules/DL3006",
            })
        }
    }

    return violations
}

func hasExplicitTag(image string) bool {
    // ubuntu -> false
    // ubuntu:latest -> true (even though not pinned)
    // ubuntu:22.04 -> true
    parts := strings.Split(image, ":")
    return len(parts) > 1 && parts[1] != ""
}
```

**Test file: `internal/rules/base/pin_version_test.go`**

```go
package base

import (
    "testing"

    "github.com/tinovyatkin/tally/internal/testutil"
)

func TestPinVersion(t *testing.T) {
    tests := []struct {
        name       string
        dockerfile string
        wantCount  int  // Expected number of violations
    }{
        {
            name:       "untagged image",
            dockerfile: "FROM ubuntu\n",
            wantCount:  1,
        },
        {
            name:       "tagged image",
            dockerfile: "FROM ubuntu:22.04\n",
            wantCount:  0,
        },
        {
            name:       "latest tag (allowed but not recommended)",
            dockerfile: "FROM ubuntu:latest\n",
            wantCount:  0,  // Different rule checks for :latest
        },
        {
            name: "multi-stage with mixed tagging",
            dockerfile: `
FROM ubuntu AS builder
FROM alpine:3.18
`,
            wantCount: 1,  // Only first FROM is untagged
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            violations := testutil.LintString(tt.dockerfile, PinVersionRule)
            if len(violations) != tt.wantCount {
                t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
            }
        })
    }
}
```

### 2. Rule Registry

**Central registration point:**

```go
// internal/rules/registry.go
package rules

import "github.com/tinovyatkin/tally/internal/linter"

// Import all rule packages
import (
    _ "github.com/tinovyatkin/tally/internal/rules/base"
    _ "github.com/tinovyatkin/tally/internal/rules/security"
    _ "github.com/tinovyatkin/tally/internal/rules/stage"
    // ...
)

// AllRules contains all registered rules
var AllRules []*linter.Rule

// Register is called by rule packages during init()
func Register(rule *linter.Rule) {
    AllRules = append(AllRules, rule)
}

// GetRule returns a rule by code
func GetRule(code string) *linter.Rule {
    for _, rule := range AllRules {
        if rule.Code == code {
            return rule
        }
    }
    return nil
}

// GetRulesByCategory returns rules matching a category
func GetRulesByCategory(category string) []*linter.Rule {
    var rules []*linter.Rule
    for _, rule := range AllRules {
        if rule.Category == category {
            rules = append(rules, rule)
        }
    }
    return rules
}
```

**Rule packages register during init():**

```go
// internal/rules/base/pin_version.go
package base

import "github.com/tinovyatkin/tally/internal/rules"

func init() {
    rules.Register(PinVersionRule)
}
```

### 3. Rule Interface

**Clean, minimal interface:**

```go
// internal/linter/rule.go
package linter

import "github.com/tinovyatkin/tally/internal/parser"

// Rule represents a linting rule
type Rule struct {
    // Metadata
    Code        string
    Name        string
    Description string
    Category    string   // "security", "best-practices", "style", etc.
    Severity    Severity
    URL         string   // Documentation URL

    // Configuration
    Enabled     bool
    Experimental bool

    // Implementation
    Check RuleFunc
}

// RuleFunc is the signature for rule implementations
type RuleFunc func(ast *parser.AST, semantic *parser.SemanticModel) []Violation

// Violation represents a single rule violation
type Violation struct {
    RuleCode string
    Message  string
    File     string
    Line     int
    Column   int
    Severity Severity
    DocURL   string

    // Optional fields
    Detail       string
    SourceCode   string
    SuggestedFix *Fix
}

type Severity int

const (
    SeverityError Severity = iota
    SeverityWarning
    SeverityInfo
    SeverityStyle
)
```

---

## Category-Based Organization

### Rule Categories

Organize rules into logical categories:

```text
rules/
├── stage/           # DL4xxx - Stage management
├── base/            # DL3xxx - Base image rules
├── instruction/     # DL30xx - Instruction-level
├── security/        # DL3xxx - Security
├── best_practices/  # DL3xxx - Best practices
├── style/           # DL3xxx - Style/formatting
└── meta/            # DL1xxx - Meta rules
```

**Benefits:**

- Easy navigation
- Logical grouping
- Clear ownership
- Simplifies imports

---

## Testing Strategy

### Unit Tests (Per-Rule)

**Co-located with rule implementation:**

```go
// internal/rules/base/pin_version_test.go
func TestPinVersion(t *testing.T) {
    // Table-driven tests for this specific rule
}
```

### Integration Tests

**End-to-end validation:**

```go
// internal/integration/integration_test.go
func TestCheck(t *testing.T) {
    fixtures, err := os.ReadDir("testdata")
    require.NoError(t, err)

    for _, fixture := range fixtures {
        if !fixture.IsDir() {
            continue
        }

        name := fixture.Name()
        t.Run(name, func(t *testing.T) {
            dockerfilePath := filepath.Join("testdata", name, "Dockerfile")

            // Run linter
            violations, err := lint.LintFile(dockerfilePath, config)
            require.NoError(t, err)

            // Snapshot test
            snaps.MatchJSON(t, violations)
        })
    }
}
```

### Test Utilities

```go
// internal/testutil/lint.go
package testutil

import (
    "github.com/tinovyatkin/tally/internal/linter"
    "github.com/tinovyatkin/tally/internal/parser"
)

// LintString lints a Dockerfile string with given rules
func LintString(dockerfile string, rules ...*linter.Rule) []linter.Violation {
    ast, _ := parser.ParseString(dockerfile)
    semantic := parser.BuildSemanticModel(ast)

    var violations []linter.Violation
    for _, rule := range rules {
        v := rule.Check(ast, semantic)
        violations = append(violations, v...)
    }

    return violations
}

// LintFile lints a Dockerfile file
func LintFile(path string, rules ...*linter.Rule) []linter.Violation {
    content, _ := os.ReadFile(path)
    return LintString(string(content), rules...)
}
```

---

## Documentation Generation

### Auto-Generate Rule Docs

**Script: `scripts/generate_docs.go`**

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"
    "text/template"

    "github.com/tinovyatkin/tally/internal/rules"
)

const docTemplate = `# {{ .Code }}: {{ .Name }}

**Category:** {{ .Category }}
**Severity:** {{ .Severity }}
**Since:** v1.0.0

## Description

{{ .Description }}

## Examples

### Non-Compliant

` + "```dockerfile" + `
{{ .BadExample }}
` + "```" + `

### Compliant

` + "```dockerfile" + `
{{ .GoodExample }}
` + "```" + `

## Rationale

{{ .Rationale }}

## Configuration

` + "```toml" + `
[rules.{{ .ConfigKey }}]
enabled = {{ .Enabled }}
` + "```" + `

## References

- [Dockerfile best practices](https://docs.docker.com/develop/develop-images/dockerfile_best-practices/)
`

func main() {
    tmpl := template.Must(template.New("rule").Parse(docTemplate))

    for _, rule := range rules.AllRules {
        outputPath := filepath.Join("docs", "rules", rule.Code+".md")
        f, _ := os.Create(outputPath)
        defer f.Close()

        data := struct {
            *linter.Rule
            BadExample  string
            GoodExample string
            Rationale   string
            ConfigKey   string
        }{
            Rule:        rule,
            BadExample:  getExampleBad(rule.Code),
            GoodExample: getExampleGood(rule.Code),
            Rationale:   getRationale(rule.Code),
            ConfigKey:   ruleCodeToConfigKey(rule.Code),
        }

        tmpl.Execute(f, data)
    }
}
```

---

## Build and Development

### Makefile

```makefile
.PHONY: build test lint docs

# Build binary
build:
	go build -o tally ./cmd/tally

# Run all tests
test:
	go test ./...

# Run integration tests with coverage
test-integration:
	go test -cover ./internal/integration/...

# Update integration test snapshots
test-update-snaps:
	UPDATE_SNAPS=true go test ./internal/integration/...

# Run linter on tally itself
lint:
	golangci-lint run ./...

# Generate rule documentation
docs:
	go run scripts/generate_docs.go

# Run benchmarks
bench:
	go test -bench=. ./internal/linter/

# Install locally
install:
	go install ./cmd/tally

# Clean build artifacts
clean:
	rm -f tally
	rm -rf coverage/
```

---

## Scaling Patterns

### Adding a New Rule

**Step 1: Create rule file**

```bash
# internal/rules/security/no_root_user.go
touch internal/rules/security/no_root_user.go
```

**Step 2: Implement rule**

```go
package security

import "github.com/tinovyatkin/tally/internal/rules"

const NoRootUserCode = "DL3002"

var NoRootUserRule = &linter.Rule{
    Code:        NoRootUserCode,
    Name:        "Avoid running as root",
    Description: "Use USER instruction to switch to non-root user",
    Category:    "security",
    Severity:    linter.SeverityWarning,
    URL:         "https://docs.tally.dev/rules/DL3002",
    Check:       checkNoRootUser,
}

func init() {
    rules.Register(NoRootUserRule)
}

func checkNoRootUser(ast *parser.AST, semantic *parser.SemanticModel) []linter.Violation {
    // Implementation
}
```

**Step 3: Add tests**

```go
// internal/rules/security/no_root_user_test.go
func TestNoRootUser(t *testing.T) {
    // Tests
}
```

**Step 4: Generate docs**

```bash
make docs
```

**That's it!** No registration needed, rule is automatically included.

### Rule Discoverability

**List all rules:**

```bash
tally rules list
```

**Show rule details:**

```bash
tally rules show DL3006
```

**Implementation:**

```go
// cmd/tally/cmd/rules.go
func listRules(ctx context.Context, cmd *cli.Command) error {
    for _, rule := range rules.AllRules {
        fmt.Printf("%s: %s [%s]\n", rule.Code, rule.Name, rule.Category)
    }
    return nil
}

func showRule(ctx context.Context, cmd *cli.Command) error {
    code := cmd.Args().Get(0)
    rule := rules.GetRule(code)
    if rule == nil {
        return fmt.Errorf("rule not found: %s", code)
    }

    fmt.Printf("Code: %s\n", rule.Code)
    fmt.Printf("Name: %s\n", rule.Name)
    fmt.Printf("Description: %s\n", rule.Description)
    fmt.Printf("Category: %s\n", rule.Category)
    fmt.Printf("Severity: %s\n", rule.Severity)
    fmt.Printf("URL: %s\n", rule.URL)

    return nil
}
```

---

## Key Takeaways

1. ✅ **One file per rule** - Easy to find and maintain
2. ✅ **Category-based organization** - Logical grouping
3. ✅ **Automatic registration** - init() functions
4. ✅ **Co-located tests** - Tests next to implementation
5. ✅ **Integration tests** - End-to-end validation with snapshots
6. ✅ **Generated documentation** - Docs from code
7. ✅ **Clear interfaces** - Minimal, focused APIs

---

## Migration Path

From current structure to recommended:

1. **Phase 1: Extract rules** (Week 1)
   - Move `max_lines` to `internal/rules/style/max_lines.go`
   - Create rule registry
   - Update tests

2. **Phase 2: Add infrastructure** (Week 1-2)
   - Create semantic model builder
   - Add inline directive support
   - Add reporter infrastructure

3. **Phase 3: Implement rules** (Ongoing)
   - Add 5-10 rules per sprint
   - Prioritize based on value and usage
   - Test thoroughly

---

## References

- Ruff rules: `crates/ruff_linter/src/rules/`
- Oxlint rules: `crates/oxc_linter/src/rules/`
- golangci-lint: `pkg/golinters/`
- Hadolint: `src/Hadolint/Rule/`
