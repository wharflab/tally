package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
)

// DL3023: COPY --from should not reference the stage's own FROM alias.
//
// A COPY instruction with --from flag cannot reference the same stage it's in,
// as this creates a self-referential dependency that is invalid. The --from flag
// should reference a previous stage, an external image, or a numeric stage index.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3023

const DL3023Code = "hadolint/DL3023"

var DL3023DocURL = rules.HadolintDocURL("DL3023")

// DL3023Rule checks that COPY --from does not reference its own stage alias.
type DL3023Rule struct{}

func NewDL3023Rule() *DL3023Rule {
	return &DL3023Rule{}
}

func (r *DL3023Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            DL3023Code,
		Name:            "COPY --from cannot reference its own FROM alias",
		Description:     "`COPY --from` cannot reference its own `FROM` alias",
		DocURL:          DL3023DocURL,
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

func (r *DL3023Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		stageName := normalizeStageRef(stage.Name)
		if stageName == "" {
			continue
		}

		for _, cmd := range stage.Commands {
			copyCmd, ok := cmd.(*instructions.CopyCommand)
			if !ok || copyCmd.From == "" || normalizeStageRef(copyCmd.From) != stageName {
				continue
			}

			v := rules.NewViolation(
				rules.NewLocationFromRanges(input.File, copyCmd.Location()),
				meta.Code,
				meta.Description,
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL)
			v.StageIndex = stageIdx
			violations = append(violations, v)
		}
	}

	return violations
}

func init() {
	rules.Register(NewDL3023Rule())
}
