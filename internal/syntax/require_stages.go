package syntax

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// checkRequireStages verifies that the Dockerfile contains at least one FROM
// instruction (i.e., at least one build stage). A Dockerfile without any FROM
// always fails with "dockerfile contains no stages to build" at build time.
func checkRequireStages(file string, ast *parser.Result) []Error {
	if ast == nil || ast.AST == nil {
		return nil
	}

	for _, node := range ast.AST.Children {
		if node != nil && strings.EqualFold(node.Value, command.From) {
			return nil // At least one stage exists.
		}
	}

	// No FROM found (or empty instruction list). Report on line 1.
	line := 1
	if len(ast.AST.Children) > 0 {
		if first := ast.AST.Children[0]; first != nil {
			line = first.StartLine
		}
	}

	return []Error{{
		File:     file,
		Message:  `Dockerfile has no stages to build; add a FROM instruction`,
		Line:     line,
		RuleCode: "tally/require-stages",
	}}
}
