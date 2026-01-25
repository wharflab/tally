# Context-Aware Linting Foundation

**Research Focus:** Architectural foundation to support future context-aware features

---

## What is Context-Aware Linting?

Context-aware linting analyzes Dockerfiles with **additional build-time information**:

- Build arguments (`--build-arg`)
- Target platform (`--platform`)
- Build context files (source code, config files)
- `.dockerignore` file
- External files referenced by COPY/ADD
- Multi-file Dockerfile relationships
- Registry information (for base image validation)

**Example: Why it matters**

```dockerfile
# Without context
COPY package.json /app/  # Is package.json in .dockerignore? Unknown.

# With context
COPY package.json /app/  # ✓ Can verify file exists and isn't ignored
```

---

## Current Landscape

### BuildKit's Context-Aware Validation

Docker buildx bake `--check` is already context-aware:

**CopyIgnoredFile rule:**

```go
// Checks if COPY source is in .dockerignore
func checkCopyIgnoredFile(copyInst *Node, dockerignore []string) []Warning {
    for _, source := range copySources {
        if isIgnored(source, dockerignore) {
            warnings = append(warnings, Warning{
                RuleName: "CopyIgnoredFile",
                Detail: fmt.Sprintf("Attempting to copy %s which is excluded", source),
            })
        }
    }
    return warnings
}
```

**Platform validation:**

```go
// Validates FROM --platform against target platform
func validateBasePlatform(from *Node, targetPlatform string, buildArgs map[string]string) []Warning {
    basePlatform := expandVars(extractPlatform(from), buildArgs)
    if basePlatform != "" && basePlatform != targetPlatform {
        return []Warning{{
            RuleName: "InvalidBaseImagePlatform",
            Detail: fmt.Sprintf("Base platform %s doesn't match target %s",
                               basePlatform, targetPlatform),
        }}
    }
    return nil
}
```

### Advanced Context-Aware Checks

**Examples from BuildKit and Hadolint:**

1. **File existence validation**
   - COPY refers to non-existent file
   - WORKDIR path doesn't exist in context

2. **Package manager checks**
   - Verify package names against repositories
   - Check for outdated/vulnerable packages

3. **Multi-stage optimization**
   - Detect unreferenced stages
   - Optimize layer caching based on COPY patterns

4. **Registry validation**
   - Verify base image exists
   - Check for known vulnerabilities (trivy integration)
   - Validate image digests

---

## Architectural Foundation

### 1. Context Interface

**Design for extensibility:**

```go
// internal/context/context.go
package context

// BuildContext contains all contextual information for linting
type BuildContext struct {
    // Build configuration
    BuildArgs map[string]string
    Platform  string
    Target    string  // Target stage for multi-stage builds

    // File system context
    ContextDir   string      // Build context directory
    Dockerignore []string    // Parsed .dockerignore patterns
    Files        FileSet     // Available files in context

    // External data
    Registry RegistryClient  // For base image validation
    Cache    *Cache          // For caching expensive checks
}

// FileSet represents files available in build context
type FileSet struct {
    files map[string]FileInfo
}

type FileInfo struct {
    Path    string
    Size    int64
    ModTime time.Time
    IsDir   bool
}

// NewBuildContext creates a context from build options
func NewBuildContext(opts ...ContextOption) (*BuildContext, error) {
    ctx := &BuildContext{
        BuildArgs: make(map[string]string),
        Files:     NewFileSet(),
    }

    for _, opt := range opts {
        if err := opt(ctx); err != nil {
            return nil, err
        }
    }

    return ctx, nil
}

// ContextOption is a functional option for BuildContext
type ContextOption func(*BuildContext) error

// WithContextDir sets the build context directory
func WithContextDir(dir string) ContextOption {
    return func(ctx *BuildContext) error {
        ctx.ContextDir = dir
        return ctx.loadContextFiles()
    }
}

// WithBuildArgs sets build arguments
func WithBuildArgs(args map[string]string) ContextOption {
    return func(ctx *BuildContext) error {
        ctx.BuildArgs = args
        return nil
    }
}

// WithPlatform sets the target platform
func WithPlatform(platform string) ContextOption {
    return func(ctx *BuildContext) error {
        ctx.Platform = platform
        return nil
    }
}

// Helper methods
func (ctx *BuildContext) FileExists(path string) bool {
    _, ok := ctx.Files.files[path]
    return ok
}

func (ctx *BuildContext) IsIgnored(path string) bool {
    for _, pattern := range ctx.Dockerignore {
        if match, _ := filepath.Match(pattern, path); match {
            return true
        }
    }
    return false
}

func (ctx *BuildContext) ExpandVars(s string) string {
    return os.Expand(s, func(key string) string {
        if val, ok := ctx.BuildArgs[key]; ok {
            return val
        }
        return ""
    })
}

func (ctx *BuildContext) loadContextFiles() error {
    // Scan context directory
    err := filepath.WalkDir(ctx.ContextDir, func(path string, d os.DirEntry, err error) error {
        if err != nil {
            return err
        }

        relPath, _ := filepath.Rel(ctx.ContextDir, path)
        info, _ := d.Info()

        ctx.Files.files[relPath] = FileInfo{
            Path:    relPath,
            Size:    info.Size(),
            ModTime: info.ModTime(),
            IsDir:   d.IsDir(),
        }

        return nil
    })

    // Load .dockerignore
    dockerignorePath := filepath.Join(ctx.ContextDir, ".dockerignore")
    if data, err := os.ReadFile(dockerignorePath); err == nil {
        ctx.Dockerignore = parseDockerignore(string(data))
    }

    return err
}

func parseDockerignore(content string) []string {
    var patterns []string
    for _, line := range strings.Split(content, "\n") {
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        patterns = append(patterns, line)
    }
    return patterns
}
```

