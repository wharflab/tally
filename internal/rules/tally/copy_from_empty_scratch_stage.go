package tally

import (
	"fmt"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// CopyFromEmptyScratchStageRuleCode is the full rule code.
const CopyFromEmptyScratchStageRuleCode = rules.TallyRulePrefix + "copy-from-empty-scratch-stage"

// CopyFromEmptyScratchStageRule detects COPY --from references to scratch stages
// that have no file-producing instructions (ADD, COPY, or RUN).
// Such COPY instructions are guaranteed to fail because scratch stages
// start with an empty filesystem.
type CopyFromEmptyScratchStageRule struct{}

// NewCopyFromEmptyScratchStageRule creates a new rule instance.
func NewCopyFromEmptyScratchStageRule() *CopyFromEmptyScratchStageRule {
	return &CopyFromEmptyScratchStageRule{}
}

// Metadata returns the rule metadata.
func (r *CopyFromEmptyScratchStageRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            CopyFromEmptyScratchStageRuleCode,
		Name:            "Copy From Empty Scratch Stage",
		Description:     "Detects COPY --from referencing a scratch stage with no file-producing instructions",
		DocURL:          rules.TallyDocURL(CopyFromEmptyScratchStageRuleCode),
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

// Check runs the copy-from-empty-scratch-stage rule.
//
// Cross-rule interaction with shell-run-in-scratch:
// A scratch stage with only a shell-form RUN is not considered empty here
// (hasFileProducingCommands counts any RUN). The shell-run-in-scratch rule
// handles that case. If the user removes the failing RUN, this rule will then
// fire on any COPY --from referencing the now-empty stage.
func (r *CopyFromEmptyScratchStageRule) Check(input rules.LintInput) []rules.Violation {
	if input.Semantic == nil {
		return nil
	}

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	// Identify empty scratch stages (scratch with no ADD, COPY, or RUN).
	emptyScratch := make(map[int]bool)
	for i := range sem.StageCount() {
		info := sem.StageInfo(i)
		if info == nil || !info.IsScratch() {
			continue
		}
		if !hasFileProducingCommands(info.Stage) {
			emptyScratch[i] = true
		}
	}

	if len(emptyScratch) == 0 {
		return nil
	}

	// Find COPY --from references pointing to empty scratch stages.
	var violations []rules.Violation

	for i := range sem.StageCount() {
		info := sem.StageInfo(i)
		if info == nil {
			continue
		}
		for _, ref := range info.CopyFromRefs {
			if !ref.IsStageRef || !emptyScratch[ref.StageIndex] {
				continue
			}

			srcInfo := sem.StageInfo(ref.StageIndex)
			stageName := formatEmptyScratchStageName(srcInfo, ref.StageIndex)

			loc := rules.NewLocationFromRanges(input.File, ref.Location)

			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				fmt.Sprintf("COPY --from references empty scratch %s which has no ADD, COPY, or RUN instructions", stageName),
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL).WithDetail(
				"The scratch stage has no file-producing instructions, so COPY --from will always fail. "+
					"Add content to the source stage or change the --from reference",
			))
		}
	}

	return violations
}

// hasFileProducingCommands returns true if the stage has any ADD, COPY, or RUN commands.
func hasFileProducingCommands(stage *instructions.Stage) bool {
	if stage == nil {
		return false
	}
	for _, cmd := range stage.Commands {
		switch cmd.(type) {
		case *instructions.AddCommand, *instructions.CopyCommand, *instructions.RunCommand:
			return true
		}
	}
	return false
}

// formatEmptyScratchStageName formats a stage reference for error messages.
func formatEmptyScratchStageName(info *semantic.StageInfo, idx int) string {
	if info != nil && info.Stage != nil && info.Stage.Name != "" {
		return fmt.Sprintf("%q (stage %d)", info.Stage.Name, idx)
	}
	return fmt.Sprintf("stage %d", idx)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewCopyFromEmptyScratchStageRule())
}
