# Tally BuildKit Violation Pattern - Comprehensive Overview

## Executive Summary

Tally emits BuildKit violations through **TWO mechanisms**:

1. **Parse-Time Violations**: Captured from BuildKit's linter during Dockerfile parsing
2. **Custom Tally Rules**: Static linting rules implemented by tally itself

Both are converted to violations with `"buildkit/"` prefix and can receive auto-fix enrichment.

---

## Architecture Flow

### Phase 1: Violation Collection (in `internal/linter/linter.go`)

```go
// 1. Parse Dockerfile and collect BuildKit warnings
parseResult, err := dockerfile.Parse(bytes.NewReader(content), cfg)

// 2. Run all registered tally rules (including custom buildkit rules)
for _, rule := range rules.All() {
    violations = append(violations, rule.Check(ruleInput)...)
}

// 3. Convert BuildKit warnings to violations
for _, w := range parseResult.Warnings {
    violations = append(violations, rules.NewViolationFromBuildKitWarning(...))
}

// 4. Enrich BuildKit violations with auto-fixes
fixes.EnrichBuildKitFixes(violations, sem, content)
```

### Phase 2: BuildKit Warning Capture (in `internal/dockerfile/parser.go`)

During parsing, BuildKit calls a linter callback:

```go
warnFunc := func(rulename, description, url, fmtmsg string, location []parser.Range) {
    warnings = append(warnings, LintWarning{
        RuleName: rulename,
        Description: description,
        URL: url,
        Message: fmtmsg,
        Location: location,
    })
}
lint := linter.New(lintCfg)
stages, metaArgs, err := instructions.Parse(astForInstructions, lint)
```

