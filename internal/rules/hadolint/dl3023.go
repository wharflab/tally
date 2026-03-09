package hadolint

import "github.com/wharflab/tally/internal/rules"

// DL3023: COPY --from should not reference the stage's own FROM alias.
//
// A COPY instruction with --from flag cannot reference the same stage it's in,
// as this creates a self-referential dependency that is invalid. The --from flag
// should reference a previous stage, an external image, or a numeric stage index.
//
// IMPLEMENTATION: This rule is detected during semantic analysis in
// internal/semantic/builder.go when processing COPY instructions. The semantic
// builder maintains a map of stage names and checks if COPY --from references
// the current stage being built.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3023

const DL3023Code = "hadolint/DL3023"

var DL3023DocURL = rules.HadolintDocURL("DL3023")

// DL3023Rule registers the rule so it appears in rules.All() with proper metadata.
// The actual detection runs during semantic model construction in builder.go.
type DL3023Rule struct{}

func (r *DL3023Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            DL3023Code,
		Name:            "COPY --from cannot reference its own FROM alias",
		Description:     "`COPY --from` cannot reference its own `FROM` alias",
		DocURL:          DL3023DocURL,
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

func (r *DL3023Rule) Check(rules.LintInput) []rules.Violation {
	return nil // Detected during semantic model construction.
}

func init() {
	rules.Register(&DL3023Rule{})
}
