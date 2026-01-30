package customlint

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var ruleStructAnalyzer = &analysis.Analyzer{
	Name: "rulestruct",
	Doc:  "checks that rule structs in internal/rules follow naming conventions",
	Run:  runRuleStruct,
	Requires: []*analysis.Analyzer{
		inspect.Analyzer,
	},
}

func runRuleStruct(pass *analysis.Pass) (any, error) {
	// Only check files in internal/rules/
	if !strings.Contains(pass.Pkg.Path(), "internal/rules") {
		return nil, nil
	}

	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.GenDecl)(nil),
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok {
			return
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			// Check if it's a struct
			_, ok = typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// Only check exported structs that look like rules (end with "Rule")
			name := typeSpec.Name.Name
			if !ast.IsExported(name) || !strings.HasSuffix(name, "Rule") {
				continue
			}

			// Check if there's documentation
			// First check TypeSpec.Doc (individual type comment in grouped declarations)
			// then fall back to GenDecl.Doc (comment before 'type' keyword)
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

	return nil, nil
}
