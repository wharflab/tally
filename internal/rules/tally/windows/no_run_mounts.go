package windows

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/runmount"
)

// NoRunMountsRuleCode is the full rule code for tally/windows/no-run-mounts.
const NoRunMountsRuleCode = rules.TallyRulePrefix + "windows/no-run-mounts"

// NoRunMountsRule flags RUN --mount usage in Windows stages.
// All mount types (cache, secret, ssh, bind, tmpfs) fail at runtime on Windows
// containers because the containerd/HCS layer does not support them.
type NoRunMountsRule struct{}

// NewNoRunMountsRule creates a new rule instance.
func NewNoRunMountsRule() *NoRunMountsRule { return &NoRunMountsRule{} }

// Metadata returns the rule metadata.
func (r *NoRunMountsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoRunMountsRuleCode,
		Name:            "No RUN --mount on Windows",
		Description:     "RUN --mount flags are not supported on Windows containers and will fail at runtime",
		DocURL:          rules.TallyDocURL(NoRunMountsRuleCode),
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

// Check runs the rule against the given input.
func (r *NoRunMountsRule) Check(input rules.LintInput) []rules.Violation {
	stages := windowsStages(input)
	if len(stages) == 0 {
		return nil
	}

	meta := r.Metadata()
	var violations []rules.Violation

	for _, info := range stages {
		if info.Stage == nil {
			continue
		}
		for _, cmd := range info.Stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			mounts := runmount.GetMounts(run)
			if len(mounts) == 0 {
				continue
			}

			loc := rules.NewLocationFromRanges(input.File, run.Location())
			if loc.IsFileLevel() {
				continue
			}

			types := collectMountTypes(mounts)
			message := "RUN --mount=type=" + types + " is not supported on Windows containers"
			detail := "BuildKit accepts --mount flags without error, but the build will fail at the containerd/HCS runtime layer. " +
				"All mount types (cache, secret, ssh, bind, tmpfs) are unsupported on Windows."

			v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithDetail(detail)
			v.StageIndex = info.Index
			violations = append(violations, v)
		}
	}

	return violations
}

// collectMountTypes returns a comma-separated list of mount types found.
func collectMountTypes(mounts []*instructions.Mount) string {
	if len(mounts) == 1 {
		return string(mounts[0].Type)
	}
	seen := make(map[string]struct{}, len(mounts))
	types := make([]string, 0, len(mounts))
	for _, m := range mounts {
		t := string(m.Type)
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		types = append(types, t)
	}
	return strings.Join(types, ",")
}

func init() {
	rules.Register(NewNoRunMountsRule())
}
