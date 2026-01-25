# Linter Pipeline Architecture

**Research Focus:** How modern linters structure their processing pipelines

Based on analysis of ruff (Rust/Python), oxlint (Rust/JS/TS), and golangci-lint (Go).

---

## Standard Pipeline Stages

Modern linters follow a consistent multi-stage pipeline:

```text
┌─────────────┐
│ 1. Discovery│  Find files to analyze
└──────┬──────┘
       │
┌──────▼──────┐
│ 2. Parsing  │  Build AST/CST + semantic model
└──────┬──────┘
       │
┌──────▼──────┐
│ 3. Analysis │  Run rules against AST
└──────┬──────┘
       │
┌──────▼──────┐
│ 4. Filtering│  Apply suppressions, deduplication
└──────┬──────┘
       │
┌──────▼──────┐
│ 5. Reporting│  Format and output results
└─────────────┘
```

---

## 1. File Discovery Stage

### Patterns Observed

**Ruff approach:**

```rust
// File discovery with .gitignore support
let (paths, resolver) = python_files_in_path(
    files,
    pyproject_config,
    config_arguments
)?;

// Package root discovery for semantic context
let package_roots = resolver.package_roots(&paths);
```

**Key features:**

- Respects `.gitignore` and language-specific ignore files
- Glob pattern support
- Recursive directory traversal
- Path normalization and canonicalization
- Symlink handling

### For Tally

```text
Input: File paths, directories, or glob patterns
       ├─ Single file: "Dockerfile"
       ├─ Directory: "." (finds all Dockerfiles)
       ├─ Pattern: "**/Dockerfile*"
       └─ Multiple: ["Dockerfile", "build/Dockerfile.prod"]

Output: List of normalized file paths to analyze
        ├─ Filter by .dockerignore (context-aware mode)
        ├─ Filter by config exclusions
        └─ Deduplicate paths
```

**Implementation recommendation:**

- Use Go's `filepath.Walk` or `filepath.WalkDir` for traversal
- Consider `github.com/bmatcuk/doublestar/v4` for advanced glob patterns
- Support `.dockerignore` file detection for context-aware mode
- Cache resolved paths for multi-file runs

---

## 2. Parsing Stage

### AST vs CST Decision

**Ruff and oxlint both use AST**, not full CST:

- Parse source code into abstract syntax tree
- Preserve essential trivia (comments, line numbers)
- Drop irrelevant whitespace details

**Semantic model construction:**

**Oxlint approach (explicit semantic pass):**

```rust
// 1. Parse to AST (fast, syntax-only)
let ast = Parser::new(&allocator, source_text).parse();

// 2. Build semantic model (slower, but needed for advanced rules)
let semantic = SemanticBuilder::new(source_text)
    .with_check_syntax_error(true)
    .build(&ast.program)?;

// 3. Rules access both AST and semantic information
rule.run(node, &LintContext::new(&semantic));
```

**Ruff approach (lazy semantic model):**

```rust
// Build semantic model incrementally during AST traversal
let mut checker = Checker::new(&settings, ...);
checker.visit_program(&parsed.ast);  // Single pass builds semantics on-the-fly
```

### For Tally

**Current state:** Already using `moby/buildkit/frontend/dockerfile/parser`

**Enhancement opportunities:**

1. **Preserve more context during parsing**
   - Track comment associations (which instruction does each comment precede?)
   - Store original formatting details if needed for style rules

2. **Build semantic model**
   - Track stage definitions and references
   - Build variable scope chains (ARG/ENV visibility)
   - Resolve COPY --from references
   - Track layer relationships

**Example semantic model:**

```go
type SemanticModel struct {
    Stages        map[string]*Stage          // Stage name -> Stage definition
    Variables     map[string]*Variable       // Variable name -> scope chain
    BaseImages    []BaseImageRef             // All FROM instructions
    CopyReferences []CopyFromRef             // COPY --from references
    Labels        map[string]string          // LABEL instructions
}

type Stage struct {
    Name      string
    BaseImage string
    LineRange LineRange
    Platform  string  // FROM --platform value
}

type Variable struct {
    Name       string
    Value      string
    Scope      VariableScope  // Global (ARG before FROM) or Stage-local
    Definition LineRange
    References []LineRange    // Where variable is used
}
```

**Implementation path:**

```go
// Phase 1: Use existing buildkit AST directly
ast, err := parser.Parse(reader)
// Access: ast.AST.Children (array of *Node)

// Phase 2: Build semantic model in separate pass
semantic := BuildSemanticModel(ast)

// Phase 3: Pass both to rules
for _, rule := range enabledRules {
    violations := rule.Check(ast, semantic, config)
    allViolations = append(allViolations, violations...)
}
```

---

## 3. Rule Evaluation Stage

### Concurrency Models

#### Option A: File-Level Parallelism (Ruff)

**Pattern:** Parallel files, sequential rules

