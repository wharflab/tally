package hadolint

// DL3022: COPY --from should reference a previously defined FROM alias.
//
// The COPY --from flag should reference either a named stage alias defined
// in a previous FROM instruction, a valid numeric stage index, or an external
// image (containing ":""). Using an undefined reference is likely a typo or
// indicates a stage that was removed or renamed.
//
// IMPLEMENTATION: This rule is detected during semantic analysis in
// internal/semantic/builder.go when processing COPY instructions. The semantic
// builder resolves --from references against known stage names and indices,
// and reports a violation when the reference cannot be resolved and doesn't
// look like an external image.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3022

const (
	DL3022Code   = "hadolint/DL3022"
	DL3022DocURL = "https://github.com/hadolint/hadolint/wiki/DL3022"
)
