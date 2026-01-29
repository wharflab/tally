# Priority 7: Violation Processing Pipeline + Rule Configuration

## Implementation Plan

Based on research of the current codebase and golangci-lint's processor architecture.

---

## Phase 1: New Configuration Structure for Namespaced Rules

### Goal
Make BuildKit's 22 captured rules "first-class citizens" - individually configurable with enable/disable and severity overrides.

### New Config Structure

```toml
# Global output settings (unchanged)
[output]
format = "text"
path = "stdout"
show-source = true
fail-level = "style"

# Per-rule configuration with namespaced keys
# Rule selection (Ruff-style include/exclude)
[rules]
include = ["buildkit/*", "tally/*"]           # Enable rules by namespace
exclude = ["buildkit/MaintainerDeprecated", "hadolint/DL3006"]  # Disable specific rules

# Per-rule configuration (severity, options)
[rules.tally.max-lines]
severity = "warning"    # Override default severity
max = 100
skip-blank-lines = true
skip-comments = true

[rules.tally.secrets-in-code]
severity = "error"
exclude.paths = ["test/**", "testdata/**"]

[rules.buildkit.StageNameCasing]
severity = "info"       # Downgrade from default warning

[rules.hadolint.DL3026]
severity = "warning"
trusted-registries = ["docker.io", "gcr.io"]
```

### Implementation Details

1. **New `RuleConfig` struct** in `internal/config/rules.go`:

```go
// RuleConfig represents per-rule configuration.
type RuleConfig struct {
    Enabled  *bool           `koanf:"enabled"`   // nil = use default
    Severity string          `koanf:"severity"`  // empty = use default
    Options  map[string]any  `koanf:"options"`   // Rule-specific options
    Exclude  ExcludeConfig   `koanf:"exclude"`   // Per-rule exclusions
}

// ExcludeConfig defines exclusion patterns.
type ExcludeConfig struct {
    Paths []string `koanf:"paths"` // Glob patterns
}

// RulesConfig is a map of rule code -> config
type RulesConfig map[string]RuleConfig
```

2. **Changes to `Config` struct**:

```go
type Config struct {
    Rules            RulesConfig            `koanf:"rules"`
    Output           OutputConfig           `koanf:"output"`
    InlineDirectives InlineDirectivesConfig `koanf:"inline-directives"`
    ConfigFile       string                 `koanf:"-"`
}
```

3. **Helper methods on `RulesConfig`**:

```go
// IsEnabled checks if a rule is enabled (considering namespace defaults).
func (rc RulesConfig) IsEnabled(ruleCode string) bool

// GetSeverity returns severity override for a rule (empty = use default).
func (rc RulesConfig) GetSeverity(ruleCode string) string

// GetOptions returns rule-specific options.
func (rc RulesConfig) GetOptions(ruleCode string) map[string]any

// GetExcludePaths returns exclusion patterns for a rule.
func (rc RulesConfig) GetExcludePaths(ruleCode string) []string
```

---

## Phase 2: Processor Chain Architecture

### Goal
Centralize violation processing in a composable, testable pipeline (golangci-lint style).

### New Package: `internal/processor/`

```text
internal/processor/
├── processor.go          # Processor interface and chain runner
├── path.go               # Path normalization
├── enable.go             # Config-based enable/disable filtering
├── severity.go           # Severity override application
├── directive.go          # Inline directive filter (migrate from check.go)
├── dedup.go              # Deduplication
├── sort.go               # Stable sorting
├── snippet.go            # SourceCode attachment
└── processor_test.go     # Unit tests
```

### Core Interface

```go
// Processor transforms a slice of violations.
type Processor interface {
    // Name returns the processor's identifier (for debugging/logging).
    Name() string

    // Process applies the processor's logic to violations.
    // Returns the transformed slice (may be same, filtered, or modified).
    Process(violations []rules.Violation, ctx *Context) []rules.Violation
}

// Context provides shared state for processors.
type Context struct {
    Config       *config.Config
    FileSources  map[string][]byte    // For snippet extraction
    SourceMaps   map[string]*sourcemap.SourceMap // Cached source maps
}

// Chain runs processors in sequence.
type Chain struct {
    processors []Processor
}

func NewChain(processors ...Processor) *Chain

func (c *Chain) Process(violations []rules.Violation, ctx *Context) []rules.Violation
```

### Processor Implementations

#### 1. PathNormalization
Converts paths to forward slashes for cross-platform consistency.

```go
type PathNormalization struct{}

func (p *PathNormalization) Name() string { return "path-normalization" }

func (p *PathNormalization) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
    for i := range violations {
        violations[i].Location.File = filepath.ToSlash(violations[i].Location.File)
    }
    return violations
}
```

#### 2. EnableFilter
Removes violations for disabled rules.

