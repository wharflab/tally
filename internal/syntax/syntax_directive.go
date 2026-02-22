package syntax

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// knownFrontends lists well-known syntax directive image repositories.
var knownFrontends = []string{
	"docker/dockerfile",
	"docker.io/docker/dockerfile",
}

// checkSyntaxDirective detects typos in `# syntax=` parser directives.
func checkSyntaxDirective(file string, source []byte) []Error {
	syntax, _, loc, ok := parser.DetectSyntax(source)
	if !ok || syntax == "" {
		return nil
	}

	// Validate: must be non-empty with no spaces.
	if strings.ContainsAny(syntax, " \t") {
		return []Error{{
			File:     file,
			Message:  fmt.Sprintf("syntax directive %q contains whitespace", syntax),
			Line:     directiveLine(loc),
			RuleCode: "tally/syntax-directive-typo",
		}}
	}

	// Split off the tag (e.g. "docker/dockerfile:1.7" -> repo "docker/dockerfile", tag ":1.7").
	repo, tag, _ := strings.Cut(syntax, ":")

	suggestion := closestMatch(repo, knownFrontends, 3)
	if suggestion == "" || suggestion == repo {
		return nil
	}

	suggested := suggestion
	if tag != "" {
		suggested += ":" + tag
	}
	return []Error{{
		File:     file,
		Message:  fmt.Sprintf("syntax directive %q appears misspelled (did you mean %q?)", syntax, suggested),
		Line:     directiveLine(loc),
		RuleCode: "tally/syntax-directive-typo",
	}}
}

// directiveLine extracts the 1-based line number from a parser.Range slice.
func directiveLine(loc []parser.Range) int {
	if len(loc) > 0 {
		return loc[0].Start.Line
	}
	return 1
}
