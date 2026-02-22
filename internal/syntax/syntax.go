// Package syntax provides fail-fast AST-level correctness checks that run
// before the full lint pipeline. If any check fires, linting is aborted.
package syntax

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// Error represents a single syntax-level diagnostic.
type Error struct {
	File     string // path to the Dockerfile
	Message  string // e.g. `unknown instruction "FORM" (did you mean "FROM"?)`
	Line     int    // 1-based line number
	RuleCode string // e.g. "tally/unknown-instruction"
}

func (e *Error) Error() string {
	return e.File + ":" + strconv.Itoa(e.Line) + ": " + e.Message
}

// CheckError wraps one or more syntax check failures for error propagation.
type CheckError struct {
	Errors []Error
}

func (e *CheckError) Error() string {
	n := len(e.Errors)
	if n == 1 {
		return "1 syntax error found"
	}
	return fmt.Sprintf("%d syntax errors found", n)
}

// Check runs all syntax checks on a parsed AST.
// Returns nil if no issues are found.
func Check(file string, ast *parser.Result, source []byte) []Error {
	errs := make([]Error, 0, 4) //nolint:mnd // small pre-alloc for typical case
	errs = append(errs, checkUnknownInstructions(file, ast)...)
	errs = append(errs, checkSyntaxDirective(file, source)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// closestMatch returns the closest string from candidates using Levenshtein
// distance, or "" if no candidate is within maxDist.
func closestMatch(input string, candidates []string, maxDist int) string {
	best := ""
	bestDist := maxDist + 1
	for _, c := range candidates {
		d := levenshteinDistance(input, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	if bestDist <= maxDist {
		return best
	}
	return ""
}

// levenshteinDistance computes the Levenshtein edit distance between two strings.
// This is a simple O(mn) implementation sufficient for short instruction keywords.
func levenshteinDistance(a, b string) int {
	ra := []rune(strings.ToLower(a))
	rb := []rune(strings.ToLower(b))
	la, lb := len(ra), len(rb)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Use single-row optimisation.
	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		cur := make([]int, lb+1)
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(
				cur[j-1]+1,     // insert
				prev[j]+1,      // delete
				prev[j-1]+cost, // substitute
			)
		}
		prev = cur
	}
	return prev[lb]
}
