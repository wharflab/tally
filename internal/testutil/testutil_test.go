package testutil

import (
	"testing"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

func TestParseDockerfile(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestMakeLintInput_UsesShellDirectivesForSemanticAndFacts(t *testing.T) {
	t.Parallel()

	content := `# hadolint shell=bash
FROM alpine
RUN echo hello
`
	input := MakeLintInput(t, "test/Dockerfile", content)

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		t.Fatalf("Semantic type = %T, want *semantic.Model", input.Semantic)
	}
	stageInfo := sem.StageInfo(0)
	if stageInfo == nil {
		t.Fatal("StageInfo(0) returned nil")
	}
	if stageInfo.ShellSetting.Variant != shell.VariantBash {
		t.Fatalf("semantic shell variant = %v, want %v", stageInfo.ShellSetting.Variant, shell.VariantBash)
	}

	fileFacts, ok := input.Facts.(*facts.FileFacts)
	if !ok || fileFacts == nil {
		t.Fatalf("Facts type = %T, want *facts.FileFacts", input.Facts)
	}
	stageFacts := fileFacts.Stage(0)
	if stageFacts == nil {
		t.Fatal("Stage(0) returned nil")
	}
	if stageFacts.InitialShell.Variant != shell.VariantBash {
		t.Fatalf("facts shell variant = %v, want %v", stageFacts.InitialShell.Variant, shell.VariantBash)
	}
}

func TestMakeLintInputWithConfig(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	// Test with empty violations (should pass)
	AssertNoViolations(t, nil)
	AssertNoViolations(t, []rules.Violation{})
}

func TestAssertViolationCount(t *testing.T) {
	t.Parallel()
	v := []rules.Violation{
		rules.NewViolation(rules.NewLineLocation("test", 1), "test-rule", "msg", rules.SeverityError),
	}

	// Should pass
	AssertViolationCount(t, v, 1)
	AssertViolationCount(t, nil, 0)
	AssertViolationCount(t, []rules.Violation{}, 0)
}
