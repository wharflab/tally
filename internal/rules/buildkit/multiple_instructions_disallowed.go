package buildkit

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
)

// MultipleInstructionsDisallowedRule checks for duplicate CMD, ENTRYPOINT, and
// HEALTHCHECK instructions within the same stage, matching BuildKit semantics.
type MultipleInstructionsDisallowedRule struct{}

func NewMultipleInstructionsDisallowedRule() *MultipleInstructionsDisallowedRule {
	return &MultipleInstructionsDisallowedRule{}
}

func (r *MultipleInstructionsDisallowedRule) Metadata() rules.RuleMetadata {
	return *GetMetadata("MultipleInstructionsDisallowed")
}

func (r *MultipleInstructionsDisallowedRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		var lastCmdLoc, lastEntrypointLoc, lastHealthcheckLoc *parser.Range

		for _, cmd := range stage.Commands {
			var prev **parser.Range
			switch cmd.(type) {
			case *instructions.CmdCommand:
				prev = &lastCmdLoc
			case *instructions.EntrypointCommand:
				prev = &lastEntrypointLoc
			case *instructions.HealthCheckCommand:
				prev = &lastHealthcheckLoc
			default:
				continue
			}

			v, next := duplicateInstructionViolation(input.File, meta, *prev, cmd)
			*prev = next
			if v == nil {
				continue
			}
			v.StageIndex = stageIdx
			violations = append(violations, *v)
		}
	}

	return violations
}

func duplicateInstructionViolation(
	file string,
	meta rules.RuleMetadata,
	prevLoc *parser.Range,
	cmd instructions.Command,
) (*rules.Violation, *parser.Range) {
	next := prevLoc
	if ranges := cmd.Location(); len(ranges) > 0 {
		loc := ranges[0]
		next = &loc
	}

	if prevLoc == nil {
		return nil, next
	}

	v := rules.NewViolation(
		rules.NewLocationFromRange(file, *prevLoc),
		meta.Code,
		"Multiple "+cmd.Name()+" instructions should not be used in the same stage because only the last one will be used",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL)

	return &v, next
}

func init() {
	rules.Register(NewMultipleInstructionsDisallowedRule())
}
