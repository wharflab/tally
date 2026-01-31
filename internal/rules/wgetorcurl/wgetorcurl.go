// Package wgetorcurl implements hadolint DL4001.
// This rule warns when both wget and curl are used in the same Dockerfile,
// as it's better to standardize on one tool to reduce image size and complexity.
package wgetorcurl

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/shell"
)

// Rule implements the DL4001 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL4001",
		Name:            "Either wget or curl but not both",
		Description:     "Either use wget or curl but not both to reduce image size",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL4001",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "maintainability",
		IsExperimental:  false,
	}
}

// Check runs the DL4001 rule.
// It warns when both wget and curl are used in different RUN instructions.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	var wgetLocs []rules.Location
	var curlLocs []rules.Location

	// Get the semantic model for shell variant information
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}

	for stageIdx, stage := range input.Stages {
		// Get the shell variant for this stage from semantic model
		shellVariant := shell.VariantBash // default
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
			}
		}

		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}

			// Get the shell command string, including heredocs
			var cmdBuilder strings.Builder
			cmdBuilder.WriteString(strings.Join(run.CmdLine, " "))
			for _, f := range run.Files {
				cmdBuilder.WriteByte('\n')
				cmdBuilder.WriteString(f.Data)
			}
			cmdStr := cmdBuilder.String()

			loc := rules.NewLocationFromRanges(input.File, run.Location())

			// Use proper shell parsing with the correct variant
			if shell.ContainsCommandWithVariant(cmdStr, "wget", shellVariant) {
				wgetLocs = append(wgetLocs, loc)
			}
			if shell.ContainsCommandWithVariant(cmdStr, "curl", shellVariant) {
				curlLocs = append(curlLocs, loc)
			}
		}
	}

	// Only report if both are used
	if len(wgetLocs) == 0 || len(curlLocs) == 0 {
		return nil
	}

	violations := make([]rules.Violation, 0, len(curlLocs))

	// Report on all curl usages (arbitrary choice - could be wget instead)
	for _, loc := range curlLocs {
		violations = append(violations, rules.NewViolation(
			loc,
			r.Metadata().Code,
			"both wget and curl are used; pick one to reduce image size and complexity",
			r.Metadata().DefaultSeverity,
		).WithDocURL(r.Metadata().DocURL).WithDetail(
			"Using both wget and curl increases image size and maintenance burden. "+
				"Standardize on one tool. curl is generally preferred in containers "+
				"due to better scripting support and broader protocol support.",
		))
	}

	return violations
}

// New creates a new DL4001 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
