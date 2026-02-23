package tally

import (
	"fmt"
	"slices"
	"strings"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// CircularStageDepsRuleCode is the full rule code for the circular-stage-deps rule.
const CircularStageDepsRuleCode = rules.TallyRulePrefix + "circular-stage-deps"

// CircularStageDepsRule detects circular dependencies between build stages.
// A cycle occurs when stages form mutual dependencies through FROM <stage>,
// COPY --from=<stage>, or RUN --mount from=<stage> references.
type CircularStageDepsRule struct{}

// NewCircularStageDepsRule creates a new circular-stage-deps rule instance.
func NewCircularStageDepsRule() *CircularStageDepsRule {
	return &CircularStageDepsRule{}
}

// Metadata returns the rule metadata.
func (r *CircularStageDepsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            CircularStageDepsRuleCode,
		Name:            "Circular Stage Dependencies",
		Description:     "Detects circular dependencies between build stages",
		DocURL:          rules.TallyDocURL(CircularStageDepsRuleCode),
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

// Check runs the circular-stage-deps rule.
func (r *CircularStageDepsRule) Check(input rules.LintInput) []rules.Violation {
	if input.Semantic == nil {
		return nil
	}

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	if sem.StageCount() < 2 {
		return nil
	}

	graph := sem.Graph()
	if graph == nil {
		return nil
	}

	cycles := graph.DetectCycles()
	if len(cycles) == 0 {
		return nil
	}

	violations := make([]rules.Violation, 0, len(cycles))

	for _, cycle := range cycles {
		message := formatCycleMessage(sem, cycle)

		// Report at the FROM instruction of the lowest-indexed stage.
		minIdx := slices.Min(cycle)

		info := sem.StageInfo(minIdx)
		var loc rules.Location
		if info != nil && info.Stage != nil && len(info.Stage.Location) > 0 {
			loc = rules.NewLocationFromRange(input.File, info.Stage.Location[0])
		} else {
			loc = rules.NewFileLocation(input.File)
		}

		violations = append(violations, rules.NewViolation(
			loc,
			r.Metadata().Code,
			message,
			r.Metadata().DefaultSeverity,
		).WithDocURL(r.Metadata().DocURL).WithDetail(
			"Circular stage dependencies prevent the build from completing. "+
				"Restructure stages so that dependencies flow in one direction",
		))
	}

	return violations
}

// formatCycleMessage formats a cycle as a human-readable arrow chain.
func formatCycleMessage(sem *semantic.Model, cycle []int) string {
	var sb strings.Builder
	sb.WriteString("circular dependency between stages: ")

	for i, idx := range cycle {
		if i > 0 {
			sb.WriteString(" → ")
		}
		sb.WriteString(formatStageName(sem, idx))
	}
	// Close the cycle by repeating the first element.
	sb.WriteString(" → ")
	sb.WriteString(formatStageName(sem, cycle[0]))

	return sb.String()
}

// formatStageName returns a display name for a stage.
func formatStageName(sem *semantic.Model, idx int) string {
	info := sem.StageInfo(idx)
	if info != nil && info.Stage != nil && info.Stage.Name != "" {
		return fmt.Sprintf("%q", info.Stage.Name)
	}
	return fmt.Sprintf("stage %d", idx)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewCircularStageDepsRule())
}
