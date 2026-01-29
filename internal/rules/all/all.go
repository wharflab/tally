// Package all imports all rule packages to register them.
// Import this package with a blank identifier to enable all rules:
//
//	import _ "github.com/tinovyatkin/tally/internal/rules/all"
package all

import (
	// Import all rule packages to trigger their init() registration
	_ "github.com/tinovyatkin/tally/internal/rules/copyignoredfile"
	_ "github.com/tinovyatkin/tally/internal/rules/maxlines"
	_ "github.com/tinovyatkin/tally/internal/rules/nounreachablestages"
	_ "github.com/tinovyatkin/tally/internal/rules/redundanttargetplatform"
	_ "github.com/tinovyatkin/tally/internal/rules/secretsinargorenv"
	_ "github.com/tinovyatkin/tally/internal/rules/secretsincode"
	_ "github.com/tinovyatkin/tally/internal/rules/trustedbaseimage"
	_ "github.com/tinovyatkin/tally/internal/rules/workdirrelativepath"
)
