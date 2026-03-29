package directive

import "github.com/wharflab/tally/internal/semantic"

// ToSemanticShellDirectives converts parsed directive shell directives into the
// lightweight representation used by the semantic builder.
func ToSemanticShellDirectives(directives []ShellDirective) []semantic.ShellDirective {
	if len(directives) == 0 {
		return nil
	}

	out := make([]semantic.ShellDirective, 0, len(directives))
	for _, d := range directives {
		out = append(out, semantic.ShellDirective{
			Line:  d.Line,
			Shell: d.Shell,
		})
	}
	return out
}
