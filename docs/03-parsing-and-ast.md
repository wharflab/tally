# Parsing, AST, and Semantic Analysis for Dockerfile Linting

**Research Focus:** Available Go libraries for parsing and code analysis

---

## Executive Summary

**Recommendation:** Continue using `moby/buildkit/frontend/dockerfile/parser` for Tally v1.0.

The buildkit parser provides an AST with sufficient trivia preservation (comments, line numbers, original text) for semantic linting. Tree-sitter
could be added later for advanced style rules or editor integration, but is not necessary for the core linting use cases.

---

## AST vs CST: Understanding the Trade-offs

### Abstract Syntax Tree (AST)

**What it is:**

- Represents the **logical structure** of code
- Strips away syntactic details (exact whitespace, punctuation)
- Focuses on semantic meaning

**Example:**

```dockerfile
FROM    ubuntu:latest    AS    builder
```

AST representation:

```text
FROM
├─ Image: ubuntu:latest
└─ Alias: builder
```

**Best for:**

- Semantic analysis
- Logic validation
- Best practice enforcement
- Security checks

### Concrete Syntax Tree (CST)

**What it is:**

- Preserves **all source code details** including trivia
- Maintains exact representation of original source
- Every token, space, and comment is represented

**Example:**

```dockerfile
FROM    ubuntu:latest    AS    builder
```

CST representation:

```text
Instruction
├─ Keyword: "FROM"
├─ Whitespace: "    "
├─ Image: "ubuntu:latest"
├─ Whitespace: "    "
├─ Keyword: "AS"
├─ Whitespace: "    "
└─ Alias: "builder"
```

**Best for:**

- Code formatters
- Refactoring tools
- Style linters
- Editor tooling

### Linting Requirements

| Use Case | Requires CST? | Notes |
|----------|---------------|-------|
| Security checks (root user, exposed secrets) | ❌ | AST sufficient |
| Best practices (layer optimization, caching) | ❌ | AST sufficient |
| Deprecated instructions | ❌ | AST sufficient |
| Undefined variables | ❌ | AST + semantics |
| Comment-based documentation | ❌ | AST with preserved comments |
| Formatting/whitespace rules | ✅ | Full CST needed |
| Indentation enforcement | ✅ | Full CST needed |
| Auto-formatting | ✅ | Full CST needed |

**Conclusion:** AST with preserved comments is sufficient for 95% of linting rules.

---

## Current Parser: moby/buildkit

**Package:** `github.com/moby/buildkit/frontend/dockerfile/parser`

### What It Provides

**Node structure:**

```go
type Node struct {
    Value       string      // Current token value (e.g., "from", "run")
    Next        *Node       // Next sibling token
    Children    []*Node     // Child nodes (for nested structures)
    Attributes  map[string]bool  // Instruction flags (--platform, --mount, etc.)
    Original    string      // Original line before parsing
    Flags       []string    // Parsed flags
    StartLine   int         // Line where node starts
    EndLine     int         // Line where node ends
    Heredocs    []Heredoc   // Attached heredoc content
    PrevComment []string    // Comments preceding this node ✓
}

func (node *Node) Location() []parser.Range {
    // Returns source location ranges
}
```

### Capabilities ✅

| Feature | Supported | Notes |
|---------|-----------|-------|
| Line numbers | ✅ | `StartLine`, `EndLine` |
| Column positions | ✅ | Via `Location()` method |
| Comments | ✅ | `PrevComment` field |
| Original text | ✅ | `Original` field |
| Heredocs | ✅ | Full heredoc support |
| Multi-line instructions | ✅ | Via `Children` |
| Instruction flags | ✅ | `Attributes` map |
| Exact whitespace | ❌ | Not preserved |
| Full CST | ❌ | Not a concrete syntax tree |

### Example Usage

```go
package main

import (
    "os"
    "github.com/moby/buildkit/frontend/dockerfile/parser"
)

func main() {
    f, _ := os.Open("Dockerfile")
    defer f.Close()

    result, err := parser.Parse(f)
    if err != nil {
        // Handle parse errors
    }

    // Access warnings (deprecated features, etc.)
    for _, warning := range result.Warnings {
        fmt.Printf("Warning: %s\n", warning.Short)
    }

    // Walk AST
    for _, child := range result.AST.Children {
        processInstruction(child)
    }
}

func processInstruction(node *parser.Node) {
    // Instruction type (lowercase: "from", "run", "copy", etc.)
    instruction := strings.ToLower(node.Value)

    // Get arguments
    args := getArguments(node)

    // Check preceding comments
    for _, comment := range node.PrevComment {
        // Analyze comment content
    }

    // Source location
    fmt.Printf("Instruction %s at lines %d-%d\n",
               instruction, node.StartLine, node.EndLine)
}

func getArguments(node *parser.Node) []string {
    var args []string
    current := node.Next
    for current != nil {
        args = append(args, current.Value)
        current = current.Next
    }
    return args
}
```

