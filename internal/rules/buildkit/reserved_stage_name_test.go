package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
)

func TestReservedStageNameRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewReservedStageNameRule().Metadata())
}

func TestReservedStageNameRule_Check(t *testing.T) {
	t.Parallel()
	r := NewReservedStageNameRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{Name: "scratch"},
			{Name: "context"},
		},
	}

	violations := r.Check(input)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
	if violations[0].RuleCode != "buildkit/ReservedStageName" {
		t.Fatalf("expected rule %q, got %q", "buildkit/ReservedStageName", violations[0].RuleCode)
	}
}

func TestReservedStageNameRule_Check_NoName(t *testing.T) {
	t.Parallel()
	r := NewReservedStageNameRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{Name: ""},
			{Name: "final"},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestReservedStageNameRule_Check_NonReserved(t *testing.T) {
	t.Parallel()
	r := NewReservedStageNameRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{Name: "builder"},
			{Name: "final"},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestReservedStageNameRule_Check_CaseSensitive(t *testing.T) {
	t.Parallel()
	r := NewReservedStageNameRule()

	// "Scratch" and "Context" (capitalized) should NOT trigger - matching BuildKit behavior
	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{Name: "Scratch"},
			{Name: "Context"},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}
