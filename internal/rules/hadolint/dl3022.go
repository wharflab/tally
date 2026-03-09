package hadolint

import "github.com/wharflab/tally/internal/rules"

// DL3022: COPY --from should reference a previously defined FROM alias.
//
// The COPY --from flag should reference either a named stage alias defined
// in a previous FROM instruction, a valid numeric stage index, or an external
// image (containing ":""). Using an undefined reference is likely a typo or
// indicates a stage that was removed or renamed.
//
// Default severity is Off because this rule cannot account for --build-context
// sources, which are valid COPY --from targets supplied at build time.
//
// IMPLEMENTATION: This rule is detected during semantic analysis in
// internal/semantic/builder.go when processing COPY instructions. The semantic
// builder resolves --from references against known stage names and indices,
// and reports a violation when the reference cannot be resolved and doesn't
// look like an external image.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3022

const DL3022Code = "hadolint/DL3022"

var DL3022DocURL = rules.HadolintDocURL("DL3022")

// DL3022Rule registers the rule so it appears in rules.All() with proper metadata.
// The actual detection runs during semantic model construction in builder.go.
type DL3022Rule struct{}

func (r *DL3022Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            DL3022Code,
		Name:            "COPY --from should reference a previously defined FROM alias",
		Description:     "`COPY --from` should reference a previously defined `FROM` alias",
		DocURL:          DL3022DocURL,
		DefaultSeverity: rules.SeverityOff,
		Category:        "correctness",
	}
}

func (r *DL3022Rule) Check(rules.LintInput) []rules.Violation {
	return nil // Detected during semantic model construction.
}

func init() {
	rules.Register(&DL3022Rule{})
}
