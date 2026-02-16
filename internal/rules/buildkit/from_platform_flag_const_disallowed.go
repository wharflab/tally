package buildkit

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/wharflab/tally/internal/rules"
)

// FromPlatformFlagConstDisallowedRule implements BuildKit's FromPlatformFlagConstDisallowed check.
//
// It detects FROM instructions where --platform is set to a constant value (e.g., linux/amd64)
// instead of using a build argument (e.g., $BUILDPLATFORM). Hardcoded platform values prevent
// multi-platform builds and reduce portability.
//
// An exception is made when the stage name references the platform's OS or architecture
// (e.g., "FROM --platform=linux/amd64 scratch AS build_amd64"), as this indicates intentional
// per-platform stages in a multi-platform build.
//
// Original source:
// https://github.com/moby/buildkit/blob/master/frontend/dockerfile/dockerfile2llb/convert.go
type FromPlatformFlagConstDisallowedRule struct{}

func NewFromPlatformFlagConstDisallowedRule() *FromPlatformFlagConstDisallowedRule {
	return &FromPlatformFlagConstDisallowedRule{}
}

func (r *FromPlatformFlagConstDisallowedRule) Metadata() rules.RuleMetadata {
	const name = "FromPlatformFlagConstDisallowed"
	return *GetMetadata(name)
}

// Check runs the FromPlatformFlagConstDisallowed rule.
// It examines FROM instructions for hardcoded --platform values that prevent
// multi-platform builds.
func (r *FromPlatformFlagConstDisallowedRule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation
	meta := r.Metadata()

	for _, stage := range input.Stages {
		platform := stage.Platform
		if platform == "" {
			continue
		}

		// If the platform contains variable references, it's not a constant.
		// BuildKit checks this via ProcessWordWithMatches; we check for $ in
		// the raw string since tally works with unparsed platform values.
		if strings.Contains(platform, "$") {
			continue
		}

		// Parse the platform into OS/architecture components.
		// Skip if the platform doesn't look like a valid platform spec.
		os, arch, ok := parsePlatformParts(platform)
		if !ok {
			continue
		}

		// If the stage name references the platform's OS or architecture,
		// it indicates intentional per-platform staging (e.g., "build_amd64").
		// This matches BuildKit's exception logic.
		// Guard against empty strings: strings.Contains(s, "") is always true.
		if stage.Name != "" {
			if (os != "" && strings.Contains(stage.Name, os)) ||
				(arch != "" && strings.Contains(stage.Name, arch)) {
				continue
			}
		}

		loc := rules.NewLocationFromRanges(input.File, stage.Location)
		msg := linter.RuleFromPlatformFlagConstDisallowed.Format(platform)
		violations = append(violations, rules.NewViolation(
			loc, meta.Code, msg, meta.DefaultSeverity,
		).WithDocURL(meta.DocURL))
	}

	return violations
}

// parsePlatformParts splits a platform string into OS and architecture.
//
// The OCI platform format is "os/arch[/variant]". Single-component values
// (e.g., "linux") are also accepted as valid platform specs — BuildKit's
// platforms.Parse() normalizes them using runtime defaults.
//
// Examples:
//   - "linux/amd64"     → ("linux", "amd64", true)
//   - "linux/arm/v7"    → ("linux", "arm", true)
//   - "windows/amd64"   → ("windows", "amd64", true)
//   - "linux"           → ("linux", "", true)
//   - ""                → ("", "", false)
func parsePlatformParts(platform string) (string, string, bool) {
	if platform == "" {
		return "", "", false
	}
	parts := strings.Split(platform, "/")
	switch {
	case len(parts) == 1:
		// Single component (e.g., "linux" or "amd64").
		return parts[0], "", true
	case parts[0] == "" || parts[1] == "":
		// Malformed (e.g., "/amd64" or "linux/").
		return "", "", false
	default:
		// Two or more components: os/arch[/variant].
		return parts[0], parts[1], true
	}
}

func init() {
	rules.Register(NewFromPlatformFlagConstDisallowedRule())
}
