package buildkit

import "github.com/tinovyatkin/tally/internal/rules"

// MultipleInstructionsDisallowedRule registers BuildKit's MultipleInstructionsDisallowed check.
//
// IMPLEMENTATION: The actual detection runs during semantic model construction in
// internal/semantic/builder.go, which has access to per-stage instruction lists.
// This file exists so the rule appears in rules.All() for config, docs, and
// the sync-buildkit-rules script.
type MultipleInstructionsDisallowedRule struct{}

func NewMultipleInstructionsDisallowedRule() *MultipleInstructionsDisallowedRule {
	return &MultipleInstructionsDisallowedRule{}
}

func (r *MultipleInstructionsDisallowedRule) Metadata() rules.RuleMetadata {
	return *GetMetadata("MultipleInstructionsDisallowed")
}

func (r *MultipleInstructionsDisallowedRule) Check(rules.LintInput) []rules.Violation {
	return nil // Detected during semantic model construction.
}

func init() {
	rules.Register(NewMultipleInstructionsDisallowedRule())
}