### 2. Rule Interface Extension

**Add optional context parameter:**

```go
// internal/linter/rule.go

type Rule struct {
    // ... existing fields ...

    // Context-aware check (optional)
    CheckWithContext ContextRuleFunc

    // Indicates if rule requires context
    RequiresContext bool
}

// ContextRuleFunc is the signature for context-aware rules
type ContextRuleFunc func(
    ast *parser.AST,
    semantic *parser.SemanticModel,
    ctx *context.BuildContext,
) []Violation

// Rule execution logic
func (r *Rule) Execute(ast *parser.AST, semantic *parser.SemanticModel, ctx *context.BuildContext) []Violation {
    // Use context-aware check if available and context provided
    if r.CheckWithContext != nil && ctx != nil {
        return r.CheckWithContext(ast, semantic, ctx)
    }

    // Fall back to basic check
    if r.Check != nil {
        return r.Check(ast, semantic)
    }

    return nil
}
```

### 3. Context-Aware Rule Example

```go
// internal/rules/copy/copy_ignored_file.go
package copy

import (
    "github.com/tinovyatkin/tally/internal/context"
    "github.com/tinovyatkin/tally/internal/linter"
    "github.com/tinovyatkin/tally/internal/parser"
)

var CopyIgnoredFileRule = &linter.Rule{
    Code:             "DL3060",
    Name:             "COPY references ignored file",
    Description:      "Attempting to COPY a file that is excluded by .dockerignore",
    Category:         "correctness",
    Severity:         linter.SeverityError,
    RequiresContext:  true,  // ← Indicates context is needed
    CheckWithContext: checkCopyIgnoredFile,
}

func checkCopyIgnoredFile(
    ast *parser.AST,
    semantic *parser.SemanticModel,
    ctx *context.BuildContext,
) []linter.Violation {
    var violations []linter.Violation

    for _, node := range ast.FindInstructions("COPY", "ADD") {
        sources := extractSources(node)

        for _, src := range sources {
            // Check if file is in .dockerignore
            if ctx.IsIgnored(src) {
                violations = append(violations, linter.Violation{
                    RuleCode: "DL3060",
                    Message:  fmt.Sprintf("Attempting to copy '%s' which is excluded by .dockerignore", src),
                    Line:     node.StartLine,
                    Severity: linter.SeverityError,
                })
            }

            // Check if file exists in context
            if !ctx.FileExists(src) {
                violations = append(violations, linter.Violation{
                    RuleCode: "DL3061",
                    Message:  fmt.Sprintf("COPY source '%s' does not exist in build context", src),
                    Line:     node.StartLine,
                    Severity: linter.SeverityError,
                })
            }
        }
    }

    return violations
}

func extractSources(node *parser.Node) []string {
    // Parse COPY/ADD arguments
    // COPY [--from=...] <src>... <dest>
    var sources []string
    current := node.Next

    for current != nil && current.Next != nil {  // Last arg is destination
        if !strings.HasPrefix(current.Value, "--") {
            sources = append(sources, current.Value)
        }
        current = current.Next
    }

    return sources
}
```

