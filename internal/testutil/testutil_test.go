package testutil

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestParseDockerfile(t *testing.T) {
	content := "FROM alpine\nRUN echo hello"
	result := ParseDockerfile(t, content)

	if result == nil {
		t.Fatal("ParseDockerfile returned nil")
	}
	if result.AST == nil {
		t.Error("AST is nil")
	}
	// Verify full ParseResult fields are populated
	if len(result.Stages) != 1 {
		t.Errorf("Stages = %d, want 1", len(result.Stages))
	}
	if len(result.Source) == 0 {
		t.Error("Source is empty")
	}
}

func TestMakeLintInput(t *testing.T) {
	content := "FROM alpine\nRUN echo hello"
	input := MakeLintInput(t, "test/Dockerfile", content)

	if input.File != "test/Dockerfile" {
		t.Errorf("File = %q, want %q", input.File, "test/Dockerfile")
	}
	if input.AST == nil {
		t.Error("AST is nil")
	}
	if string(input.Source) != content {
		t.Errorf("Source = %q, want %q", string(input.Source), content)
	}
	if input.Context != nil {
		t.Error("Context should be nil")
	}
	if input.Config != nil {
		t.Error("Config should be nil")
	}
	// Verify Stages is populated (single FROM = 1 stage)
	if len(input.Stages) != 1 {
		t.Errorf("Stages = %d, want 1", len(input.Stages))
	}
	// MetaArgs should be empty for this content
	if len(input.MetaArgs) != 0 {
		t.Errorf("MetaArgs = %d, want 0", len(input.MetaArgs))
	}
}

func TestMakeLintInputWithConfig(t *testing.T) {
	content := "FROM alpine"
	config := struct{ Max int }{Max: 100}

	input := MakeLintInputWithConfig(t, "Dockerfile", content, config)

	if input.Config == nil {
		t.Fatal("Config is nil")
	}
	cfg, ok := input.Config.(struct{ Max int })
	if !ok {
		t.Fatalf("Config type = %T, want struct{Max int}", input.Config)
	}
	if cfg.Max != 100 {
		t.Errorf("Config.Max = %d, want 100", cfg.Max)
	}
}

func TestAssertNoViolations(t *testing.T) {
	// Test with empty violations (should pass)
	AssertNoViolations(t, nil)
	AssertNoViolations(t, []rules.Violation{})
}

func TestAssertViolationCount(t *testing.T) {
	v := []rules.Violation{
		rules.NewViolation(rules.NewLineLocation("test", 1), "test-rule", "msg", rules.SeverityError),
	}

	// Should pass
	AssertViolationCount(t, v, 1)
	AssertViolationCount(t, nil, 0)
	AssertViolationCount(t, []rules.Violation{}, 0)
}