```go
type EnableFilter struct{}

func (p *EnableFilter) Name() string { return "enable-filter" }

func (p *EnableFilter) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
    return filterViolations(violations, func(v rules.Violation) bool {
        return ctx.Config.Rules.IsEnabled(v.RuleCode)
    })
}
```

#### 3. SeverityOverride
Applies severity overrides from config.

```go
type SeverityOverride struct{}

func (p *SeverityOverride) Name() string { return "severity-override" }

func (p *SeverityOverride) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
    for i := range violations {
        if override := ctx.Config.Rules.GetSeverity(violations[i].RuleCode); override != "" {
            if sev, err := rules.ParseSeverity(override); err == nil {
                violations[i].Severity = sev
            }
        }
    }
    return violations
}
```

#### 4. PathExclusionFilter
Filters violations based on per-rule path exclusions.

```go
type PathExclusionFilter struct {
    matchers map[string][]glob.Glob // Cached compiled patterns
}

func (p *PathExclusionFilter) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
    return filterViolations(violations, func(v rules.Violation) bool {
        patterns := ctx.Config.Rules.GetExcludePaths(v.RuleCode)
        for _, pattern := range patterns {
            if match, _ := doublestar.Match(pattern, v.Location.File); match {
                return false // excluded
            }
        }
        return true
    })
}
```

#### 5. InlineDirectiveFilter
Migrated from `check.go`, applies `# tally ignore=...` etc.

```go
type InlineDirectiveFilter struct {
    unusedViolations []rules.Violation // Collected for later reporting
}

func (p *InlineDirectiveFilter) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
    if !ctx.Config.InlineDirectives.Enabled {
        return violations
    }
    // Group by file, apply directive.Filter() per file
    // Collect unused directive warnings
    // ...
}

// UnusedDirectiveViolations returns warnings for unused directives.
func (p *InlineDirectiveFilter) UnusedDirectiveViolations() []rules.Violation
```

#### 6. Deduplication
Removes duplicate violations (same file, line, rule).

```go
type Deduplication struct{}

func (p *Deduplication) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
    seen := make(map[string]bool)
    return filterViolations(violations, func(v rules.Violation) bool {
        key := fmt.Sprintf("%s:%d:%s", v.Location.File, v.Location.Start.Line, v.RuleCode)
        if seen[key] {
            return false
        }
        seen[key] = true
        return true
    })
}
```

#### 7. Sorting
Stable sort by file, line, column, rule code.

```go
type Sorting struct{}

func (p *Sorting) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
    reporter.SortViolations(violations) // Reuse existing sort
    return violations
}
```

#### 8. SnippetAttachment
Populates `Violation.SourceCode` from file sources.

```go
type SnippetAttachment struct{}

func (p *SnippetAttachment) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
    for i := range violations {
        if violations[i].SourceCode != "" {
            continue // Already has snippet
        }
        if sm, ok := ctx.SourceMaps[violations[i].Location.File]; ok {
            violations[i].SourceCode = extractSnippet(sm, violations[i].Location)
        }
    }
    return violations
}
```

---

## Phase 3: BuildKit Rules Registry

### Goal
Register all 22 BuildKit Phase 1 rules as first-class citizens with proper metadata.

### New File: `internal/rules/buildkit/registry.go`

```go
// BuildKitRule represents metadata for a BuildKit linter rule.
type BuildKitRule struct {
    Name            string
    Description     string
    DocURL          string
    DefaultSeverity rules.Severity
    Category        string
}

// Registry of all BuildKit rules with their metadata.
var Registry = map[string]BuildKitRule{
    "StageNameCasing": {
        Name:            "Stage name should be lowercase",
        Description:     "Stage names should use lowercase letters for consistency",
        DocURL:          "https://docs.docker.com/go/dockerfile/rule/stage-name-casing/",
        DefaultSeverity: rules.SeverityWarning,
        Category:        "style",
    },
    "FromAsCasing": {
        Name:            "FROM AS should be lowercase",
        Description:     "The 'as' keyword in FROM should be lowercase",
        DocURL:          "https://docs.docker.com/go/dockerfile/rule/from-as-casing/",
        DefaultSeverity: rules.SeverityWarning,
        Category:        "style",
    },
    // ... all 22 rules
}

// GetMetadata returns RuleMetadata for a BuildKit rule.
func GetMetadata(ruleName string) rules.RuleMetadata {
    r := Registry[ruleName]
    return rules.RuleMetadata{
        Code:            rules.BuildKitRulePrefix + ruleName,
        Name:            r.Name,
        Description:     r.Description,
        DocURL:          r.DocURL,
        DefaultSeverity: r.DefaultSeverity,
        Category:        r.Category,
        EnabledByDefault: true, // BuildKit rules are enabled by default
        IsExperimental:   false,
    }
}
```

### Complete BuildKit Rule List (from research)