---

## Progressive Enhancement Strategy

### Phase 1: Context-Optional (v1.0)

**Goal:** All rules work without context, some enhanced with context

```go
// Linter can run with or without context
func (l *Linter) LintFile(path string, ctx *context.BuildContext) ([]Violation, error) {
    ast := parse(path)
    semantic := buildSemanticModel(ast)

    var violations []Violation
    for _, rule := range l.rules {
        // Skip context-requiring rules if no context provided
        if rule.RequiresContext && ctx == nil {
            continue  // Or emit a note that rule was skipped
        }

        v := rule.Execute(ast, semantic, ctx)
        violations = append(violations, v...)
    }

    return violations, nil
}
```

**CLI:**

```bash
# Basic linting (no context)
tally check Dockerfile

# Context-aware linting
tally check --context . Dockerfile
tally check --build-arg VERSION=1.0 --context . Dockerfile
```

### Phase 2: Default Context (v1.5)

**Goal:** Auto-detect build context

```go
func (l *Linter) LintFile(path string, ctx *context.BuildContext) ([]Violation, error) {
    // Auto-create context if not provided
    if ctx == nil {
        // Detect build context from Dockerfile location
        contextDir := filepath.Dir(path)
        ctx, _ = context.NewBuildContext(
            context.WithContextDir(contextDir),
        )
    }

    // ... rest of linting
}
```

### Phase 3: Full Context Integration (v2.0)

**Goal:** Deep integration with build system

- Registry API integration
- Vulnerability scanning
- Layer analysis
- Build caching optimization recommendations

---

## Configuration

### Enable/Disable Context-Aware Rules

```toml
# .tally.toml

[context]
# Enable context-aware linting (requires --context flag or auto-detection)
enabled = true

# Directory to use as build context (default: Dockerfile directory)
dir = "."

# Build arguments for variable expansion
[context.build-args]
VERSION = "1.0.0"
ENVIRONMENT = "production"

# Target platform
platform = "linux/amd64"

# Rules that require context
[rules]
DL3060 = { enabled = true }  # COPY ignored file
DL3061 = { enabled = true }  # COPY non-existent file
```

### CLI Flags

```go
// cmd/tally/cmd/check.go
&cli.StringFlag{
    Name:  "context",
    Usage: "Build context directory (enables context-aware rules)",
},
&cli.StringSliceFlag{
    Name:  "build-arg",
    Usage: "Build arguments (can be specified multiple times)",
},
&cli.StringFlag{
    Name:  "platform",
    Usage: "Target platform (e.g., linux/amd64)",
},
```

---

## Advanced Context Features

### 1. Registry Client Interface

**For base image validation:**

```go
// internal/context/registry.go

type RegistryClient interface {
    // Check if image exists
    ImageExists(image string) (bool, error)

    // Get image manifest
    GetManifest(image string) (*Manifest, error)

    // Get image digest
    GetDigest(image string) (string, error)
}

type Manifest struct {
    Layers   []Layer
    Config   Config
    Platform Platform
}

// Example usage in rule
func checkBaseImageExists(
    ast *parser.AST,
    semantic *parser.SemanticModel,
    ctx *context.BuildContext,
) []linter.Violation {
    var violations []linter.Violation

    for _, stage := range semantic.Stages {
        if ctx.Registry != nil {
            exists, _ := ctx.Registry.ImageExists(stage.BaseImage)
            if !exists {
                violations = append(violations, linter.Violation{
                    RuleCode: "DL3062",
                    Message:  fmt.Sprintf("Base image '%s' not found in registry", stage.BaseImage),
                })
            }
        }
    }

    return violations
}
```

### 2. File Content Analysis

**For deeper validation:**

