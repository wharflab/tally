package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestDuplicateStageNameRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDuplicateStageNameRule().Metadata())
}

func TestDuplicateStageNameRule_Check(t *testing.T) {
	t.Parallel()
	r := NewDuplicateStageNameRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{Name: "builder"},
			{Name: "final"},
			{Name: "builder"}, // duplicate
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].RuleCode != "buildkit/DuplicateStageName" {
		t.Fatalf("expected rule %q, got %q", "buildkit/DuplicateStageName", violations[0].RuleCode)
	}
}

func TestDuplicateStageNameRule_Check_CaseInsensitive(t *testing.T) {
	t.Parallel()
	r := NewDuplicateStageNameRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{Name: "Builder"},
			{Name: "BUILDER"},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
}

func TestDuplicateStageNameRule_Check_EmptyNameIgnored(t *testing.T) {
	t.Parallel()
	r := NewDuplicateStageNameRule()

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
