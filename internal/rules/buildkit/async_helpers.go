package buildkit

import "github.com/wharflab/tally/internal/rules/asyncutil"

// planExternalImageChecks delegates to asyncutil.PlanExternalImageChecks.
var planExternalImageChecks = asyncutil.PlanExternalImageChecks //nolint:gochecknoglobals // package-local alias
