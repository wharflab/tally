// Package usecopynotadd implements hadolint DL3020.
// This rule warns when ADD is used instead of COPY for local files/folders.
// COPY is preferred because it's more transparent and only supports local file copying.
package usecopynotadd

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Rule implements the DL3020 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3020",
		Name:            "Use COPY instead of ADD",
		Description:     "Use COPY instead of ADD for local files; ADD has unexpected features",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3020",
		DefaultSeverity: rules.SeverityError,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// Check runs the DL3020 rule.
// It warns when ADD is used with local files/folders instead of COPY.
// ADD is acceptable for:
// - Remote URLs (http://, https://, ftp://)
// - Tar archives (recognized by extension or explicit use case)
// - Git repositories
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			add, ok := cmd.(*instructions.AddCommand)
			if !ok {
				continue
			}

			// Check each source
			for _, src := range add.SourcePaths {
				// Skip URLs - ADD is valid for URLs
				if isURL(src) {
					continue
				}

				// Skip tar archives - ADD auto-extracts them
				if isTarArchive(src) {
					continue
				}

				// Skip heredocs - they use special syntax
				if isHeredoc(src) {
					continue
				}

				// This is a local file/folder - should use COPY
				loc := rules.NewLocationFromRanges(input.File, add.Location())
				violations = append(violations, rules.NewViolation(
					loc,
					r.Metadata().Code,
					fmt.Sprintf(
						"use COPY instead of ADD for local file %q; COPY is more explicit and secure",
						src,
					),
					r.Metadata().DefaultSeverity,
				).WithDocURL(r.Metadata().DocURL).WithDetail(
					"ADD has implicit features (auto-extraction, URL fetching) that make builds less predictable. "+
						"Use COPY for simple file copies. Only use ADD when you need tar extraction or URL fetching.",
				))
				break // One violation per ADD instruction is enough
			}
		}
	}

	return violations
}

// isURL checks if a source path is a URL.
func isURL(src string) bool {
	src = strings.ToLower(src)
	return strings.HasPrefix(src, "http://") ||
		strings.HasPrefix(src, "https://") ||
		strings.HasPrefix(src, "ftp://") ||
		strings.HasPrefix(src, "git://") ||
		strings.HasPrefix(src, "git@")
}

// isTarArchive checks if a source path is a tar archive that ADD would extract.
func isTarArchive(src string) bool {
	src = strings.ToLower(src)
	tarExtensions := []string{
		".tar",
		".tar.gz", ".tgz",
		".tar.bz2", ".tbz", ".tbz2",
		".tar.xz", ".txz",
		".tar.zst", ".tzst",
		".tar.lz4",
	}
	for _, ext := range tarExtensions {
		if strings.HasSuffix(src, ext) {
			return true
		}
	}
	return false
}

// isHeredoc checks if a source is a heredoc marker.
func isHeredoc(src string) bool {
	return strings.HasPrefix(src, "<<")
}

// New creates a new DL3020 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
