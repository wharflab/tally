package customlint

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var lspLiteralAnalyzer = &analysis.Analyzer{
	Name: "lspliteral",
	Doc:  "checks that LSP method names use protocol.Method* constants instead of string literals",
	Run:  runLSPLiteral,
	Requires: []*analysis.Analyzer{
		inspect.Analyzer,
	},
}

// lspMethodPrefixes are prefixes that identify LSP method name strings.
var lspMethodPrefixes = []string{
	"textDocument/",
	"workspace/",
	"window/",
	"callHierarchy/",
	"typeHierarchy/",
	"codeAction/",
	"codeLens/",
	"completionItem/",
	"documentLink/",
	"inlayHint/",
	"notebookDocument/",
	"workspaceSymbol/",
	"$/",
}

// lspMethodExact are exact LSP method names without a slash prefix.
var lspMethodExact = map[string]bool{
	"initialize":  true,
	"initialized": true,
	"shutdown":    true,
	"exit":        true,
}

func runLSPLiteral(pass *analysis.Pass) (any, error) {
	// Only check files in internal/lspserver.
	if !strings.Contains(pass.Pkg.Path(), "internal/lspserver") {
		return nil, nil
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.BasicLit)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}

		// Strip quotes to get the raw string value.
		val := lit.Value
		if len(val) < 2 {
			return
		}
		inner := val[1 : len(val)-1]

		if !isLSPMethod(inner) {
			return
		}

		pass.Reportf(
			lit.Pos(),
			"use protocol.Method* constant instead of string literal %s for LSP method name",
			val,
		)
	})

	return nil, nil
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
