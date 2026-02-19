package tally

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// EpilogueOrderRuleCode is the full rule code for the epilogue-order rule.
const EpilogueOrderRuleCode = rules.TallyRulePrefix + "epilogue-order"

// EpilogueOrderRule implements the epilogue-order linting rule.
// It checks that runtime-configuration instructions (STOPSIGNAL, HEALTHCHECK,
// ENTRYPOINT, CMD) appear at the end of each output stage in canonical order.
//
// Cross-rule interactions:
//   - MultipleInstructionsDisallowed: removes duplicate CMD/ENTRYPOINT/HEALTHCHECK
//     before this rule runs. checkStage skips the fix when duplicates remain.
//   - DL3057 (HEALTHCHECK presence): may flag the same Dockerfile; no conflict
//     since epilogue-order preserves instruction presence and only reorders.
//   - JSONArgsRecommended: converts CMD/ENTRYPOINT to exec form (sync, priority 0);
//     epilogue-order reorders independently of instruction form.
//   - ONBUILD variants are intentionally skipped (not treated as epilogue).
//   - newline-between-instructions (async, priority 200): runs after this rule's
//     fix (NeedsResolve, FixPriority 175) and normalizes blank lines.
type EpilogueOrderRule struct{}

// NewEpilogueOrderRule creates a new epilogue-order rule instance.
func NewEpilogueOrderRule() *EpilogueOrderRule {
	return &EpilogueOrderRule{}
}

// Metadata returns the rule metadata.
func (r *EpilogueOrderRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code: EpilogueOrderRuleCode,
		Name: "Epilogue Order",
		Description: "Runtime-configuration instructions should appear at the end of " +
			"each output stage in canonical order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD",
		DocURL:          rules.TallyDocURL(EpilogueOrderRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		FixPriority:     175,
	}
}

// Check runs the epilogue-order rule.
func (r *EpilogueOrderRule) Check(input rules.LintInput) []rules.Violation {
	// Semantic model is required for stage graph analysis.
	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Safe assertion with nil fallback
	if sem == nil {
		return nil
	}

	graph := sem.Graph()
	if graph == nil {
		return nil
	}

	meta := r.Metadata()
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		info := sem.StageInfo(stageIdx)
		if info == nil {
			continue
		}

		// Only check applicable stages: final stage or stages with no dependents.
		if !info.IsLastStage && len(graph.DirectDependents(stageIdx)) > 0 {
			continue
		}

		if v, ok := r.checkStage(input.File, stage, meta); ok {
			violations = append(violations, v)
		}
	}

	return violations
}

// epilogueCmd holds a detected epilogue instruction and its position within the stage.
type epilogueCmd struct {
	name  string // lowercase instruction name
	index int    // position within stage.Commands
}

// checkStage examines a single stage for epilogue ordering issues.
func (r *EpilogueOrderRule) checkStage(
	file string,
	stage instructions.Stage,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	// Collect epilogue instructions, skipping ONBUILD variants.
	var epilogues []epilogueCmd
	for i, cmd := range stage.Commands {
		if _, isOnbuild := cmd.(*instructions.OnbuildCommand); isOnbuild {
			continue
		}
		name := strings.ToLower(cmd.Name())
		if rules.IsEpilogueInstruction(name) {
			epilogues = append(epilogues, epilogueCmd{name: name, index: i})
		}
	}

	if len(epilogues) == 0 {
		return rules.Violation{}, false
	}

	// Use shared check for combined position + order validation.
	if rules.CheckEpilogueOrder(stage.Commands) {
		return rules.Violation{}, false
	}

	// Find the first misplaced instruction for the violation location.
	firstBad := r.firstMisplaced(stage, epilogues)

	cmd := stage.Commands[firstBad.index]
	loc := rules.NewLocationFromRanges(file, cmd.Location())

	// Check for duplicate epilogue types — if present, report violation but skip fix.
	names := make([]string, len(epilogues))
	for i, ep := range epilogues {
		names[i] = ep.name
	}
	hasDuplicates := rules.HasDuplicateEpilogueNames(names)

	v := rules.NewViolation(loc, meta.Code,
		"epilogue instructions should appear at the end of the stage in order: "+
			"STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL)

	if !hasDuplicates {
		v = v.WithSuggestedFix(&rules.SuggestedFix{
			Description:  "Reorder epilogue instructions",
			Safety:       rules.FixSafe,
			Priority:     meta.FixPriority,
			NeedsResolve: true,
			ResolverID:   rules.EpilogueOrderResolverID,
			ResolverData: &rules.EpilogueOrderResolveData{},
			IsPreferred:  true,
		})
	}

	return v, true
}

// firstMisplaced returns the first epilogue instruction that violates position or order constraints.
// It checks position violations first (non-epilogue after epilogue), then order violations.
func (r *EpilogueOrderRule) firstMisplaced(
	stage instructions.Stage,
	epilogues []epilogueCmd,
) epilogueCmd {
	// Check position: find first epilogue with a non-epilogue instruction after it.
	for _, ep := range epilogues {
		for j := ep.index + 1; j < len(stage.Commands); j++ {
			cmd := stage.Commands[j]
			if _, isOnbuild := cmd.(*instructions.OnbuildCommand); isOnbuild {
				continue
			}
			name := strings.ToLower(cmd.Name())
			if !rules.IsEpilogueInstruction(name) {
				return ep // Position violation
			}
		}
	}

	// No position issue — must be an order violation.
	prevRank := -1
	for _, ep := range epilogues {
		rank := rules.EpilogueOrderRank[ep.name]
		if rank < prevRank {
			return ep
		}
		prevRank = rank
	}

	return epilogues[0] // Fallback
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewEpilogueOrderRule())
}
