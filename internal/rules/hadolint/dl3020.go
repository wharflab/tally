package hadolint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// DL3020Rule implements the DL3020 linting rule.
type DL3020Rule struct{}

// NewDL3020Rule creates a new DL3020 rule instance.
func NewDL3020Rule() *DL3020Rule {
	return &DL3020Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3020Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3020",
		Name:            "Use COPY instead of ADD",
		Description:     "Use COPY instead of ADD for local files; ADD has unexpected features",
		DocURL:          rules.HadolintDocURL("DL3020"),
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
func (r *DL3020Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()
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
				if isURLDL3020(src) {
					continue
				}

				// Skip tar archives - ADD auto-extracts them
				if isTarArchiveDL3020(src) {
					continue
				}

				// Skip heredocs - they use special syntax
				if isHeredocDL3020(src) {
					continue
				}

				// This is a local file/folder - should use COPY
				loc := rules.NewLocationFromRanges(input.File, add.Location())
				v := rules.NewViolation(
					loc,
					meta.Code,
					fmt.Sprintf(
						"use COPY instead of ADD for local file %q; COPY is more explicit and secure",
						src,
					),
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"ADD has implicit features (auto-extraction, URL fetching) that make builds less predictable. " +
						"Use COPY for simple file copies. Only use ADD when you need tar extraction or URL fetching.",
				)

				if fix := buildDL3020Fix(input.File, add.Location(), sm, meta); fix != nil {
					v = v.WithSuggestedFix(fix)
				}

				violations = append(violations, v)
				break // One violation per ADD instruction is enough
			}
		}
	}

	return violations
}

// buildDL3020Fix generates an auto-fix that replaces "ADD" with "COPY" on the
// instruction's first source line. The edit targets only the 3-character keyword,
// preserving all flags, sources, and destination unchanged.
func buildDL3020Fix(
	file string,
	ranges []parser.Range,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	if len(ranges) == 0 {
		return nil
	}

	startLine := ranges[0].Start.Line // 1-based
	lineIdx := startLine - 1          // 0-based for SourceMap
	if lineIdx < 0 || lineIdx >= sm.LineCount() {
		return nil
	}

	sourceLine := sm.Line(lineIdx)

	// Find "ADD" keyword in the source line (case-insensitive), matching BuildKit's
	// convention where the keyword appears at or near the start of the line.
	upper := strings.ToUpper(sourceLine)
	addKeyword := strings.ToUpper(command.Add) // "ADD"
	idx := strings.Index(upper, addKeyword)
	if idx < 0 {
		return nil
	}

	// Verify that the character after "ADD" is whitespace (space or tab) to
	// avoid matching a word like "ADDRESS".
	afterAdd := idx + len(addKeyword)
	if afterAdd >= len(sourceLine) ||
		(sourceLine[afterAdd] != ' ' && sourceLine[afterAdd] != '\t') {
		return nil
	}

	// Preserve the original casing style: if the original keyword is all-lowercase,
	// use "copy"; otherwise default to "COPY".
	original := sourceLine[idx : idx+len(addKeyword)]
	replacement := strings.ToUpper(command.Copy) // "COPY"
	if original == strings.ToLower(original) {
		replacement = command.Copy // "copy"
	}

	return &rules.SuggestedFix{
		Description: "Replace ADD with COPY",
		Safety:      rules.FixSafe,
		Priority:    meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(
				file,
				startLine, idx,
				startLine, afterAdd,
			),
			NewText: replacement,
		}},
	}
}

// isURLDL3020 checks if a source path is a URL.
func isURLDL3020(src string) bool {
	src = shell.DropQuotes(strings.ToLower(src))
	return shell.IsURL(src) ||
		strings.HasPrefix(src, "git://") ||
		strings.HasPrefix(src, "git@")
}

// isTarArchiveDL3020 checks if a source path is a tar archive that ADD would extract.
func isTarArchiveDL3020(src string) bool {
	src = shell.DropQuotes(strings.ToLower(src))
	tarExtensions := []string{
		".tar",
		".tar.gz", ".tgz",
		".tar.bz2", ".tbz", ".tbz2",
		".tar.xz", ".txz",
		".tar.zst", ".tzst",
		".tar.lz4",
	}
	return slices.ContainsFunc(tarExtensions, func(ext string) bool {
		return strings.HasSuffix(src, ext)
	})
}

// isHeredocDL3020 checks if a source is a heredoc marker.
func isHeredocDL3020(src string) bool {
	src = strings.TrimSpace(shell.DropQuotes(src))
	return strings.HasPrefix(src, "<<")
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3020Rule())
}
