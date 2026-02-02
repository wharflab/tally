package hadolint

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

// DL3021Rule implements the DL3021 linting rule.
// COPY with more than 2 arguments requires the last argument to end with /.
type DL3021Rule struct{}

// NewDL3021Rule creates a new DL3021 rule instance.
func NewDL3021Rule() *DL3021Rule {
	return &DL3021Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3021Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3021",
		Name:            "COPY destination must end with /",
		Description:     "COPY with more than 2 arguments requires the last argument to end with /",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3021",
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Check runs the DL3021 rule.
// It warns when a COPY instruction has more than 2 arguments and the
// destination does not end with /.
func (r *DL3021Rule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation
	meta := r.Metadata()

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			copyCmd, ok := cmd.(*instructions.CopyCommand)
			if !ok {
				continue
			}

			// Only check when there are more than 1 source (i.e., more than 2 args total)
			if len(copyCmd.SourcePaths) <= 1 {
				continue
			}

			// Check if destination ends with /
			// Strip quotes if present (handles "dest" case)
			dest := stripQuotes(copyCmd.DestPath)
			if dest != "" && !strings.HasSuffix(dest, "/") {
				loc := rules.NewLocationFromRanges(input.File, copyCmd.Location())
				violations = append(violations, rules.NewViolation(
					loc,
					meta.Code,
					"COPY with more than 2 arguments requires the last argument to end with /",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"When copying multiple source files, the destination must be a directory "+
						"(indicated by a trailing /). Without it, the build will fail.",
				))
			}
		}
	}

	return violations
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3021Rule())
}