### Assessment

**Strengths:**

- ✅ Official Docker parser (guaranteed compatibility)
- ✅ Actively maintained (part of BuildKit)
- ✅ Preserves essential trivia (comments, line numbers)
- ✅ Supports all Dockerfile syntax including heredocs
- ✅ Parse error recovery
- ✅ Warning system for deprecated features
- ✅ Zero external dependencies (besides BuildKit)

**Limitations:**

- ❌ No full CST (whitespace details lost)
- ❌ No built-in semantic analysis
- ❌ No query language for pattern matching
- ❌ Linked-list API (less ergonomic than tree iteration)

**Verdict:** Excellent foundation for semantic linting. Covers all current needs for Tally v1.0.

---

## Alternative: Tree-sitter

**What is Tree-sitter?**

A parser generator that creates incremental, error-tolerant parsers with full CST support.

### Tree-sitter Dockerfile Grammar

**Repository:** `github.com/camdencheek/tree-sitter-dockerfile`

- **Stars:** 92
- **Status:** Maintained (last update: January 2025)
- **Features:** Complete Dockerfile grammar

### Go Bindings

**Recommended:** `github.com/smacker/go-tree-sitter` ⭐

- **Stars:** 533
- **Status:** Actively maintained
- **Built-in grammars:** 30+ languages including Dockerfile
- **Features:**
  - Query support (S-expression pattern matching)
  - Incremental parsing
  - Error recovery
  - Full CST with trivia

**Example:**

