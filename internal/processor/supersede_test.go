package processor

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

func TestSupersession_ErrorSuppressesLower(t *testing.T) {
	t.Parallel()
	p := NewSupersession()

	violations := []rules.Violation{
		{
			RuleCode: "buildkit/ReservedStageName",
			Severity: rules.SeverityError,
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 1}},
		},
		{
			RuleCode: "buildkit/StageNameCasing",
			Severity: rules.SeverityWarning,
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 1}},
		},
		{
			RuleCode: "buildkit/StageNameCasing",
			Severity: rules.SeverityWarning,
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 5}},
		},
	}

	result := p.Process(violations, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(result))
	}
	if result[0].RuleCode != "buildkit/ReservedStageName" {
		t.Errorf("expected ReservedStageName, got %q", result[0].RuleCode)
	}
	if result[1].RuleCode != "buildkit/StageNameCasing" || result[1].Location.Start.Line != 5 {
		t.Errorf("expected StageNameCasing on line 5, got %q on line %d",
			result[1].RuleCode, result[1].Location.Start.Line)
	}
}

func TestSupersession_MultipleErrors(t *testing.T) {
	t.Parallel()
	p := NewSupersession()

	violations := []rules.Violation{
		{
			RuleCode: "rule/error-a",
			Severity: rules.SeverityError,
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 3}},
		},
		{
			RuleCode: "rule/error-b",
			Severity: rules.SeverityError,
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 3}},
		},
		{
			RuleCode: "rule/info",
			Severity: rules.SeverityInfo,
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 3}},
		},
	}

	result := p.Process(violations, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 violations (both errors kept, info dropped), got %d", len(result))
	}
}

func TestSupersession_NoErrors(t *testing.T) {
	t.Parallel()
	p := NewSupersession()

	violations := []rules.Violation{
		{
			RuleCode: "buildkit/StageNameCasing",
			Severity: rules.SeverityWarning,
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 1}},
		},
		{
			RuleCode: "buildkit/DuplicateStageName",
			Severity: rules.SeverityWarning,
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 3}},
		},
	}

	result := p.Process(violations, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 violations (no suppression), got %d", len(result))
	}
}