```rust
let diagnostics = paths.par_iter()  // Parallel iterator
    .filter_map(|path| {
        lint_path(path, settings)  // All rules run sequentially per file
    })
    .collect();
```

**Characteristics:**

- Each file processed independently in parallel
- Within a file, rules run sequentially
- No shared mutable state between workers
- Results aggregated after all files complete

**Pros:**

- Simple to implement (no coordination between rules)
- Works with any rules
- Good scaling for projects with many files
- Thread-safe by design

**Cons:**

- Single small file doesn't parallelize
- CPU utilization limited by file count

#### Option B: Rule-Level Parallelism (golangci-lint's metalinter)

**Pattern:** Shared AST, parallel rules

```go
// Combine compatible rules into metalinter
analyzers := []analysis.Analyzer{errcheck, govet, staticcheck, ...}

// go/analysis framework runs analyzers in parallel
results := analysis.Run(analyzers, packages)
```

**Characteristics:**

- Parse once, run multiple rules in parallel
- Rules can share computation (type-checking, SSA, etc.)
- Sophisticated coordination via analysis framework

**Pros:**

- Efficient for expensive parsing
- Shares semantic information between rules
- Great for small file count, many rules

**Cons:**

- More complex coordination
- Requires rules to be thread-safe
- Framework-specific (go/analysis)

#### Option C: Adaptive Strategy (Oxlint)

**Pattern:** Different strategies based on file size

```rust
if semantic.nodes().len() > 200_000 {
    // LARGE files: iterate rules in outer loop (better cache locality)
    for (rule, ctx) in &rules {
        rule.run_once(ctx);
        for node in semantic.nodes() {
            rule.run(node, ctx);
        }
    }
} else {
    // SMALL files: iterate nodes in outer loop (faster dispatch)
    for node in semantic.nodes() {
        for (rule, ctx) in &rules {
            rule.run(node, ctx);
        }
    }
}
```

**Characteristics:**

- Optimizes for common case (small files)
- Adapts to large files to maintain performance
- Minimizes cache misses

**Pros:**

- Best of both worlds
- CPU cache-friendly
- Handles edge cases gracefully

**Cons:**

- More complex implementation
- Requires profiling to determine threshold

### For Tally

**Recommendation: Start with File-Level Parallelism (Option A)**

```go
type Linter struct {
    rules   []Rule
    config  *Config
    workers int
}

func (l *Linter) LintFiles(paths []string) ([]Violation, error) {
    var wg sync.WaitGroup
    violations := make(chan []Violation, len(paths))

    // Worker pool
    sem := make(chan struct{}, l.workers)

    for _, path := range paths {
        wg.Add(1)
        go func(p string) {
            defer wg.Done()
            sem <- struct{}{}        // Acquire
            defer func() { <-sem }() // Release

            v := l.lintFile(p)
            violations <- v
        }(path)
    }

    // Collect results
    go func() {
        wg.Wait()
        close(violations)
    }()

    // Aggregate
    var allViolations []Violation
    for v := range violations {
        allViolations = append(allViolations, v...)
    }

    return allViolations, nil
}

func (l *Linter) lintFile(path string) []Violation {
    ast := parse(path)
    semantic := buildSemanticModel(ast)

    var violations []Violation
    for _, rule := range l.rules {
        if !rule.Enabled() { continue }
        v := rule.Check(ast, semantic)
        violations = append(violations, v...)
    }

    return violations
}
```

**Why this works for Tally:**

- Dockerfiles are typically small (< 200 lines)
- File-level parallelism is simple and effective
- No coordination complexity
- Easy to implement with `golang.org/x/sync/errgroup`
- Can add adaptive strategy later if needed

**Future optimization:** If parsing becomes expensive (e.g., with tree-sitter), consider caching parsed ASTs.

---

## 4. Rule Dispatch Optimization

### Node Type Filtering (Oxlint)

Rules specify which AST node types they care about:

```rust
impl Rule for NoDebugger {
    // Only called for DebuggerStatement nodes
    fn run<'a>(&self, node: &AstNode<'a>, ctx: &LintContext<'a>) {
        if let AstKind::DebuggerStatement(stmt) = node.kind() {
            ctx.diagnostic(...);
        }
    }
}

// During traversal
for node in semantic.nodes() {
    for (rule, ctx) in &rules {
        if rule.matches_node_type(node) {  // Bitset check
            rule.run(node, ctx);
        }
    }
}
```

**Optimization:** Use bitsets for O(1) node type checks

### For Tally

**Similar pattern for Dockerfile instructions:**

