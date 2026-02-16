// Package all imports all rule packages to register them.
// Import this package with a blank identifier to enable all rules:
//
//	import _ "github.com/wharflab/tally/internal/rules/all"
package all

import (
	// Import all rule packages to trigger their init() registration
	_ "github.com/wharflab/tally/internal/rules/buildkit"
	_ "github.com/wharflab/tally/internal/rules/hadolint"
	_ "github.com/wharflab/tally/internal/rules/tally"
)
