package directive

import "github.com/tinovyatkin/tally/internal/rules"

// FilterResult contains the results of filtering violations through directives.
type FilterResult struct {
	// Violations that were not suppressed.
	Violations []rules.Violation

	// Suppressed violations that were filtered out.
	Suppressed []rules.Violation

	// UnusedDirectives that did not suppress any violations.
	UnusedDirectives []Directive
}

// Filter applies directives to filter violations.
// Violations are suppressed if a directive matches both:
//   - The violation's rule code (or "all")
//   - The violation's line number
//
// Line number conversion: Violations use 1-based lines; directives use 0-based.
// We convert violation lines to 0-based for comparison.
//
// Matching precedence: Uses first-match-wins semantics. When multiple directives
// could suppress the same violation (e.g., a global and a next-line directive),
// only the first matching directive is marked as Used. This keeps suppression
// deterministic but may cause subsequent matching directives to appear unused.
func Filter(violations []rules.Violation, directives []Directive) *FilterResult {
	result := &FilterResult{
		Violations: make([]rules.Violation, 0, len(violations)),
		Suppressed: make([]rules.Violation, 0),
	}

	// Create a mutable copy of directives to track usage
	directiveCopies := make([]Directive, len(directives))
	copy(directiveCopies, directives)

	for _, v := range violations {
		suppressed := false
		// Convert 1-based violation line to 0-based
		line0 := v.Line() - 1

		for i := range directiveCopies {
			d := &directiveCopies[i]
			if d.SuppressesLine(line0) && d.SuppressesRule(v.RuleCode) {
				suppressed = true
				d.Used = true
				break
			}
		}

		if suppressed {
			result.Suppressed = append(result.Suppressed, v)
		} else {
			result.Violations = append(result.Violations, v)
		}
	}

	// Collect unused directives
	for _, d := range directiveCopies {
		if !d.Used {
			result.UnusedDirectives = append(result.UnusedDirectives, d)
		}
	}

	return result
}
