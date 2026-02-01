package hadolint

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

const (
	DL3023Code   = "hadolint/DL3023"
	DL3023DocURL = "https://github.com/hadolint/hadolint/wiki/DL3023"
)
