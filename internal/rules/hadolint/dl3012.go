package hadolint

// DL3012: Multiple HEALTHCHECK instructions found.
//
// Only one HEALTHCHECK instruction should exist per stage. Multiple HEALTHCHECK
// instructions in the same stage will cause only the last one to take effect,
// which is likely unintentional.
//
// IMPLEMENTATION: This rule is detected during semantic analysis in
// internal/semantic/builder.go when processing instructions within each stage.
// The semantic builder tracks HEALTHCHECK instructions per stage and reports
// violations when duplicates are found.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3012

const (
	DL3012Code    = "hadolint/DL3012"
	DL3012Message = "Multiple HEALTHCHECK instructions found in stage"
	DL3012DocURL  = "https://github.com/hadolint/hadolint/wiki/DL3012"
)
