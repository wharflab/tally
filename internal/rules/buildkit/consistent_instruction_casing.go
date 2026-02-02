package buildkit

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/linter"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
)

// ConsistentInstructionCasingRule implements the ConsistentInstructionCasing linting rule.
// It checks that all instruction keywords use consistent casing (all UPPER or all lower)
// throughout the Dockerfile.
//
// This mirrors BuildKit's validateCommandCasing logic from dockerfile2llb/convert.go
// but runs during the linting phase rather than during LLB conversion.
type ConsistentInstructionCasingRule struct{}

// NewConsistentInstructionCasingRule creates a new ConsistentInstructionCasing rule instance.
func NewConsistentInstructionCasingRule() *ConsistentInstructionCasingRule {
	return &ConsistentInstructionCasingRule{}
}

// Metadata returns the rule metadata.
func (r *ConsistentInstructionCasingRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.BuildKitRulePrefix + "ConsistentInstructionCasing",
		Name:            "Consistent Instruction Casing",
		Description:     linter.RuleConsistentInstructionCasing.Description,
		DocURL:          linter.RuleConsistentInstructionCasing.URL,
		DefaultSeverity: rules.SeverityWarning,
		Category:        "style",
		IsExperimental:  false,
	}
}

// Check runs the ConsistentInstructionCasing rule.
// It counts upper vs lower case instructions and reports any that don't match the majority.
// When counts are equal, it prefers uppercase (Docker convention).
func (r *ConsistentInstructionCasingRule) Check(input rules.LintInput) []rules.Violation {
	// First pass: count upper vs lower case instructions
	var lowerCount, upperCount int

	// Check MetaArgs (ARG instructions before first FROM)
	for _, arg := range input.MetaArgs {
		cmdName := arg.Name()
		if isSelfConsistentCasing(cmdName) {
			if strings.ToLower(cmdName) == cmdName {
				lowerCount++
			} else {
				upperCount++
			}
		}
	}

	for _, stage := range input.Stages {
		// Check FROM instruction casing
		if isSelfConsistentCasing(stage.OrigCmd) {
			if strings.ToLower(stage.OrigCmd) == stage.OrigCmd {
				lowerCount++
			} else {
				upperCount++
			}
		}

		// Check all commands in the stage
		for _, cmd := range stage.Commands {
			cmdName := cmd.Name()
			if isSelfConsistentCasing(cmdName) {
				if strings.ToLower(cmdName) == cmdName {
					lowerCount++
				} else {
					upperCount++
				}
			}
		}
	}

	// Determine the expected casing based on majority
	// When equal, prefer uppercase (Docker convention)
	isMajorityLower := lowerCount > upperCount

	// Second pass: report violations
	var violations []rules.Violation

	// Check MetaArgs for violations
	for _, arg := range input.MetaArgs {
		if v := r.checkCasing(arg.Name(), isMajorityLower, arg.Location(), input.File); v != nil {
			violations = append(violations, *v)
		}
	}

	for _, stage := range input.Stages {
		if v := r.checkCasing(stage.OrigCmd, isMajorityLower, stage.Location, input.File); v != nil {
			violations = append(violations, *v)
		}

		for _, cmd := range stage.Commands {
			if v := r.checkCasing(cmd.Name(), isMajorityLower, cmd.Location(), input.File); v != nil {
				violations = append(violations, *v)
			}
		}
	}

	return violations
}

// checkCasing checks if an instruction name matches the expected casing.
// Returns a violation if it doesn't match.
func (r *ConsistentInstructionCasingRule) checkCasing(
	name string,
	isMajorityLower bool,
	location []parser.Range,
	file string,
) *rules.Violation {
	var correctCasing string
	if isMajorityLower && strings.ToLower(name) != name {
		correctCasing = "lowercase"
	} else if !isMajorityLower && strings.ToUpper(name) != name {
		correctCasing = "uppercase"
	}

	if correctCasing == "" {
		return nil
	}

	// Create the message using BuildKit's format function
	msg := linter.RuleConsistentInstructionCasing.Format(name, correctCasing)

	loc := rules.NewLocationFromRanges(file, location)
	v := rules.NewViolation(
		loc,
		r.Metadata().Code,
		msg,
		r.Metadata().DefaultSeverity,
	).WithDocURL(r.Metadata().DocURL)

	return &v
}

// isSelfConsistentCasing checks if a string is entirely uppercase or entirely lowercase.
// Mixed-case strings like "From" return false and are not counted.
func isSelfConsistentCasing(s string) bool {
	return s == strings.ToLower(s) || s == strings.ToUpper(s)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewConsistentInstructionCasingRule())
}
