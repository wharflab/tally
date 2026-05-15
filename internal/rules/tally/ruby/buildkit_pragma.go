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
//
// BuildKit recognizes parser directives as a contiguous run of `# k=v`
// comment lines at the start of the file. A bare `#` (empty comment)
// terminates the directive block, as does the first non-comment line.
// Whitespace around `=` is tolerated to match BuildKit's parser.
func hasBuildKitSyntaxPragma(input rules.LintInput) bool {
	for line := range strings.SplitSeq(string(input.Source), "\n") {
		trimmed := strings.TrimSpace(line)
		// A bare `#` or any non-comment line ends the directive block.
		if trimmed == "#" || !strings.HasPrefix(trimmed, "#") {
			return false
		}
		directive := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		name, value, ok := strings.Cut(directive, "=")
		if !ok || strings.TrimSpace(name) != "syntax" {
			continue
		}
		v := strings.TrimSpace(value)
		for _, m := range dockerfileSyntaxBuildKitMarkers {
			if strings.Contains(v, m) {
				return true
			}
		}
	}
	return false
}
