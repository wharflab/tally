package autofix

import (
	"github.com/tinovyatkin/tally/internal/ai/autofixdata"
	"github.com/tinovyatkin/tally/internal/fix"
)

// Register registers the AI AutoFix resolver.
//
// This is intentionally explicit (no init side effects). Callers should invoke
// Register only when fixes are being applied and at least one effective config
// has ai.enabled=true.
func Register() {
	if fix.GetResolver(autofixdata.ResolverID) != nil {
		return
	}
	fix.RegisterResolver(newResolver())
}
