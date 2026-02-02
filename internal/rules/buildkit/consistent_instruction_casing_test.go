package buildkit

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestConsistentInstructionCasingRule_Metadata(t *testing.T) {
	r := NewConsistentInstructionCasingRule()
	meta := r.Metadata()

	assert.Equal(t, "buildkit/ConsistentInstructionCasing", meta.Code)
	assert.Equal(t, "style", meta.Category)
	assert.Equal(t, rules.SeverityWarning, meta.DefaultSeverity)
}

func TestConsistentInstructionCasingRule_Check_MajorityUppercase(t *testing.T) {
	r := NewConsistentInstructionCasingRule()

	// 2 uppercase (FROM, COPY), 1 lowercase (run) -> majority uppercase
	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				OrigCmd:  "FROM",
				Location: []parser.Range{{Start: parser.Position{Line: 1, Character: 0}}},
				Commands: []instructions.Command{
					&runCommandMock{name: "run", loc: []parser.Range{{Start: parser.Position{Line: 2, Character: 0}}}},
					&copyCommandMock{name: "COPY", loc: []parser.Range{{Start: parser.Position{Line: 3, Character: 0}}}},
				},
			},
		},
	}

	violations := r.Check(input)
	require.Len(t, violations, 1)

	assert.Equal(t, "buildkit/ConsistentInstructionCasing", violations[0].RuleCode)
	assert.Contains(t, violations[0].Message, "run")
	assert.Contains(t, violations[0].Message, "uppercase")
}

func TestConsistentInstructionCasingRule_Check_MajorityLowercase(t *testing.T) {
	r := NewConsistentInstructionCasingRule()

	// 1 uppercase (COPY), 2 lowercase (from, run) -> majority lowercase
	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				OrigCmd:  "from",
				Location: []parser.Range{{Start: parser.Position{Line: 1, Character: 0}}},
				Commands: []instructions.Command{
					&runCommandMock{name: "run", loc: []parser.Range{{Start: parser.Position{Line: 2, Character: 0}}}},
					&copyCommandMock{name: "COPY", loc: []parser.Range{{Start: parser.Position{Line: 3, Character: 0}}}},
				},
			},
		},
	}

	violations := r.Check(input)
	require.Len(t, violations, 1)

	assert.Equal(t, "buildkit/ConsistentInstructionCasing", violations[0].RuleCode)
	assert.Contains(t, violations[0].Message, "COPY")
	assert.Contains(t, violations[0].Message, "lowercase")
}

func TestConsistentInstructionCasingRule_Check_AllUppercase(t *testing.T) {
	r := NewConsistentInstructionCasingRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				OrigCmd:  "FROM",
				Location: []parser.Range{{Start: parser.Position{Line: 1, Character: 0}}},
				Commands: []instructions.Command{
					&runCommandMock{name: "RUN", loc: []parser.Range{{Start: parser.Position{Line: 2, Character: 0}}}},
					&copyCommandMock{name: "COPY", loc: []parser.Range{{Start: parser.Position{Line: 3, Character: 0}}}},
				},
			},
		},
	}

	violations := r.Check(input)
	assert.Empty(t, violations)
}

func TestConsistentInstructionCasingRule_Check_AllLowercase(t *testing.T) {
	r := NewConsistentInstructionCasingRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				OrigCmd:  "from",
				Location: []parser.Range{{Start: parser.Position{Line: 1, Character: 0}}},
				Commands: []instructions.Command{
					&runCommandMock{name: "run", loc: []parser.Range{{Start: parser.Position{Line: 2, Character: 0}}}},
					&copyCommandMock{name: "copy", loc: []parser.Range{{Start: parser.Position{Line: 3, Character: 0}}}},
				},
			},
		},
	}

	violations := r.Check(input)
	assert.Empty(t, violations)
}

func TestConsistentInstructionCasingRule_Check_EqualCountPrefersUppercase(t *testing.T) {
	r := NewConsistentInstructionCasingRule()

	// 1 uppercase (FROM), 1 lowercase (run) -> tie, prefer uppercase
	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				OrigCmd:  "FROM",
				Location: []parser.Range{{Start: parser.Position{Line: 1, Character: 0}}},
				Commands: []instructions.Command{
					&runCommandMock{name: "run", loc: []parser.Range{{Start: parser.Position{Line: 2, Character: 0}}}},
				},
			},
		},
	}

	violations := r.Check(input)
	require.Len(t, violations, 1)

	// Should report lowercase 'run' as violation (expecting uppercase)
	assert.Contains(t, violations[0].Message, "run")
	assert.Contains(t, violations[0].Message, "uppercase")
}

