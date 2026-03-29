package hadolint

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"

	"github.com/wharflab/tally/internal/rules"
)

// DL3043: ONBUILD, FROM, or MAINTAINER triggered from within ONBUILD instruction.
//
// The ONBUILD instruction cannot trigger ONBUILD, FROM, or MAINTAINER instructions.
// These meta-instructions are not allowed as ONBUILD triggers because they would
// create invalid or confusing build semantics.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3043

const (
	DL3043Code    = "hadolint/DL3043"
	DL3043Message = "`ONBUILD`, `FROM` or `MAINTAINER` triggered from within `ONBUILD` instruction."
)

var DL3043DocURL = rules.HadolintDocURL("DL3043")

// DL3043Rule checks for forbidden ONBUILD trigger instructions.
type DL3043Rule struct{}

func NewDL3043Rule() *DL3043Rule {
	return &DL3043Rule{}
}

func (r *DL3043Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            DL3043Code,
		Name:            "Forbidden ONBUILD trigger instruction",
		Description:     DL3043Message,
		DocURL:          DL3043DocURL,
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

func (r *DL3043Rule) Check(input rules.LintInput) []rules.Violation {
	if input.AST == nil || input.AST.AST == nil {
		return nil
	}

	meta := r.Metadata()
	var violations []rules.Violation

	for _, node := range topLevelInstructionNodes(input.AST.AST) {
		if node == nil || !strings.EqualFold(node.Value, command.Onbuild) {
			continue
		}

		trigger := onbuildTriggerKeyword(node)
		if trigger == "" {
			continue
		}

		if !strings.EqualFold(trigger, command.Onbuild) &&
			!strings.EqualFold(trigger, command.From) &&
			!strings.EqualFold(trigger, command.Maintainer) {
			continue
		}

		violations = append(violations, rules.NewViolation(
			rules.NewRangeLocation(input.File, node.StartLine, 0, node.StartLine, 0),
			meta.Code,
			meta.Description,
			meta.DefaultSeverity,
		).WithDocURL(meta.DocURL))
	}

	return violations
}

func init() {
	rules.Register(NewDL3043Rule())
}