```go
import (
    sitter "github.com/smacker/go-tree-sitter"
    "github.com/smacker/go-tree-sitter/dockerfile"
)

func parseDockerfile(source []byte) *sitter.Tree {
    parser := sitter.NewParser()
    parser.SetLanguage(dockerfile.GetLanguage())

    tree, _ := parser.ParseCtx(context.Background(), nil, source)
    return tree
}

// Query API for pattern matching
func findRunInstructions(tree *sitter.Tree, source []byte) []*sitter.Node {
    query := `(
        (instruction
            name: (KEYWORD) @keyword
            (#eq? @keyword "RUN")
        ) @run
    )`

    compiled, _ := sitter.NewQuery([]byte(query), dockerfile.GetLanguage())
    cursor := sitter.NewQueryCursor()
    cursor.Exec(compiled, tree.RootNode())

    var nodes []*sitter.Node
    for {
        match, ok := cursor.NextMatch()
        if !ok { break }
        nodes = append(nodes, match.Captures[0].Node)
    }
    return nodes
}
```

### When to Use Tree-sitter

**Use cases:**

1. **Formatting enforcement** - Indentation, whitespace rules
2. **Editor integration** - LSP server, syntax highlighting
3. **Advanced pattern matching** - Complex structural queries
4. **Incremental linting** - Fast re-linting after edits
5. **Auto-formatting** - Need full CST to reconstruct source

**Example tree-sitter-only rules:**

```text
- Indentation must be 2 spaces
- No trailing whitespace
- Empty lines before FROM instructions
- Consistent line continuation style (\ at end vs beginning)
```

**Not needed for:**

- Semantic validation (undefined vars, duplicate stages)
- Best practice checks (layer optimization, caching)
- Security issues (secrets, root user)
- Deprecated instructions

### Integration Approach

**Dual-parser architecture:**

```go
// Parser interface abstracts underlying parser
type Parser interface {
    Parse(content []byte) (*ParseResult, error)
}

type ParseResult struct {
    AST      interface{}  // Parser-specific AST
    Comments []Comment
    Errors   []ParseError
}

// Buildkit implementation (semantic rules)
type BuildkitParser struct{}

func (p *BuildkitParser) Parse(content []byte) (*ParseResult, error) {
    result, err := parser.Parse(bytes.NewReader(content))
    return &ParseResult{
        AST: result.AST,
        Comments: extractComments(result.AST),
        Errors: convertErrors(err),
    }, nil
}

// Tree-sitter implementation (style rules)
type TreeSitterParser struct {
    parser *sitter.Parser
}

func (p *TreeSitterParser) Parse(content []byte) (*ParseResult, error) {
    tree, err := p.parser.ParseCtx(context.Background(), nil, content)
    return &ParseResult{
        AST: tree,
        Errors: collectErrors(tree),
    }, err
}

// Rule chooses parser
type Rule interface {
    RequiredParser() ParserType  // Buildkit or TreeSitter
    Check(result *ParseResult) []Violation
}
```

**Benefits:**

- Use buildkit for 95% of rules (fast, simple)
- Use tree-sitter only for style rules (when needed)
- Pay tree-sitter cost only when style rules enabled

---

## Semantic Analysis

### What Is Semantic Analysis?

Building a model of code's meaning beyond syntax:

- **Scopes:** Which variables are visible where?
- **References:** Where is each variable defined and used?
- **Types:** What type does each expression have?
- **Control flow:** What execution paths exist?

### For Dockerfiles

**Semantic model should track:**

```go
type SemanticModel struct {
    // Stage management
    Stages map[string]*Stage  // Stage name -> definition

    // Variable scoping
    GlobalArgs map[string]*Variable  // ARG before first FROM
    StageVars  map[string]map[string]*Variable  // Per-stage ARG/ENV
    BuildArgs  map[string]string     // Build-time args from CLI (--build-arg)

    // Cross-stage references
    CopyFromRefs []CopyFromRef  // COPY --from=stage

    // Base images
    BaseImages []BaseImageRef

    // Labels
    Labels map[string]string

    // Exposed ports
    Ports []Port
}

type Stage struct {
    Name      string
    BaseImage string
    Platform  string      // FROM --platform value
    LineRange LineRange
    Variables map[string]*Variable
}

type Variable struct {
    Name       string
    Value      string
    Scope      VariableScope  // Global or stage-local
    Definition LineRange
    References []LineRange
}

type CopyFromRef struct {
    StageName string
    SourcePath string
    Line int
}
```

### Building the Semantic Model

**Single-pass construction:**

```go
func BuildSemanticModel(ast *parser.Result, buildArgs map[string]string) *SemanticModel {
    model := &SemanticModel{
        Stages: make(map[string]*Stage),
        GlobalArgs: make(map[string]*Variable),
        StageVars: make(map[string]map[string]*Variable),
        BuildArgs: buildArgs, // Build-time args from CLI
    }

    var currentStage string

    for _, node := range ast.AST.Children {
        instruction := strings.ToLower(node.Value)

        switch instruction {
        case "arg":
            // Global ARG if before first FROM
            if currentStage == "" {
                model.GlobalArgs[getArgName(node)] = parseVariable(node)
            } else {
                // Stage-local ARG
                model.StageVars[currentStage][getArgName(node)] = parseVariable(node)
            }

        case "from":
            // New stage
            stageName := getStageName(node)
            currentStage = stageName
            model.Stages[stageName] = &Stage{
                Name: stageName,
                BaseImage: getBaseImage(node),
                Platform: getPlatform(node),
                LineRange: getLineRange(node),
                Variables: make(map[string]*Variable),
            }

        case "env":
            // Stage-local environment variable
            model.StageVars[currentStage][getEnvName(node)] = parseVariable(node)

        case "copy":
            // Track COPY --from references
            if fromStage := getCopyFrom(node); fromStage != "" {
                model.CopyFromRefs = append(model.CopyFromRefs, CopyFromRef{
                    StageName: fromStage,
                    SourcePath: getCopySource(node),
                    Line: node.StartLine,
                })
            }

        case "expose":
            model.Ports = append(model.Ports, parsePorts(node)...)

        case "label":
            parseLabels(node, model.Labels)
        }
    }

    return model
}

// ResolveVariable resolves a variable name to its value with proper precedence:
// 1. BuildArgs (CLI --build-arg, highest priority)
// 2. Stage-local variables (ARG/ENV in current stage)
// 3. Global ARGs (ARG before first FROM)
func (m *SemanticModel) ResolveVariable(name string, stage string) (string, bool) {
    // 1. Check BuildArgs first (CLI overrides everything)
    if val, ok := m.BuildArgs[name]; ok {
        return val, true
    }

    // 2. Check stage-local variables
    if stageVars, ok := m.StageVars[stage]; ok {
        if variable, ok := stageVars[name]; ok {
            return variable.Value, true
        }
    }

    // 3. Check global ARGs
    if variable, ok := m.GlobalArgs[name]; ok {
        return variable.Value, true
    }

    return "", false
}
```

### Using Semantic Model in Rules

```go
// Check for undefined variable references
func CheckUndefinedVariables(ast *parser.Result, semantic *SemanticModel) []Violation {
    var violations []Violation

    for _, node := range ast.AST.Children {
        // Find variable references in instruction
        refs := extractVariableReferences(node)

        for _, ref := range refs {
            if !semantic.IsVariableDefined(ref.Name, getCurrentStage(node)) {
                violations = append(violations, Violation{
                    Rule: "undefined-variable",
                    Message: fmt.Sprintf("Variable %s is not defined", ref.Name),
                    Location: ref.Location,
                })
            }
        }
    }

    return violations
}

// Check for duplicate stage names
func CheckDuplicateStages(semantic *SemanticModel) []Violation {
    seen := make(map[string]*Stage)
    var violations []Violation

    for name, stage := range semantic.Stages {
        if existing, ok := seen[name]; ok {
            violations = append(violations, Violation{
                Rule: "duplicate-stage-name",
                Message: fmt.Sprintf("Stage '%s' already defined at line %d",
                                   name, existing.LineRange.Start),
                Location: Location{
                    File: stage.File,
                    Line: stage.LineRange.Start,
                },
            })
        }
        seen[name] = stage
    }

    return violations
}

// Check for COPY --from references to non-existent stages
func CheckCopyFromReferences(semantic *SemanticModel) []Violation {
    var violations []Violation

    for _, ref := range semantic.CopyFromRefs {
        if _, exists := semantic.Stages[ref.StageName]; !exists {
            violations = append(violations, Violation{
                Rule: "undefined-stage-reference",
                Message: fmt.Sprintf("COPY --from references undefined stage '%s'",
                                   ref.StageName),
                Location: Location{Line: ref.Line},
            })
        }
    }

    return violations
}
```

---

## Recommendations

### For Tally v1.0: Buildkit Parser + Semantic Model

#### Phase 1: AST-based linting

```go
// Parse with buildkit
result, err := parser.Parse(reader)

// Build semantic model with build args from context (if available)
buildArgs := config.BuildArgs // From CLI --build-arg flags
semantic := BuildSemanticModel(result, buildArgs)

// Run rules
violations := RunRules(result.AST, semantic, config)
```

**Rules to implement:**

- Stage naming conventions ✅
- Undefined variables ✅
- Duplicate stages ✅
- Invalid COPY --from references ✅
- Deprecated instructions ✅
- Best practices (no root, pin versions, etc.) ✅
- Comment-based documentation checks ✅

**Estimated effort:** 2-3 weeks for core implementation

### For Tally v2.0+: Add Tree-sitter (Optional)

**Only if needed for:**

- Formatting/style enforcement
- Editor integration (LSP server)
- Advanced pattern matching queries

**Integration:**

```bash
go get github.com/smacker/go-tree-sitter
go get github.com/smacker/go-tree-sitter/dockerfile
```

**Estimated effort:** 1 week for integration, 1-2 weeks for style rules

---

## Key Takeaways

1. ✅ **Buildkit parser is sufficient** for semantic linting
2. ❌ **Tree-sitter is overkill** for current requirements
3. ✅ **Semantic model is essential** for advanced rules
4. ✅ **Single-pass analysis** is fast and practical
5. 🔄 **Can add tree-sitter later** without major refactoring

---

## Implementation Checklist

### Phase 1: Buildkit Parser Enhancement

- [x] Already using `moby/buildkit/frontend/dockerfile/parser`
- [ ] Build semantic model from AST
- [ ] Track stage definitions and references
- [ ] Track variable scopes (global ARG vs stage-local)
- [ ] Extract comment associations
- [ ] Implement variable reference tracking

### Phase 2: Semantic Analysis Rules

- [ ] Undefined variable detection
- [ ] Duplicate stage name detection
- [ ] Invalid COPY --from references
- [ ] Unused variable detection
- [ ] Variable shadowing detection
- [ ] Unreachable stage detection

### Phase 3: Tree-sitter Integration (Optional)

- [ ] Add tree-sitter dependency
- [ ] Implement dual-parser architecture
- [ ] Create style rules using tree-sitter
- [ ] Performance benchmarking

---

## References

- Buildkit parser: `github.com/moby/buildkit/frontend/dockerfile/parser`
- Tree-sitter: `github.com/tree-sitter/tree-sitter`
- Tree-sitter Dockerfile: `github.com/camdencheek/tree-sitter-dockerfile`
- go-tree-sitter: `github.com/smacker/go-tree-sitter`
- Oxc semantic: `crates/oxc_semantic/src/semantic.rs`
- Ruff checker: `crates/ruff_linter/src/checkers/ast.rs`
