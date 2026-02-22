---
name: create-tally-customlint
description: >
  Create a new custom Go analysis linter (analyzer) in _tools/customlint/ for the tally project.
  Use this skill whenever the user wants to add a new lint check, custom analyzer, static analysis rule,
  or code quality check to the customlint plugin. Also use when the user says things like "add a linter
  for X", "catch bad pattern Y at CI time", "flag hardcoded Z values", or "enforce convention W in code".
---

# Creating a Custom Lint Analyzer for tally

This skill walks you through creating a new `go/analysis` analyzer in `_tools/customlint/`,
registering it in the golangci-lint module plugin, writing tests with `analysistest`, and
adding documentation.

## Before you start

Read these files to understand the project's existing analyzer patterns:

- `_tools/customlint/plugin.go` — registration point
- At least one existing analyzer (e.g. `_tools/customlint/rulestruct.go` or `_tools/customlint/docurl.go`) and its test

For detailed code templates and examples, read `references/analyzer-patterns.md` inside this skill directory.

## Step-by-step workflow

### 1. Design the analyzer

Decide:

- **Name**: short lowercase identifier (e.g. `docurl`, `rulestruct`, `lspliteral`)
- **Scope**: which packages to check — use `strings.Contains(pass.Pkg.Path(), ...)` as a guard
- **AST nodes**: which node types to filter (`*ast.KeyValueExpr`, `*ast.CallExpr`, `*ast.BasicLit`, `*ast.GenDecl`, etc.)
- **Condition**: what makes a node "bad" (e.g. a string literal where a function call is expected)
- **Message**: the diagnostic string — should tell the developer *what to do instead*

### 2. Create the analyzer file

Create `_tools/customlint/<name>.go` in package `customlint`.

Structure:

1. Declare `var <name>Analyzer = &analysis.Analyzer{...}` (unexported, camelCase)
2. Implement `func run<Name>(pass *analysis.Pass) (any, error)` (unexported)
3. Start with a scope guard: `if !strings.Contains(pass.Pkg.Path(), "<target>") { return nil, nil }`
4. Use `pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)` for AST traversal
5. Call `insp.Preorder(nodeFilter, func(n ast.Node) { ... })` with detection logic
6. Report with `pass.Reportf(pos, "use X instead of Y %s", value)`

Conventions:

- Always require `inspect.Analyzer`
- Keep imports minimal: `go/ast`, `go/token`, `strings`, and the analysis packages
- Break complex detection into helper functions (e.g. `checkDocURLField`, `checkNewIssueCall`)
- Diagnostic messages tell the developer what to use instead, not just what's wrong

### 3. Create test fixtures

Fixtures live under `_tools/customlint/testdata/src/<package-path>/`.
The package path must match what you pass to `analysistest.Run`.

Each fixture is a normal Go file with `// want` comments on lines that should trigger diagnostics.

Fixture conventions:

- Include at least one "Bad" case (triggers diagnostic) and two "Good" cases (no diagnostic)
- Add brief comments (`// Bad: ...`, `// Good: ...`) explaining each case
- The `// want` comment content is a regex — escape special chars with `\`
- Fixtures define minimal stub types/functions; they don't import from the main project
- If the analyzer checks multiple packages, create a separate fixture directory for each

### 4. Create the test file

Create `_tools/customlint/<name>_test.go`:

```go
package customlint

import (
    "testing"

    "golang.org/x/tools/go/analysis/analysistest"
)

func Test<Name>(t *testing.T) {
    t.Parallel()
    testdata := analysistest.TestData()
    analysistest.Run(t, testdata, <name>Analyzer,
        "<package/path/1>",
        "<package/path/2>", // one entry per fixture package
    )
}
```

- Test function is `Test<Name>` matching the analyzer
- Always call `t.Parallel()`
- Pass one package pattern per fixture directory

### 5. Register in plugin.go

Add the analyzer to the slice in `BuildAnalyzers()` in `_tools/customlint/plugin.go`:

```go
func (p *plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
    return []*analysis.Analyzer{
        ruleStructAnalyzer,
        lspLiteralAnalyzer,
        docURLAnalyzer,
        <name>Analyzer,  // <-- add here
    }, nil
}
```

### 6. Update README.md

Add a section under `## Implemented Rules` in `_tools/customlint/README.md`:

```markdown
### <name>

<One-paragraph description of what it checks and why.>

Detects:

- <pattern 1>
- <pattern 2>

**Test**: `go test ./...` (all tests pass)
```

### 7. Verify

Run these commands sequentially:

```bash
# Targeted test
cd _tools && go test ./customlint/... -run Test<Name> -v

# All analyzer tests still pass
cd _tools && go test ./customlint/...

# No false positives on the real codebase
make lint
```

All three must pass before the analyzer is complete.

## Common AST patterns

| What you want to detect | Node type | Key check |
|---|---|---|
| Struct field `Foo: "bar"` | `*ast.KeyValueExpr` | `key.(*ast.Ident).Name == "Foo"` and `value.(*ast.BasicLit).Kind == token.STRING` |
| Function call `f(x, "bar")` | `*ast.CallExpr` | `fun.(*ast.Ident).Name == "f"` and `args[N].(*ast.BasicLit)` |
| Any string literal matching pattern | `*ast.BasicLit` | `lit.Kind == token.STRING` then `strconv.Unquote` and match |
| Exported type declaration | `*ast.GenDecl` -> `*ast.TypeSpec` | `ast.IsExported(name)` |
| Switch/case with literal | `*ast.BasicLit` inside `*ast.CaseClause` | Filter `*ast.BasicLit`, check parent context |

## Error message style

Write messages in the form: **"use X instead of Y"** or **"X should have Y"**.

Good:

- `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string %s`
- `use protocol.Method* constant instead of string literal %s for LSP method name`
- `exported rule struct %s should have a documentation comment`

Bad:

- `found hardcoded URL` (doesn't say what to do instead)
- `error: wrong type` (vague)
