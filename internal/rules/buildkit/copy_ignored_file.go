package buildkit

import (
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
)

// CopyIgnoredFileRule implements the copy-ignored-file linting rule.
type CopyIgnoredFileRule struct{}

// NewCopyIgnoredFileRule creates a new copy-ignored-file rule instance.
func NewCopyIgnoredFileRule() *CopyIgnoredFileRule {
	return &CopyIgnoredFileRule{}
}

// Metadata returns the rule metadata.
func (r *CopyIgnoredFileRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.BuildKitRulePrefix + "CopyIgnoredFile",
		Name:            "COPY/ADD Ignored File",
		Description:     "Detects COPY/ADD sources that would be ignored by .dockerignore",
		DocURL:          rules.BuildKitDocURL("CopyIgnoredFile"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Check runs the copy-ignored-file rule.
// This rule requires a build context to be set.
func (r *CopyIgnoredFileRule) Check(input rules.LintInput) []rules.Violation {
	if !hasPrimaryContextRef(input) || input.Facts == nil {
		return nil
	}

	var violations []rules.Violation
	for _, stageFacts := range input.Facts.Stages() {
		if stageFacts == nil {
			continue
		}
		violations = append(violations, r.checkBuildContextSources(stageFacts.BuildContextSources, input.File)...)
	}
	return violations
}

func hasPrimaryContextRef(input rules.LintInput) bool {
	return input.InvocationContext != nil && input.InvocationContext.ContextRef().Kind != ""
}

func (r *CopyIgnoredFileRule) checkBuildContextSources(
	sources []*facts.BuildContextSource,
	file string,
) []rules.Violation {
	var violations []rules.Violation
	for _, src := range sources {
		if src == nil {
			continue
		}
		if !isSpecificContextSource(src) {
			continue
		}

		loc := rules.NewFileLocation(file)
		if len(src.Location) > 0 {
			loc = rules.NewLocationFromRanges(file, src.Location)
		} else if src.Line > 0 {
			loc = rules.NewLineLocation(file, src.Line)
		}

		if src.AvailabilityErr != nil {
			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				"failed to evaluate build context availability for '"+src.SourcePath+"'",
				rules.SeverityWarning,
			).WithDocURL(r.Metadata().DocURL).WithDetail(src.AvailabilityErr.Error()))
			continue
		}

		if !src.AvailableInContext {
			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				"source '"+src.SourcePath+"' is not available in the build context and will not be copied",
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL).WithDetail(
				"The source '"+src.SourcePath+"' is not present in the resolved build context. "+
					"It may be excluded by .dockerignore or otherwise unavailable to the build."))
		}
	}
	return violations
}

func isSpecificContextSource(src *facts.BuildContextSource) bool {
	path := src.NormalizedSourcePath
	if path == "" {
		path = src.SourcePath
	}
	return path != "" && path != "." && !strings.ContainsAny(path, "*?[")
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewCopyIgnoredFileRule())
}
