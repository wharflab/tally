package hadolint

// DL3024: FROM stage names must be unique.
//
// Each stage in a multi-stage Dockerfile must have a unique name when using
// the AS clause. Duplicate stage names are invalid and will cause build failures
// or unexpected behavior. Stage name comparison is case-insensitive.
//
// IMPLEMENTATION: This rule is detected during semantic analysis in
// internal/semantic/builder.go when processing stage naming. The semantic builder
// maintains a map of normalized stage names (lowercased) and their indices,
// reporting violations when a duplicate is encountered.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3024

const (
	DL3024Code   = "hadolint/DL3024"
	DL3024DocURL = "https://github.com/hadolint/hadolint/wiki/DL3024"
)