**Important**: Only parse-time rules are captured. BuildKit also checks during LLB conversion (which tally doesn't run).

**CapturedRuleNames** (from `registry.go`):

```go
var CapturedRuleNames = []string{
    "StageNameCasing",
    "FromAsCasing",
    "MaintainerDeprecated",
    "InvalidDefinitionDescription",
    "NoEmptyContinuation",
}
```

---

## Two Types of Violations with "buildkit/" Code

### Type 1: Parse-Time BuildKit Warnings

**Sources**: Captured from BuildKit's linter during `instructions.Parse()`

Example: `FromAsCasing`, `StageNameCasing`

In `violation.go`:

```go
func NewViolationFromBuildKitWarning(
    file string,
    ruleName string,
    description string,
    url string,
    message string,
    location []parser.Range,
) Violation {
    return Violation{
        Location: NewLocationFromRange(file, location[0]),
        RuleCode: BuildKitRulePrefix + ruleName,  // "buildkit/FromAsCasing"
        Message: message,
        Detail: description,
        Severity: SeverityWarning,
        DocURL: url,
    }
}
```

### Type 2: Custom Tally Rules Implementing BuildKit Checks

**Sources**: Tally's own implementations (registered via `init()`)

Examples: `ConsistentInstructionCasing`, `DuplicateStageName`, `JSONArgsRecommended`

Pattern in `internal/rules/buildkit/*.go`:

```go
type ConsistentInstructionCasingRule struct{}

func (r *ConsistentInstructionCasingRule) Metadata() rules.RuleMetadata {
    return rules.RuleMetadata{
        Code: rules.BuildKitRulePrefix + "ConsistentInstructionCasing",
        Name: "Consistent Instruction Casing",
        Description: linter.RuleConsistentInstructionCasing.Description,
        DocURL: linter.RuleConsistentInstructionCasing.URL,
        DefaultSeverity: rules.SeverityWarning,
        Category: "style",
        IsExperimental: false,
    }
}

func (r *ConsistentInstructionCasingRule) Check(input rules.LintInput) []rules.Violation {
    // Custom logic analyzing input.Stages and input.MetaArgs
    // Returns violations with RuleCode = "buildkit/ConsistentInstructionCasing"
}

func init() {
    rules.Register(NewConsistentInstructionCasingRule())
}
```

---

## Auto-Fix Enrichment Pipeline

**File**: `internal/rules/buildkit/fixes/enricher.go`

After all violations collected, enricher modifies them in-place:

```go
func EnrichBuildKitFixes(violations []rules.Violation, sem *semantic.Model, source []byte) {
    for i := range violations {
        v := &violations[i]

        // Only process violations with "buildkit/" code
        if !strings.HasPrefix(v.RuleCode, rules.BuildKitRulePrefix) {
            continue
        }

        // Skip if already has fix
        if v.SuggestedFix != nil {
            continue
        }

        // Extract rule name: "buildkit/StageNameCasing" → "StageNameCasing"
        ruleName := strings.TrimPrefix(v.RuleCode, rules.BuildKitRulePrefix)

        // Route to appropriate enricher function
        switch ruleName {
        case "StageNameCasing":
            enrichStageNameCasingFix(v, sem, source)
        case "FromAsCasing":
            enrichFromAsCasingFix(v, source)
        case "ConsistentInstructionCasing":
            enrichConsistentInstructionCasingFix(v, source)
        case "JSONArgsRecommended":
            enrichJSONArgsRecommendedFix(v, source)
        // ... etc
        }
    }
}
```

**Fixable Rules** (registered in enricher):

```go
var fixableRuleNames = []string{
    "StageNameCasing",
    "FromAsCasing",
    "NoEmptyContinuation",
    "MaintainerDeprecated",
    "ConsistentInstructionCasing",
    "JSONArgsRecommended",
    "InvalidDefinitionDescription",
}
```

---

## Example: How ConsistentInstructionCasing Works

### 1. Rule Implementation

File: `internal/rules/buildkit/consistent_instruction_casing.go`

```go
func (r *ConsistentInstructionCasingRule) Check(input rules.LintInput) []rules.Violation {
    // First pass: count upper vs lower case instructions
    var lowerCount, upperCount int

    for _, arg := range input.MetaArgs {
        if strings.ToLower(arg.Name()) == arg.Name() {
            lowerCount++
        } else {
            upperCount++
        }
    }

    // Determine majority
    isMajorityLower := lowerCount > upperCount

    // Second pass: report violations
    var violations []rules.Violation
    for _, arg := range input.MetaArgs {
        if v, ok := r.checkCasing(arg.Name(), isMajorityLower, arg.Location(), input.File); ok {
            violations = append(violations, v)
        }
    }

    return violations
}

func (r *ConsistentInstructionCasingRule) checkCasing(...) (rules.Violation, bool) {
    return rules.NewViolation(
        loc,
        r.Metadata().Code,  // "buildkit/ConsistentInstructionCasing"
        msg,
        r.Metadata().DefaultSeverity,
    ).WithDocURL(r.Metadata().DocURL), true
}

func init() {
    rules.Register(NewConsistentInstructionCasingRule())
}
```

### 2. Auto-Fix Enrichment

File: `internal/rules/buildkit/fixes/consistent_instruction_casing.go`

```go
func enrichConsistentInstructionCasingFix(v *rules.Violation, source []byte) {
    // Extract instruction name and expected casing from message
    // Message: "Command 'run' should match the case of the command majority (uppercase)"
    matches := casingMessageRegex.FindStringSubmatch(v.Message)
    instructionName := matches[1]  // "run"
    expectedCasing := matches[2]   // "uppercase"

    // Get the line
    lineIdx := v.Location.Start.Line - 1
    line := getLine(source, lineIdx)

    // Parse instruction to find keyword
    it := ParseInstruction(line)
    var keywordToken *Token
    for _, tok := range it.tokens {
        if tok.Type == TokenKeyword && strings.EqualFold(tok.Value, instructionName) {
            keywordToken = &tok
            break
        }
    }

    // Determine new text
    var newText string
    if expectedCasing == "lowercase" {
        newText = strings.ToLower(instructionName)
    } else {
        newText = strings.ToUpper(instructionName)
    }

    // Add fix to violation (modifies in-place)
    v.SuggestedFix = &rules.SuggestedFix{
        Description: fmt.Sprintf("Change '%s' to '%s' to match majority casing", keywordToken.Value, newText),
        Safety: rules.FixSafe,
        Edits: []rules.TextEdit{{
            Location: createEditLocation(v.Location.File, v.Location.Start.Line, keywordToken.Start, keywordToken.End),
            NewText: newText,
        }},
        IsPreferred: true,
    }
}
```

### 3. Test Verification

File: `internal/rules/buildkit/fixes/fixes_test.go`

```go
func TestConsistentInstructionCasingFix(t *testing.T) {
    source := []byte("run echo hello")
    v := rules.Violation{
        Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, len(source)),
        RuleCode: rules.BuildKitRulePrefix + "ConsistentInstructionCasing",
        Message: "Command 'run' should match the case of the command majority (uppercase)",
    }

    enrichConsistentInstructionCasingFix(&v, source)

    require.NotNil(t, v.SuggestedFix)
    assert.Equal(t, "RUN", v.SuggestedFix.Edits[0].NewText)
    assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
}
```

---

## Registry Pattern

File: `internal/rules/buildkit/registry.go`

Maintains metadata for ALL BuildKit rules (both captured and non-captured):

```go
var allRules = []ruleEntry{
    {&linter.RuleStageNameCasing, rules.SeverityWarning, "style"},
    {&linter.RuleFromAsCasing, rules.SeverityWarning, "style"},
    {&linter.RuleConsistentInstructionCasing, rules.SeverityWarning, "style"},
    {&linter.RuleLegacyKeyValueFormat, rules.SeverityWarning, "style"},
    // ... etc
}

func GetMetadata(ruleName string) *rules.RuleMetadata {
    info := Get(ruleName)
    if info == nil {
        return nil
    }
    return &rules.RuleMetadata{
        Code: rules.BuildKitRulePrefix + ruleName,
        Name: ruleName,
        Description: info.Description,
        DocURL: info.DocURL,
        DefaultSeverity: info.DefaultSeverity,
        Category: info.Category,
        IsExperimental: info.Experimental,
    }
}
```

---

## Custom Rules That Aren't in BuildKit's Parse Phase

### Why They Exist

BuildKit runs checks at two phases:

1. **Parse Phase**: During AST/instruction parsing (captured by tally)
2. **LLB Conversion Phase**: During actual build conversion (tally doesn't run)

Tally implements rules that only exist in phase 2:

- `ConsistentInstructionCasing` - Needs full file analysis
- `DuplicateStageName` - Needs stage list
- `JSONArgsRecommended` - Checks all CMD/ENTRYPOINT forms
- `CopyIgnoredFileRule` - Needs .dockerignore context
- `WorkdirRelativePath` - Analyzes path logic
- `RedundantTargetPlatform` - Cross-stage analysis
- `SecretsInArgOrEnv` - Pattern matching on values

### How to Add a New One

1. **Create rule file**: `internal/rules/buildkit/my_new_rule.go`

```go
package buildkit

import (
    "github.com/wharflab/tally/internal/rules"
)

type MyNewRule struct{}

func (r *MyNewRule) Metadata() rules.RuleMetadata {
    return rules.RuleMetadata{
        Code: rules.BuildKitRulePrefix + "MyNewRule",
        Name: "My New Rule",
        Description: "Checks for foo",
        DocURL: "https://docs.docker.com/go/dockerfile/rule/my-new-rule/",
        DefaultSeverity: rules.SeverityWarning,
        Category: "correctness",
        IsExperimental: false,
    }
}

func (r *MyNewRule) Check(input rules.LintInput) []rules.Violation {
    var violations []rules.Violation

    for _, stage := range input.Stages {
        for _, cmd := range stage.Commands {
            // Check logic here
            if shouldViolate {
                violations = append(violations, rules.NewViolation(
                    loc,
                    r.Metadata().Code,  // "buildkit/MyNewRule"
                    "message",
                    r.Metadata().DefaultSeverity,
                ).WithDocURL(r.Metadata().DocURL))
            }
        }
    }

    return violations
}

func init() {
    rules.Register(NewMyNewRule())
}
```

2. **Optional: Add to registry** (`registry.go`):

```go
// Add to allRules if you want metadata available for CLI discovery
{&linter.RuleMyNewRule, rules.SeverityWarning, "correctness"},
```

3. **Optional: Add auto-fix** (`internal/rules/buildkit/fixes/my_new_rule.go`):

```go
func enrichMyNewRuleFix(v *rules.Violation, source []byte) {
    // Fix implementation
    v.SuggestedFix = &rules.SuggestedFix{
        Description: "Fix description",
        Safety: rules.FixSafe,
        Edits: []rules.TextEdit{/* ... */},
    }
}
```

4. **Register fix enricher** (`fixes/enricher.go`):

```go
// Add to fixableRuleNames
var fixableRuleNames = []string{
    // ... existing
    "MyNewRule",
}

// Add to EnrichBuildKitFixes switch
case "MyNewRule":
    enrichMyNewRuleFix(v, source)
```

---

## Key Constants

In `internal/rules/violation.go`:

```go
const (
    BuildKitRulePrefix = "buildkit/"
    TallyRulePrefix = "tally/"
    HadolintRulePrefix = "hadolint/"
)
```

---

## Pattern Summary

| Aspect | Details |
|--------|---------|
| **Violation Code Format** | `"buildkit/RuleName"` for all BuildKit-related violations |
| **Parse-Time Capture** | BuildKit linter callback during `instructions.Parse()` |
| **Custom Implementations** | Registered via `init()` functions, implement `Rule` interface |
| **Fix Enrichment** | Post-processing step that modifies violations in-place |
| **Fix Routing** | Based on rule name extracted from RuleCode prefix |
| **Registry Purpose** | Metadata for CLI discovery and documentation |
| **Fixable Rules List** | Explicit list of rules that have fix implementations |
