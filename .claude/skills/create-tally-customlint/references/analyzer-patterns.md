# Analyzer Patterns Reference

Concrete examples from tally's existing analyzers. Read this file when you need to
see the exact code structure for a new analyzer.

## Table of contents

1. [Pattern A: Struct field value check (docurl)](#pattern-a-struct-field-value-check)
2. [Pattern B: Function call argument check (docurl)](#pattern-b-function-call-argument-check)
3. [Pattern C: String literal matching (lspliteral)](#pattern-c-string-literal-matching)
4. [Pattern D: Type declaration check (rulestruct)](#pattern-d-type-declaration-check)
5. [Test file pattern](#test-file-pattern)
6. [Test fixture patterns](#test-fixture-patterns)
7. [Plugin registration](#plugin-registration)

---

## Pattern A: Struct field value check

Detect `DocURL: "https://..."` in struct composite literals.

```go
// In nodeFilter:
nodeFilter := []ast.Node{
    (*ast.KeyValueExpr)(nil),
}

// Detection helper:
func checkDocURLField(pass *analysis.Pass, kv *ast.KeyValueExpr) {
    ident, ok := kv.Key.(*ast.Ident)
    if !ok || ident.Name != "DocURL" {
        return
    }
    lit, ok := kv.Value.(*ast.BasicLit)
    if !ok || lit.Kind != token.STRING {
        return
    }
    pass.Reportf(
        lit.Pos(),
        "use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string %s",
        lit.Value,
    )
}
```

## Pattern B: Function call argument check

Detect `newIssue(..., "https://...", ...)` where the 5th argument is a string literal.

```go
// In nodeFilter (combined with Pattern A):
nodeFilter := []ast.Node{
    (*ast.KeyValueExpr)(nil),
    (*ast.CallExpr)(nil),
}

// Detection helper:
func checkNewIssueCall(pass *analysis.Pass, call *ast.CallExpr) {
    fn, ok := call.Fun.(*ast.Ident)
    if !ok || fn.Name != "newIssue" {
        return
    }
    if len(call.Args) < 5 {
        return
    }
    lit, ok := call.Args[4].(*ast.BasicLit)
    if !ok || lit.Kind != token.STRING {
        return
    }
    pass.Reportf(
        lit.Pos(),
        "use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string %s",
        lit.Value,
    )
}
```

## Pattern C: String literal matching

Detect any string literal that looks like an LSP method name (by prefix or exact match).

```go
nodeFilter := []ast.Node{
    (*ast.BasicLit)(nil),
}

insp.Preorder(nodeFilter, func(n ast.Node) {
    lit, ok := n.(*ast.BasicLit)
    if !ok || lit.Kind != token.STRING {
        return
    }

    unquoted, err := strconv.Unquote(lit.Value)
    if err != nil {
        return
    }

    if !isLSPMethod(unquoted) {
        return
    }

    pass.Reportf(
        lit.Pos(),
        "use protocol.Method* constant instead of string literal %s for LSP method name",
        lit.Value,
    )
})
```

Matching helper using prefix lists and exact-match sets:

```go
var lspMethodPrefixes = []string{
    "textDocument/",
    "workspace/",
    // ...
}

var lspMethodExact = map[string]bool{
    "initialize":  true,
    "shutdown":    true,
    // ...
}

func isLSPMethod(s string) bool {
    if lspMethodExact[s] {
        return true
    }
    for _, prefix := range lspMethodPrefixes {
        if strings.HasPrefix(s, prefix) {
            return true
        }
    }
    return false
}
```

## Pattern D: Type declaration check

Detect exported `*Rule` structs missing documentation.

```go
nodeFilter := []ast.Node{
    (*ast.GenDecl)(nil),
}

insp.Preorder(nodeFilter, func(n ast.Node) {
    genDecl, ok := n.(*ast.GenDecl)
    if !ok {
        return
    }

    for _, spec := range genDecl.Specs {
        typeSpec, ok := spec.(*ast.TypeSpec)
        if !ok {
            continue
        }
        _, ok = typeSpec.Type.(*ast.StructType)
        if !ok {
            continue
        }

        name := typeSpec.Name.Name
        if !ast.IsExported(name) || !strings.HasSuffix(name, "Rule") {
            continue
        }

        // Check TypeSpec.Doc first, fall back to GenDecl.Doc
        doc := typeSpec.Doc
        if doc == nil || len(doc.List) == 0 {
            doc = genDecl.Doc
        }
        if doc == nil || len(doc.List) == 0 {
            pass.Reportf(
                typeSpec.Pos(),
                "exported rule struct %s should have a documentation comment",
                name,
            )
        }
    }
})
```

---

## Test file pattern

All test files follow the same structure:

```go
package customlint

import (
    "testing"

    "golang.org/x/tools/go/analysis/analysistest"
)

func TestMyAnalyzer(t *testing.T) {
    t.Parallel()
    testdata := analysistest.TestData()
    analysistest.Run(t, testdata, myAnalyzer,
        "internal/rules/myanalyzer",   // matches testdata/src/internal/rules/myanalyzer/
    )
}
```

---

## Test fixture patterns

### Struct field check fixture

File: `testdata/src/internal/rules/docurl/docurl.go`

```go
package docurl

type Metadata struct {
    Code   string
    DocURL string
}

func someDocURL(code string) string { return "https://example.com/" + code }
var someVar = "https://example.com/rule"

// Bad: hardcoded string literal in DocURL field.
var badMeta = Metadata{
    Code:   "TY0001",
    DocURL: "https://example.com/rule", // want `use rules\.TallyDocURL, rules\.BuildKitDocURL, or rules\.HadolintDocURL instead of hardcoded DocURL string "https://example.com/rule"`
}

// Good: function call in DocURL field.
var goodMetaFunc = Metadata{
    Code:   "TY0002",
    DocURL: someDocURL("TY0002"),
}

// Good: variable in DocURL field.
var goodMetaVar = Metadata{
    Code:   "TY0003",
    DocURL: someVar,
}
```

### Function call argument fixture

File: `testdata/src/internal/semantic/docurl/docurl.go`

```go
package docurl

func someDocURL(code string) string { return "https://example.com/" + code }

func newIssue(file string, location int, code, message, docURL string) struct{} {
    return struct{}{}
}

func example() {
    // Bad: hardcoded string literal as 5th argument to newIssue.
    newIssue("Dockerfile", 1, "DL3000", "some message", "https://example.com/DL3000") // want `use rules\.TallyDocURL...`

    // Good: function call as 5th argument to newIssue.
    newIssue("Dockerfile", 1, "DL3001", "some message", someDocURL("DL3001"))
}
```

### Type declaration fixture

File: `testdata/src/internal/rules/rules.go`

```go
package rules

// GoodRule is a properly documented rule.
type GoodRule struct {
    MaxValue int
}

type BadRule struct { // want "exported rule struct BadRule should have a documentation comment"
    Value int
}
```

### `// want` comment syntax

The `// want` comment is a regex matched against the diagnostic message.
Special regex characters must be escaped:

- `\.` for literal dot
- `\*` for literal asterisk
- `\$` for literal dollar sign
- `\"` for literal quote inside the regex

You can use backtick quoting `` `pattern` `` or double-quote quoting `"pattern"`.

---

## Plugin registration

File: `_tools/customlint/plugin.go`

The `BuildAnalyzers()` method returns all analyzers. Add new ones at the end of the slice:

```go
func (p *plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
    return []*analysis.Analyzer{
        ruleStructAnalyzer,
        lspLiteralAnalyzer,
        docURLAnalyzer,
        // newAnalyzer,  <-- add here
    }, nil
}
```

The `GetLoadMode()` returns `register.LoadModeSyntax` — this means analyzers
only have access to syntax (AST), not type information. All detection must
use AST inspection, not type-checker results.
