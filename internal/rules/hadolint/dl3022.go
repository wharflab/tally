package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
)

// DL3022: COPY --from should reference a previously defined FROM alias.
//
// The COPY --from flag should reference either a named stage alias defined
// in a previous FROM instruction, a valid numeric stage index, or an external
// image (containing ":""). Using an undefined reference is likely a typo or
// indicates a stage that was removed or renamed.
//
// Default severity is Off because this rule cannot account for --build-context
// sources, which are valid COPY --from targets supplied at build time.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3022

const DL3022Code = "hadolint/DL3022"

var DL3022DocURL = rules.HadolintDocURL("DL3022")

// DL3022Rule checks that COPY --from references a previously defined stage.
type DL3022Rule struct{}

func NewDL3022Rule() *DL3022Rule {
	return &DL3022Rule{}
}

func (r *DL3022Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            DL3022Code,
		Name:            "COPY --from should reference a previously defined FROM alias",
		Description:     "`COPY --from` should reference a previously defined `FROM` alias",
		DocURL:          DL3022DocURL,
		DefaultSeverity: rules.SeverityOff,
		Category:        "correctness",
	}
}

func (r *DL3022Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	definedStageNames := make(map[string]struct{}, len(input.Stages))
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		currentStageName := normalizeStageRef(stage.Name)

		for _, cmd := range stage.Commands {
			copyCmd, ok := cmd.(*instructions.CopyCommand)
			if !ok || copyCmd.From == "" {
				continue
			}
			if currentStageName != "" && normalizeStageRef(copyCmd.From) == currentStageName {
				continue
			}
			if isExternalCopySource(copyCmd.From) || isPreviouslyDefinedStageRef(copyCmd.From, stageIdx, definedStageNames) {
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

		if stage.Name != "" {
			definedStageNames[normalizeStageRef(stage.Name)] = struct{}{}
		}
	}

	return violations
}

func init() {
	rules.Register(NewDL3022Rule())
}
