package directive

import (
	"testing"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/semantic"
)

func TestToFactsShellDirectives(t *testing.T) {
	t.Parallel()

	got := ToFactsShellDirectives([]ShellDirective{
		{Line: 3, Shell: "bash"},
		{Line: 7, Shell: "powershell"},
	})

	if len(got) != 2 {
		t.Fatalf("expected 2 shell directives, got %d", len(got))
	}
	if got[0] != (facts.ShellDirective{Line: 3, Shell: "bash"}) {
		t.Fatalf("unexpected first shell directive: %#v", got[0])
	}
	if got[1] != (facts.ShellDirective{Line: 7, Shell: "powershell"}) {
		t.Fatalf("unexpected second shell directive: %#v", got[1])
	}
	if ToFactsShellDirectives(nil) != nil {
		t.Fatal("expected nil result for nil input")
	}
}

func TestToSemanticShellDirectives(t *testing.T) {
	t.Parallel()

	got := ToSemanticShellDirectives([]ShellDirective{
		{Line: 3, Shell: "bash"},
		{Line: 7, Shell: "powershell"},
	})

	if len(got) != 2 {
		t.Fatalf("expected 2 shell directives, got %d", len(got))
	}
	if got[0] != (semantic.ShellDirective{Line: 3, Shell: "bash"}) {
		t.Fatalf("unexpected first shell directive: %#v", got[0])
	}
	if got[1] != (semantic.ShellDirective{Line: 7, Shell: "powershell"}) {
		t.Fatalf("unexpected second shell directive: %#v", got[1])
	}
	if ToSemanticShellDirectives(nil) != nil {
		t.Fatal("expected nil result for nil input")
	}
}
