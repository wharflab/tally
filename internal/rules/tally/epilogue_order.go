package tally

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// EpilogueOrderRuleCode is the full rule code for the epilogue-order rule.
const EpilogueOrderRuleCode = rules.TallyRulePrefix + "epilogue-order"

// epilogueInstructions defines the canonical order for epilogue instructions.
// Instructions at the end of an output stage should appear in this order.
var epilogueInstructions = []string{"stopsignal", "healthcheck", "entrypoint", "cmd"}

// epilogueOrderRank maps instruction name (lowercase) to its canonical position.
var epilogueOrderRank = func() map[string]int {
	m := make(map[string]int, len(epilogueInstructions))
	for i, name := range epilogueInstructions {
		m[name] = i
	}
	return m
}()

// EpilogueOrderRule implements the epilogue-order linting rule.
// It checks that runtime-configuration instructions (STOPSIGNAL, HEALTHCHECK,
// ENTRYPOINT, CMD) appear at the end of each output stage in canonical order.
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
		if _, ok := epilogueOrderRank[name]; ok {
			epilogues = append(epilogues, epilogueCmd{name: name, index: i})
		}
	}

	if len(epilogues) == 0 {
		return rules.Violation{}, false
	}

	// Check two conditions:
	// 1. Position: all epilogue instructions must be at the end (no non-epilogue after first epilogue)
	// 2. Order: epilogue instructions must appear in canonical order
	positionOK := r.checkPosition(stage, epilogues)
	orderOK := r.checkOrder(epilogues)

	if positionOK && orderOK {
		return rules.Violation{}, false
	}

	// Find the first misplaced instruction for the violation location.
	firstBad := epilogues[0]
	if !positionOK {
		firstBad = r.firstMisplacedByPosition(stage, epilogues)
	} else {
		firstBad = r.firstMisplacedByOrder(epilogues)
	}

	cmd := stage.Commands[firstBad.index]
	loc := rules.NewLocationFromRanges(file, cmd.Location())

	// Check for duplicate epilogue types — if present, report violation but skip fix.
	hasDuplicates := r.hasDuplicateEpilogueTypes(epilogues)

	v := rules.NewViolation(loc, meta.Code,
		"epilogue instructions should appear at the end of the stage in order: STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD",
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

// checkPosition returns true if all epilogue instructions are at the end of the stage.
// No non-epilogue instruction should appear after the first epilogue instruction.
func (r *EpilogueOrderRule) checkPosition(stage instructions.Stage, epilogues []epilogueCmd) bool {
	if len(epilogues) == 0 {
		return true
	}

	firstEpilogueIdx := epilogues[0].index
	totalCmds := len(stage.Commands)

	// Every instruction from firstEpilogueIdx to end must be an epilogue or ONBUILD.
	for i := firstEpilogueIdx; i < totalCmds; i++ {
		cmd := stage.Commands[i]
		if _, isOnbuild := cmd.(*instructions.OnbuildCommand); isOnbuild {
			continue
		}
		name := strings.ToLower(cmd.Name())
		if _, ok := epilogueOrderRank[name]; !ok {
			return false
		}
	}

	return true
}

// checkOrder returns true if epilogue instructions are in canonical order.
func (r *EpilogueOrderRule) checkOrder(epilogues []epilogueCmd) bool {
	prevRank := -1
	for _, ep := range epilogues {
		rank := epilogueOrderRank[ep.name]
		if rank < prevRank {
			return false
		}
		prevRank = rank
	}
	return true
}

// firstMisplacedByPosition returns the first epilogue instruction that has
// a non-epilogue instruction after it.
func (r *EpilogueOrderRule) firstMisplacedByPosition(stage instructions.Stage, epilogues []epilogueCmd) epilogueCmd {
	for _, ep := range epilogues {
		// Check if there's any non-epilogue instruction after this one.
		for j := ep.index + 1; j < len(stage.Commands); j++ {
			cmd := stage.Commands[j]
			if _, isOnbuild := cmd.(*instructions.OnbuildCommand); isOnbuild {
				continue
			}
			name := strings.ToLower(cmd.Name())
			if _, ok := epilogueOrderRank[name]; !ok {
				return ep
			}
		}
	}
	// Fallback (shouldn't reach here if checkPosition failed).
	return epilogues[0]
}

// firstMisplacedByOrder returns the first epilogue instruction that breaks canonical order.
func (r *EpilogueOrderRule) firstMisplacedByOrder(epilogues []epilogueCmd) epilogueCmd {
	prevRank := -1
	for _, ep := range epilogues {
		rank := epilogueOrderRank[ep.name]
		if rank < prevRank {
			return ep
		}
		prevRank = rank
	}
	return epilogues[0]
}

// hasDuplicateEpilogueTypes checks if any epilogue instruction type appears more than once.
func (r *EpilogueOrderRule) hasDuplicateEpilogueTypes(epilogues []epilogueCmd) bool {
	seen := make(map[string]bool, len(epilogues))
	for _, ep := range epilogues {
		if seen[ep.name] {
			return true
		}
		seen[ep.name] = true
	}
	return false
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewEpilogueOrderRule())
}