```go
type InstructionType uint32

const (
    InstructionFROM InstructionType = 1 << iota
    InstructionRUN
    InstructionCMD
    InstructionCOPY
    InstructionADD
    InstructionEXPOSE
    InstructionENV
    InstructionARG
    InstructionWORKDIR
    // ... etc
)

type Rule interface {
    Name() string
    Check(ast *AST, semantic *SemanticModel) []Violation

    // Optional: specify which instruction types this rule cares about
    InstructionTypes() InstructionType  // Bitset
}

// Optimized dispatch
func lintFile(ast *AST, rules []Rule) []Violation {
    var violations []Violation

    for _, node := range ast.Instructions() {
        instructionType := getInstructionType(node)

        for _, rule := range rules {
            // Fast bitset check
            if rule.InstructionTypes() & instructionType != 0 {
                v := rule.Check(ast, semantic)
                violations = append(violations, v...)
            }
        }
    }

    return violations
}
```

**Benefits:**

- Skip irrelevant rules for each instruction
- O(1) filtering vs. string comparisons
- Scales well as rule count grows

---

## 5. Processing Pipeline (golangci-lint style)

### Sequential Processor Chain

After rule evaluation, violations pass through a chain of processors:

```go
type Processor interface {
    Process(violations []Violation) ([]Violation, error)
    Name() string
}

type Pipeline struct {
    processors []Processor
}

func (p *Pipeline) Process(violations []Violation) ([]Violation, error) {
    current := violations

    for _, proc := range p.processors {
        var err error
        current, err = proc.Process(current)
        if err != nil {
            return nil, fmt.Errorf("processor %s: %w", proc.Name(), err)
        }
    }

    return current, nil
}
```

**Example processors for Tally:**

1. **PathNormalizer** - Convert to absolute/relative paths
2. **InlineDisableFilter** - Apply `# tally ignore=` directives
3. **ConfigExclusionFilter** - Apply config-based exclusions
4. **SeverityFilter** - Filter by minimum severity
5. **Deduplicator** - Remove duplicate violations
6. **MaxPerFileFilter** - Limit violations per file
7. **SourceCodeAttacher** - Attach source code snippets
8. **SortProcessor** - Sort for consistent output

**Configuration:**

```go
pipeline := NewPipeline(
    NewInlineDisableFilter(ast),
    NewConfigExclusionFilter(config),
    NewSeverityFilter(config.MinSeverity),
    NewDeduplicator(),
    NewMaxPerFileFilter(config.MaxViolationsPerFile),
    NewSourceCodeAttacher(files),
    NewSortProcessor(),
)

filtered, err := pipeline.Process(violations)
```

---

## 6. Reporting Stage

See `05-reporters-and-output.md` for detailed reporter architecture.

**Key points:**

- Support multiple simultaneous output formats
- Abstract reporter interface
- Format-specific implementations (JSON, SARIF, text, etc.)
- Separate formatting from I/O

---

## Key Architectural Decisions for Tally

### ✅ Recommended Architecture

```go
// High-level pipeline
func Run(paths []string, config *Config) error {
    // 1. Discovery
    files := discoverFiles(paths, config)

    // 2. Parallel linting (file-level)
    violations := lintFiles(files, config.Rules)

    // 3. Processing pipeline
    processed := applyProcessors(violations, config)

    // 4. Reporting
    return report(processed, config.Output)
}

// File-level linting
func lintFile(path string, rules []Rule) []Violation {
    // Parse
    ast := parse(path)

    // Semantic analysis
    semantic := buildSemanticModel(ast)

    // Run rules
    var violations []Violation
    for _, rule := range rules {
        if shouldRunRule(rule, config) {
            v := rule.Check(ast, semantic)
            violations = append(violations, v...)
        }
    }

    return violations
}
```

### Performance Targets

Based on research findings:

- **Ruff:** Lints CPython (~1.8M lines) in ~500ms
- **Oxlint:** 50-100x faster than ESLint
- **golangci-lint:** Lints large Go projects in seconds

**For Tally:**

- Target: < 100ms for typical Dockerfile (50-100 lines)
- Scale: Handle 100+ files in parallel efficiently
- Memory: Constant per file (no global state)

### Scalability Checklist

- [ ] File-level parallelism with worker pool
- [ ] No global mutable state
- [ ] Efficient AST representation (minimize allocations)
- [ ] Node type filtering for rule dispatch
- [ ] Processing pipeline for flexible filtering
- [ ] Configurable output destinations
- [ ] Rule enable/disable granularity
- [ ] Inline disable directives

---

## Next Steps

1. **Implement basic pipeline** with sequential execution
2. **Add parallelism** using `golang.org/x/sync/errgroup`
3. **Profile performance** to identify bottlenecks
4. **Optimize rule dispatch** with instruction type filtering
5. **Add caching** if parsing becomes expensive
6. **Consider adaptive strategy** for very large Dockerfiles (rare but possible)

---

## References

- Ruff source: `crates/ruff/src/commands/check.rs`, `crates/ruff_linter/src/linter.rs`
- Oxlint source: `crates/oxc_linter/src/lib.rs`
- golangci-lint source: `pkg/lint/runner.go`, `pkg/result/processors/`
