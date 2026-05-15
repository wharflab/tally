package ruby

import (
	"strings"

	"github.com/wharflab/tally/internal/rules"
)

// dockerfileSyntaxBuildKitMarkers identifies BuildKit-frontend syntax
// pragmas. Cache and bind mounts require one of these.
var dockerfileSyntaxBuildKitMarkers = []string{"docker/dockerfile", "dockerfile/labs"}

// hasBuildKitSyntaxPragma reports whether the Dockerfile carries a
// `# syntax=docker/dockerfile:1` (or compatible) directive at its top.
func hasBuildKitSyntaxPragma(input rules.LintInput) bool {
	for line := range strings.SplitSeq(string(input.Source), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		if strings.Contains(trimmed, "syntax=") {
			for _, m := range dockerfileSyntaxBuildKitMarkers {
				if strings.Contains(trimmed, m) {
					return true
				}
			}
		}
	}
	return false
}
