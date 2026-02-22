package syntax

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// validInstructions is a sorted list of Dockerfile instruction keywords.
var validInstructions = func() []string {
	keys := slices.Collect(maps.Keys(command.Commands))
	slices.Sort(keys)
	return keys
}()

// checkUnknownInstructions walks AST children and reports any instruction
// keyword that is not in BuildKit's command.Commands map.
func checkUnknownInstructions(file string, ast *parser.Result) []Error {
	if ast == nil || ast.AST == nil || len(ast.AST.Children) == 0 {
		return nil
	}

	var errs []Error
	for _, node := range ast.AST.Children {
		if node == nil {
			continue
		}
		keyword := strings.ToLower(node.Value)
		if _, ok := command.Commands[keyword]; ok {
			continue
		}

		msg := formatUnknownInstruction(node.Value)
		errs = append(errs, Error{
			File:     file,
			Message:  msg,
			Line:     node.StartLine,
			RuleCode: "tally/unknown-instruction",
		})
	}
	return errs
}

func formatUnknownInstruction(keyword string) string {
	suggestion := closestMatch(strings.ToLower(keyword), validInstructions, 2)
	if suggestion != "" {
		return fmt.Sprintf("unknown instruction %q (did you mean %q?)", strings.ToUpper(keyword), strings.ToUpper(suggestion))
	}
	return fmt.Sprintf("unknown instruction %q", strings.ToUpper(keyword))
}
