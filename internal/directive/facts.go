package directive

import "github.com/wharflab/tally/internal/facts"

// ToFactsShellDirectives converts parsed directive shell directives into the
// lightweight facts representation used by the semantic facts layer.
func ToFactsShellDirectives(directives []ShellDirective) []facts.ShellDirective {
	if len(directives) == 0 {
		return nil
	}

	out := make([]facts.ShellDirective, 0, len(directives))
	for _, d := range directives {
		out = append(out, facts.ShellDirective{
			Line:  d.Line,
			Shell: d.Shell,
		})
	}
	return out
}