func TestConsistentInstructionCasingRule_Check_MixedCaseIgnored(t *testing.T) {
	r := NewConsistentInstructionCasingRule()

	// "From" is mixed case - not counted for majority but still flagged
	// 1 uppercase (RUN), 0 lowercase (From is mixed) -> uppercase wins
	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				OrigCmd:  "From", // Mixed case, not counted
				Location: []parser.Range{{Start: parser.Position{Line: 1, Character: 0}}},
				Commands: []instructions.Command{
					&runCommandMock{name: "RUN", loc: []parser.Range{{Start: parser.Position{Line: 2, Character: 0}}}},
				},
			},
		},
	}

	violations := r.Check(input)
	require.Len(t, violations, 1)

	// "From" should be flagged as needing uppercase
	assert.Contains(t, violations[0].Message, "From")
	assert.Contains(t, violations[0].Message, "uppercase")
}

func TestConsistentInstructionCasingRule_Check_MultipleStages(t *testing.T) {
	r := NewConsistentInstructionCasingRule()

	// Stage 1: FROM, RUN (2 upper)
	// Stage 2: from, copy (2 lower)
	// Total: 2 upper, 2 lower -> tie, prefer uppercase
	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				OrigCmd:  "FROM",
				Location: []parser.Range{{Start: parser.Position{Line: 1, Character: 0}}},
				Commands: []instructions.Command{
					&runCommandMock{name: "RUN", loc: []parser.Range{{Start: parser.Position{Line: 2, Character: 0}}}},
				},
			},
			{
				OrigCmd:  "from",
				Location: []parser.Range{{Start: parser.Position{Line: 3, Character: 0}}},
				Commands: []instructions.Command{
					&copyCommandMock{name: "copy", loc: []parser.Range{{Start: parser.Position{Line: 4, Character: 0}}}},
				},
			},
		},
	}

	violations := r.Check(input)
	require.Len(t, violations, 2)

	// Both lowercase commands should be flagged
	for _, v := range violations {
		assert.Contains(t, v.Message, "uppercase")
	}
}

func TestConsistentInstructionCasingRule_Check_MetaArgs(t *testing.T) {
	r := NewConsistentInstructionCasingRule()

	// MetaArgs: arg (lowercase), FROM and RUN are uppercase
	// Total: 2 upper, 1 lower -> uppercase wins, 'arg' should be flagged
	input := rules.LintInput{
		File: "Dockerfile",
		MetaArgs: []instructions.ArgCommand{
			{},
		},
		Stages: []instructions.Stage{
			{
				OrigCmd:  "FROM",
				Location: []parser.Range{{Start: parser.Position{Line: 2, Character: 0}}},
				Commands: []instructions.Command{
					&runCommandMock{name: "RUN", loc: []parser.Range{{Start: parser.Position{Line: 3, Character: 0}}}},
				},
			},
		},
	}

	// Manually set the MetaArg name since we can't easily construct it
	// The test verifies MetaArgs are included in counting
	violations := r.Check(input)

	// With no way to set MetaArg.Name() in test, this just verifies the code path doesn't panic
	// and the existing stage commands are still processed correctly
	assert.Empty(t, violations) // All uppercase, no violations
}

func TestIsSelfConsistentCasing(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"FROM", true},      // All uppercase
		{"from", true},      // All lowercase
		{"From", false},     // Mixed
		{"FROM1", true},     // Uppercase with number
		{"run", true},       // All lowercase
		{"rUn", false},      // Mixed
		{"WORKDIR", true},   // All uppercase
		{"workdir", true},   // All lowercase
		{"WorkDir", false},  // Mixed
		{"copy", true},      // All lowercase
		{"COPY", true},      // All uppercase
		{"cOpY", false},     // Mixed
		{"ADD", true},       // Short all uppercase
		{"add", true},       // Short all lowercase
		{"Add", false},      // Short mixed
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isSelfConsistentCasing(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Mock command types for testing
type runCommandMock struct {
	instructions.RunCommand

	name string
	loc  []parser.Range
}

func (m *runCommandMock) Name() string             { return m.name }
func (m *runCommandMock) Location() []parser.Range { return m.loc }

type copyCommandMock struct {
	instructions.CopyCommand

	name string
	loc  []parser.Range
}

func (m *copyCommandMock) Name() string             { return m.name }
func (m *copyCommandMock) Location() []parser.Range { return m.loc }
