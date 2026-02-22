package customlint

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var docURLAnalyzer = &analysis.Analyzer{
	Name: "docurl",
	Doc:  "checks that DocURL fields use rules.*DocURL() helpers instead of hardcoded strings",
	Run:  runDocURL,
	Requires: []*analysis.Analyzer{
		inspect.Analyzer,
	},
}

func runDocURL(pass *analysis.Pass) (any, error) {
	// Only check files in internal/rules/ or internal/semantic/.
	pkg := pass.Pkg.Path()
	if !strings.Contains(pkg, "internal/rules") && !strings.Contains(pkg, "internal/semantic") {
		return nil, nil
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.KeyValueExpr)(nil),
		(*ast.CallExpr)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		switch node := n.(type) {
		case *ast.KeyValueExpr:
			checkDocURLField(pass, node)
		case *ast.CallExpr:
			checkNewIssueCall(pass, node)
		}
	})

	return nil, nil
}

// checkDocURLField reports if a struct literal has DocURL: "..." with a string literal.
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

// checkNewIssueCall reports if a newIssue(...) call has a string literal as its 5th argument (docURL).
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
