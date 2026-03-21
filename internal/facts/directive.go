package facts

import "github.com/wharflab/tally/internal/directive"

// ShellDirectivesFromDirective converts directive shell directives to facts shell directives.
func ShellDirectivesFromDirective(directives []directive.ShellDirective) []ShellDirective {
	if len(directives) == 0 {
		return nil
	}

	out := make([]ShellDirective, 0, len(directives))
	for _, directive := range directives {
		out = append(out, ShellDirective{
			Line:  directive.Line,
			Shell: directive.Shell,
		})
	}
	return out
}
