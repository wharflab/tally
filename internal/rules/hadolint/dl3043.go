package hadolint

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
	DL3043DocURL  = "https://github.com/hadolint/hadolint/wiki/DL3043"
)
