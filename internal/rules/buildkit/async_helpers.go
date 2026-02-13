package buildkit

import "github.com/tinovyatkin/tally/internal/rules/asyncutil"

// planExternalImageChecks delegates to asyncutil.PlanExternalImageChecks.
var planExternalImageChecks = asyncutil.PlanExternalImageChecks //nolint:gochecknoglobals // package-local alias
