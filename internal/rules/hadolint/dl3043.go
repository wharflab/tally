package hadolint

import "github.com/wharflab/tally/internal/rules"

// DL3043: ONBUILD, FROM, or MAINTAINER triggered from within ONBUILD instruction.
//
// The ONBUILD instruction cannot trigger ONBUILD, FROM, or MAINTAINER instructions.
// These meta-instructions are not allowed as ONBUILD triggers because they would
// create invalid or confusing build semantics.
//
// IMPLEMENTATION: This rule is detected during semantic analysis in
// internal/semantic/builder.go by parsing the raw AST. The builder examines
// ONBUILD instructions and checks if their trigger instruction is one of the
// forbidden types (ONBUILD, FROM, MAINTAINER).
//
// See: https://github.com/hadolint/hadolint/wiki/DL3043

const (
	DL3043Code    = "hadolint/DL3043"
	DL3043Message = "`ONBUILD`, `FROM` or `MAINTAINER` triggered from within `ONBUILD` instruction."
)

var DL3043DocURL = rules.HadolintDocURL("DL3043")

// DL3043Rule registers the rule so it appears in rules.All() with proper metadata.
// The actual detection runs during semantic model construction in builder.go.
type DL3043Rule struct{}

func (r *DL3043Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            DL3043Code,
		Name:            "Forbidden ONBUILD trigger instruction",
		Description:     DL3043Message,
		DocURL:          DL3043DocURL,
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

func (r *DL3043Rule) Check(rules.LintInput) []rules.Violation {
	return nil // Detected during semantic model construction.
}

func init() {
	rules.Register(&DL3043Rule{})
}
