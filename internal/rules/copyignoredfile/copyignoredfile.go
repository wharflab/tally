// Package copyignoredfile implements the copy-ignored-file rule.
// This rule detects COPY/ADD sources that would be ignored by .dockerignore.
package copyignoredfile

import (
	"path/filepath"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
)

// Rule implements the copy-ignored-file linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:             rules.BuildKitRulePrefix + "CopyIgnoredFile",
		Name:             "COPY/ADD Ignored File",
		Description:      "Detects COPY/ADD sources that would be ignored by .dockerignore",
		DocURL:           "https://docs.docker.com/go/dockerfile/rule/copy-ignored-file/",
		DefaultSeverity:  rules.SeverityWarning,
		Category:         "correctness",
		EnabledByDefault: true,
		IsExperimental:   false,
	}
}

// Check runs the copy-ignored-file rule.
// This rule requires a build context to be set.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	// Skip if no build context is provided
	if input.Context == nil {
		return nil
	}

	// Skip if no .dockerignore
	if !input.Context.HasIgnoreFile() {
		return nil
	}

	var violations []rules.Violation

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			violations = append(violations, r.checkCommand(cmd, input.Context, input.File)...)
		}
	}

	return violations
}

// checkCommand checks a single command for ignored COPY/ADD sources.
func (r *Rule) checkCommand(cmd instructions.Command, ctx rules.BuildContext, file string) []rules.Violation {
	switch c := cmd.(type) {
	case *instructions.CopyCommand:
		return r.checkCopyAdd(c.SourcePaths, c.SourceContents, c.From, c.Location(), ctx, file)
	case *instructions.AddCommand:
		return r.checkCopyAdd(c.SourcePaths, c.SourceContents, "", c.Location(), ctx, file)
	}
	return nil
}

// checkCopyAdd checks COPY/ADD sources against .dockerignore.
func (r *Rule) checkCopyAdd(
	sourcePaths []string,
	sourceContents []instructions.SourceContent,
	from string,
	location []parser.Range,
	ctx rules.BuildContext,
	file string,
) []rules.Violation {
	// Skip if copying from another stage or image
	if from != "" {
		return nil
	}

	// Build set of heredoc paths to skip (using shared helper)
	heredocPaths := dockerfile.CollectHeredocPaths(sourceContents)

	var violations []rules.Violation

	for _, src := range sourcePaths {
		// Skip URLs (ADD supports URLs)
		if isURL(src) {
			continue
		}

		// Skip heredoc sources
		if heredocPaths[src] {
			continue
		}

		// Skip if marked as heredoc in context
		if ctx.IsHeredocFile(src) {
			continue
		}

		// Normalize path (remove leading ./)
		normalizedSrc := normalizePath(src)

		// Check if ignored
		ignored, err := ctx.IsIgnored(normalizedSrc)
		if err != nil {
			// Surface the error as a warning so users know the rule was skipped
			loc := rules.NewLocationFromRanges(file, location)
			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				"failed to evaluate .dockerignore for '"+src+"'",
				rules.SeverityWarning,
			).WithDocURL(r.Metadata().DocURL).WithDetail(err.Error()))
			continue
		}

		if ignored {
			loc := rules.NewLocationFromRanges(file, location)
			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				"source '"+src+"' is excluded by .dockerignore and will not be copied",
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL).WithDetail(
				"The file or directory '"+src+"' matches a pattern in .dockerignore. "+
					"This COPY/ADD will fail or copy unexpected files during build."))
		}
	}

	return violations
}

// isURL checks if a path looks like a URL.
func isURL(path string) bool {
	return strings.HasPrefix(path, "http://") ||
		strings.HasPrefix(path, "https://") ||
		strings.HasPrefix(path, "ftp://")
}

// normalizePath normalizes a source path for comparison.
// Handles edge cases like trailing slashes, double slashes, and redundant segments.
func normalizePath(path string) string {
	cleaned := filepath.Clean(path)
	// filepath.Clean converts "." to ".", keep it as-is for root context
	if cleaned == "." {
		return "."
	}
	return cleaned
}

// New creates a new copy-ignored-file rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
