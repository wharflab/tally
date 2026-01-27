// Package nounreachablestages implements a rule that detects build stages
// that don't contribute to the final image.
package nounreachablestages

import (
	"fmt"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// Rule implements the no-unreachable-stages linting rule.
// It detects stages that are not reachable from the final stage
// and therefore don't contribute to the final image.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:             "no-unreachable-stages",
		Name:             "No Unreachable Stages",
		Description:      "Disallows build stages that don't contribute to the final image",
		DocURL:           "https://github.com/tinovyatkin/tally/blob/main/docs/rules/no-unreachable-stages.md",
		DefaultSeverity:  rules.SeverityWarning,
		Category:         "best-practices",
		EnabledByDefault: true,
		IsExperimental:   false,
	}
}

// Check runs the no-unreachable-stages rule.
// It uses the semantic model to find stages that are not reachable
// from the final stage through COPY --from or FROM dependencies.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	// Semantic model is required for this rule
	if input.Semantic == nil {
		return nil
	}

	// Type assert to get the semantic model
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	// Need at least 2 stages for any stage to be unreachable
	if sem.StageCount() < 2 {
		return nil
	}

	graph := sem.Graph()
	if graph == nil {
		return nil
	}

	unreachable := graph.UnreachableStages()
	if len(unreachable) == 0 {
		return nil
	}

	var violations []rules.Violation

	for _, stageIdx := range unreachable {
		info := sem.StageInfo(stageIdx)
		if info == nil || info.Stage == nil {
			continue
		}

		stage := info.Stage

		// Build a descriptive message
		var stageName string
		if stage.Name != "" {
			stageName = fmt.Sprintf("stage %q (index %d)", stage.Name, stageIdx)
		} else {
			stageName = fmt.Sprintf("stage %d", stageIdx)
		}

		message := stageName + " is not reachable from the final stage and does not contribute to the final image"

		// Get location from the FROM instruction
		var loc rules.Location
		if len(stage.Location) > 0 {
			loc = rules.NewLocationFromRange(input.File, stage.Location[0])
		} else {
			loc = rules.NewFileLocation(input.File)
		}

		violations = append(violations, rules.NewViolation(
			loc,
			r.Metadata().Code,
			message,
			r.Metadata().DefaultSeverity,
		).WithDocURL(r.Metadata().DocURL).WithDetail(
			"Consider removing this stage or using COPY --from to include its artifacts in the final image",
		))
	}

	return violations
}

// New creates a new no-unreachable-stages rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
