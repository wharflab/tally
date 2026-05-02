package psanalyzer

import (
	_ "embed"
	"strings"
)

const psscriptAnalyzerVersionEnv = "TALLY_PSSCRIPTANALYZER_VERSION"

// psscriptAnalyzerVersionFile is embedded into the tally binary at build time.
// The source file exists so Renovate and CI can read the same version pin; the
// installed binary does not read it from disk.
//
//go:embed psscriptanalyzer.version
var psscriptAnalyzerVersionFile string

func requiredPSScriptAnalyzerVersion() string {
	return strings.TrimSpace(psscriptAnalyzerVersionFile)
}
