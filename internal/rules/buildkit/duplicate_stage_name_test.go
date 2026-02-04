package buildkit

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestDuplicateStageNameRule_Metadata(t *testing.T) {
	r := NewDuplicateStageNameRule()
	meta := r.Metadata()
	if meta.Code != "buildkit/DuplicateStageName" {
		t.Fatalf("expected code %q, got %q", "buildkit/DuplicateStageName", meta.Code)
	}
	if meta.DefaultSeverity != rules.SeverityError {
		t.Fatalf("expected severity %v, got %v", rules.SeverityError, meta.DefaultSeverity)
	}
}

func TestDuplicateStageNameRule_Check(t *testing.T) {
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