| Rule Name | Description | Default Severity |
|-----------|-------------|------------------|
| StageNameCasing | Stage names should be lowercase | warning |
| FromAsCasing | FROM AS should be lowercase | warning |
| NoEmptyContinuation | Empty continuation line | error |
| ConsistentInstructionCasing | Instructions should be consistent case | warning |
| DuplicateStageName | Duplicate stage name | error |
| ReservedStageName | Reserved stage name used | error |
| JSONArgsRecommended | JSON arguments recommended | info |
| MaintainerDeprecated | MAINTAINER is deprecated | warning |
| UndefinedArgInFrom | ARG used in FROM before definition | warning |
| UndefinedVar | Undefined variable reference | warning |
| WorkdirRelativePath | WORKDIR should use absolute path | warning |
| InvalidDefaultArgInFrom | Invalid default value for ARG in FROM | error |
| FromPlatformFlagConstDisallowed | Platform flag with constant value | warning |
| CopyIgnoredFile | COPY source in .dockerignore | warning |
| InvalidBaseImagePlatform | Base image platform mismatch | error |
| RedundantTargetPlatform | Redundant TARGETPLATFORM | info |
| SecretsUsedInArgOrEnv | Secrets in ARG/ENV | warning |
| InvalidDefinitionDescription | Invalid rule definition | error |
| LegacyKeyValueFormat | Legacy key=value format | warning |
| FileConsistentCommandCasing | Inconsistent command casing | warning |
| MultipleInstructionsDisallowed | Multiple instructions on same line | warning |
| CopyRequiresChown | COPY missing --chown | info |

---

## Phase 4: Integration in check.go

### Updated Flow

```go
// After collecting all violations from rules + BuildKit warnings:

// Build processor context
procCtx := &processor.Context{
    Config:      cfg,
    FileSources: fileSources,
    SourceMaps:  make(map[string]*sourcemap.SourceMap),
}
for path, source := range fileSources {
    procCtx.SourceMaps[path] = sourcemap.New(source)
}

// Build processor chain
inlineFilter := processor.NewInlineDirectiveFilter()
chain := processor.NewChain(
    processor.NewPathNormalization(),
    processor.NewEnableFilter(),
    processor.NewSeverityOverride(),
    processor.NewPathExclusionFilter(),
    inlineFilter,
    processor.NewDeduplication(),
    processor.NewSorting(),
    processor.NewSnippetAttachment(),
)

// Process violations
allViolations = chain.Process(allViolations, procCtx)

// Add unused directive warnings if configured
if cfg.InlineDirectives.WarnUnused {
    allViolations = append(allViolations, inlineFilter.UnusedDirectiveViolations()...)
    // Re-sort after adding
    reporter.SortViolations(allViolations)
}

// Report (no longer needs to do sorting or snippet work)
rep.Report(allViolations, fileSources)
```

---

## Phase 5: Migration & Breaking Changes

Since the tool isn't released, we can make clean breaking changes:

### Config Migration

**Before** (current):
```toml
[rules.max-lines]
max = 50
skip-blank-lines = true
```

**After** (new nested format):
```toml
[rules.tally.max-lines]
enabled = true
severity = "warning"
max = 50
skip-blank-lines = true
skip-comments = true
```

### Code Cleanup

1. Remove `RulesConfig` struct with hardcoded rule configs
2. Remove `MaxLinesRule` type - use generic `RuleConfig` + `Options` map
3. Remove `getRuleConfig()` switch statement - use generic config lookup
4. Move inline directive processing from `check.go` to `InlineDirectiveFilter` processor

---

## Implementation Order

1. **Phase 1.1**: Create `internal/config/rules.go` with new `RulesConfig` type
2. **Phase 1.2**: Update `Config` struct and config loading
3. **Phase 2.1**: Create `internal/processor/processor.go` with core types
4. **Phase 2.2**: Implement simple processors (PathNormalization, Sorting, Dedup)
5. **Phase 2.3**: Implement config-aware processors (EnableFilter, SeverityOverride)
6. **Phase 2.4**: Migrate InlineDirectiveFilter from check.go
7. **Phase 2.5**: Implement SnippetAttachment
8. **Phase 3**: Create BuildKit rules registry
9. **Phase 4**: Integrate processor chain in check.go
10. **Phase 5**: Update tests and snapshots

---

## Success Criteria

- [ ] Output is stable across runs (sorting processor ensures deterministic order)
- [ ] Severity overrides work via config (`[rules.buildkit.StageNameCasing] severity = "info"`)
- [ ] Rules can be enabled/disabled via include/exclude (`include = ["buildkit/*"]`, `exclude = ["buildkit/MaintainerDeprecated"]`)
- [ ] BuildKit rules are individually configurable (all 22 in registry)
- [ ] Snippet attachment works without reporter-specific hacks
- [ ] Deduplication prevents same violation from appearing twice
- [ ] Per-rule path exclusions work
- [ ] Integration tests pass with updated config format
