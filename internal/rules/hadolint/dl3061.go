package hadolint

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"

	"github.com/wharflab/tally/internal/rules"
)

// DL3061: Invalid instruction order - Dockerfile must begin with FROM, ARG, or comment.
//
// A Dockerfile must start with either a FROM instruction (to specify the base image),
// an ARG instruction (to define build arguments that can be used in FROM), or comments.
// Any other instruction appearing before FROM (except ARG) is invalid.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3061

const (
	DL3061Code    = "hadolint/DL3061"
	DL3061Message = "Dockerfile must begin with FROM or ARG"
)

var DL3061DocURL = rules.HadolintDocURL("DL3061")

// DL3061Rule checks that the Dockerfile starts with FROM or ARG.
type DL3061Rule struct{}

func NewDL3061Rule() *DL3061Rule {
	return &DL3061Rule{}
}

func (r *DL3061Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            DL3061Code,
		Name:            "Invalid instruction order",
		Description:     DL3061Message,
		DocURL:          DL3061DocURL,
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

func (r *DL3061Rule) Check(input rules.LintInput) []rules.Violation {
	if input.AST == nil || input.AST.AST == nil {
		return nil
	}

	meta := r.Metadata()
	for _, node := range topLevelInstructionNodes(input.AST.AST) {
		if node == nil {
			continue
		}
		if strings.EqualFold(node.Value, command.From) {
			return nil
		}
		if strings.EqualFold(node.Value, command.Arg) {
			continue
		}

		return []rules.Violation{
			rules.NewViolation(
				rules.NewRangeLocation(input.File, node.StartLine, 0, node.StartLine, 0),
				meta.Code,
				"Invalid instruction order. Dockerfile must begin with `FROM`, `ARG` or comment.",
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL),
		}
	}

	return nil
}

func init() {
	rules.Register(NewDL3061Rule())
}