```go
type FileSet struct {
    files map[string]FileInfo
}

// Read file from context
func (fs *FileSet) ReadFile(path string) ([]byte, error) {
    info, ok := fs.files[path]
    if !ok {
        return nil, fmt.Errorf("file not found: %s", path)
    }
    return os.ReadFile(info.Path)
}

// Example: Validate package.json in COPY
func checkPackageJsonValid(ctx *context.BuildContext) []linter.Violation {
    if !ctx.FileExists("package.json") {
        return nil
    }

    data, _ := ctx.Files.ReadFile("package.json")
    var pkg map[string]interface{}
    if err := json.Unmarshal(data, &pkg); err != nil {
        return []linter.Violation{{
            RuleCode: "DL3063",
            Message:  "package.json is not valid JSON",
        }}
    }

    return nil
}
```

### 3. Caching

**For expensive operations:**

```go
type Cache struct {
    store map[string]interface{}
    mu    sync.RWMutex
}

func (c *Cache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    val, ok := c.store[key]
    return val, ok
}

func (c *Cache) Set(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.store[key] = value
}

// Usage: Cache registry lookups
func checkBaseImageWithCache(image string, ctx *context.BuildContext) bool {
    cacheKey := "image:exists:" + image

    // Check cache first
    if val, ok := ctx.Cache.Get(cacheKey); ok {
        return val.(bool)
    }

    // Query registry
    exists, _ := ctx.Registry.ImageExists(image)

    // Cache result
    ctx.Cache.Set(cacheKey, exists)

    return exists
}
```

---

## Testing Context-Aware Rules

### Test Fixtures

```text
internal/integration/testdata/
├── with_context/
│   ├── Dockerfile
│   ├── package.json
│   ├── .dockerignore
│   └── src/
│       └── app.js
└── copy_ignored/
    ├── Dockerfile
    ├── .dockerignore
    └── ignored.txt
```

### Test Implementation

```go
func TestContextAwareRules(t *testing.T) {
    tests := []struct {
        name    string
        fixture string
        context *context.BuildContext
        want    []string  // Expected violation codes
    }{
        {
            name:    "COPY ignored file",
            fixture: "copy_ignored",
            context: &context.BuildContext{
                ContextDir:   "testdata/copy_ignored",
                Dockerignore: []string{"ignored.txt"},
            },
            want: []string{"DL3060"},
        },
        {
            name:    "COPY non-existent file",
            fixture: "copy_nonexist",
            context: &context.BuildContext{
                ContextDir: "testdata/copy_nonexist",
            },
            want: []string{"DL3061"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            path := filepath.Join("testdata", tt.fixture, "Dockerfile")
            violations, _ := linter.LintFile(path, tt.context)

            got := extractCodes(violations)
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

## Migration Path

### Phase 1: Foundation (v1.0 - Current Sprint)

- [x] Define BuildContext interface
- [ ] Add context parameter to rule interface (optional)
- [ ] Update linter to accept context
- [ ] CLI flags for context options

### Phase 2: Basic Context Rules (v1.1)

- [ ] Implement CopyIgnoredFile rule
- [ ] Implement CopyNonExistent rule
- [ ] Add .dockerignore parsing
- [ ] File system context scanning

### Phase 3: Build Args & Platform (v1.2)

- [ ] Variable expansion with build args
- [ ] Platform validation rules
- [ ] ARG/ENV resolution with context

### Phase 4: Advanced Features (v2.0+)

- [ ] Registry client integration
- [ ] Image manifest validation
- [ ] Vulnerability scanning hooks
- [ ] Layer optimization analysis
- [ ] Multi-file Dockerfile analysis

---

## Key Takeaways

1. ✅ **Design context as optional** - Rules work with or without it
2. ✅ **Use functional options** - Flexible context construction
3. ✅ **Progressive enhancement** - Start simple, add features incrementally
4. ✅ **Cache expensive operations** - Registry lookups, file scans
5. ✅ **Clear interfaces** - Easy to mock for testing
6. 🔄 **Defer complexity** - v1.0 doesn't need full context support

---

## References

- BuildKit context rules: `moby/buildkit/frontend/dockerfile/linter/`
- Docker buildx: `docker/buildx/commands/build.go`
- Hadolint context: Uses ShellCheck for shell script validation
