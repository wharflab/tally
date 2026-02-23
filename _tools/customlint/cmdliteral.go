package customlint

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var cmdLiteralAnalyzer = &analysis.Analyzer{
	Name: "cmdliteral",
	Doc:  "checks that Dockerfile instruction keywords use command.* constants instead of string literals",
	Run:  runCmdLiteral,
	Requires: []*analysis.Analyzer{
		inspect.Analyzer,
	},
}

// dockerfileCommands maps lowercase Dockerfile instruction names to their
// command.* constant name (from moby/buildkit/frontend/dockerfile/command).
var dockerfileCommands = map[string]string{
	"add":         "command.Add",
	"arg":         "command.Arg",
	"cmd":         "command.Cmd",
	"copy":        "command.Copy",
	"entrypoint":  "command.Entrypoint",
	"env":         "command.Env",
	"expose":      "command.Expose",
	"from":        "command.From",
	"healthcheck": "command.Healthcheck",
	"label":       "command.Label",
	"maintainer":  "command.Maintainer",
	"onbuild":     "command.Onbuild",
	"run":         "command.Run",
	"shell":       "command.Shell",
	"stopsignal":  "command.StopSignal",
	"user":        "command.User",
	"volume":      "command.Volume",
	"workdir":     "command.Workdir",
}

func runCmdLiteral(pass *analysis.Pass) (any, error) {
	// Only check packages under internal/, excluding internal/shell
	// (which deals with shell command analysis, not Dockerfile instructions).
	pkg := pass.Pkg.Path()
	if !strings.Contains(pkg, "internal/") || strings.Contains(pkg, "internal/shell") {
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

		// Allow literals in test files.
		pos := pass.Fset.Position(lit.Pos())
		if strings.HasSuffix(pos.Filename, "_test.go") {
			return
		}

		unquoted, err := strconv.Unquote(lit.Value)
		if err != nil {
			return
		}

		constName, ok := dockerfileCommands[strings.ToLower(unquoted)]
		if !ok {
			return
		}

		pass.Reportf(
			lit.Pos(),
			"use %s constant instead of string literal %s for Dockerfile instruction",
			constName, lit.Value,
		)
	})

	return nil, nil
}
