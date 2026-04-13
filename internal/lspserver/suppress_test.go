package lspserver

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/dockerfile"
	protocol "github.com/wharflab/tally/internal/lsp/protocol"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

func TestSuppressLineEdit_BasicInsertion(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\nRUN apt-get update\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 2, 0, 2, 17),
		"hadolint/DL3027",
		"msg",
		rules.SeverityWarning,
	)

	edit := suppressLineEdit(v, lines, dirResult, 0, false)
	require.NotNil(t, edit)
	assert.Equal(t, "# tally ignore=hadolint/DL3027\n", edit.NewText)
	assert.Equal(t, uint32(1), edit.Range.Start.Line, "should insert before the RUN line (0-based line 1)")
}

func TestSuppressLineEdit_WithRequireReason(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\nRUN apt-get update\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 2, 0, 2, 17),
		"tally/test-rule",
		"msg",
		rules.SeverityWarning,
	)

	edit := suppressLineEdit(v, lines, dirResult, 0, true)
	require.NotNil(t, edit)
	assert.Equal(t, "# tally ignore=tally/test-rule;reason=TODO\n", edit.NewText)
}

func TestSuppressLineEdit_AboveCommentBlock(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\n# builder builds the app\nRUN make build\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 3, 0, 3, 14),
		"tally/test-rule",
		"msg",
		rules.SeverityWarning,
	)

	edit := suppressLineEdit(v, lines, dirResult, 0, false)
	require.NotNil(t, edit)
	assert.Equal(t, "# tally ignore=tally/test-rule\n", edit.NewText)
	assert.Equal(t, uint32(1), edit.Range.Start.Line,
		"should insert above the comment block, not between comment and instruction")
}

func TestSuppressLineEdit_MergesWithExisting(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\n# tally ignore=DL3008\nRUN apt-get update\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 3, 0, 3, 17),
		"DL3027",
		"msg",
		rules.SeverityWarning,
	)

	edit := suppressLineEdit(v, lines, dirResult, 0, false)
	require.NotNil(t, edit)
	assert.Equal(t, ",DL3027", edit.NewText, "should append rule to existing directive")
	assert.Equal(t, uint32(1), edit.Range.Start.Line, "should edit the existing directive line")
}

func TestSuppressLineEdit_AlreadySuppressed(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\n# tally ignore=DL3027\nRUN apt-get update\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 3, 0, 3, 17),
		"DL3027",
		"msg",
		rules.SeverityWarning,
	)

	edit := suppressLineEdit(v, lines, dirResult, 0, false)
	assert.Nil(t, edit, "should return nil when rule is already suppressed")
}

func TestSuppressLineEdit_Indentation(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\n\tRUN apt-get update\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 2, 1, 2, 18),
		"tally/test-rule",
		"msg",
		rules.SeverityWarning,
	)

	edit := suppressLineEdit(v, lines, dirResult, 0, false)
	require.NotNil(t, edit)
	assert.Equal(t, "\t# tally ignore=tally/test-rule\n", edit.NewText,
		"should match leading whitespace of the instruction")
}

func TestSuppressFileEdit_BasicInsertion(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\nRUN apt-get update\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	edit := suppressFileEdit("tally/max-lines", lines, dirResult, 0, false)
	require.NotNil(t, edit)
	assert.Equal(t, "# tally global ignore=tally/max-lines\n", edit.NewText)
	assert.Equal(t, uint32(0), edit.Range.Start.Line, "should insert at the top of the file")
}

func TestSuppressFileEdit_AfterSyntaxDirective(t *testing.T) {
	t.Parallel()

	content := "# syntax=docker/dockerfile:1\nFROM alpine\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	// FROM is on line 1 (0-based), so firstInstLine0 = 1
	edit := suppressFileEdit("tally/max-lines", lines, dirResult, 1, false)
	require.NotNil(t, edit)
	assert.Equal(t, "# tally global ignore=tally/max-lines\n", edit.NewText)
	assert.Equal(t, uint32(1), edit.Range.Start.Line,
		"should insert after the # syntax= parser directive")
}

func TestSuppressFileEdit_MergesWithExistingGlobal(t *testing.T) {
	t.Parallel()

	content := "# tally global ignore=DL3008\nFROM alpine\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	edit := suppressFileEdit("DL3027", lines, dirResult, 0, false)
	require.NotNil(t, edit)
	assert.Equal(t, ",DL3027", edit.NewText, "should append to existing global directive")
	assert.Equal(t, uint32(0), edit.Range.Start.Line)
}

func TestSuppressFileEdit_AlreadySuppressed(t *testing.T) {
	t.Parallel()

	content := "# tally global ignore=DL3027\nFROM alpine\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	edit := suppressFileEdit("DL3027", lines, dirResult, 0, false)
	assert.Nil(t, edit, "should return nil when rule is already globally suppressed")
}

func TestSuppressFileEdit_WithRequireReason(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\n"
	lines := strings.Split(content, "\n")
	sm := sourcemap.New([]byte(content))
	dirResult := directive.Parse(sm, nil, nil)

	edit := suppressFileEdit("tally/max-lines", lines, dirResult, 0, true)
	require.NotNil(t, edit)
	assert.Equal(t, "# tally global ignore=tally/max-lines;reason=TODO\n", edit.NewText)
}

func TestSuppressRuleActions_EmitsLineAndFileActions(t *testing.T) {
	t.Parallel()

	content := "FROM alpine\nRUN apt-get update\n"
	source := []byte(content)

	// Parse just like the real code does.
	parseResult := parseDockerfile(t, source)

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 2, 0, 2, 17),
		"hadolint/DL3027",
		"msg",
		rules.SeverityWarning,
	)
	v.DocURL = "https://example.com"

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})
	actions := suppressRuleActions([]rules.Violation{v}, params, content, parseResult, nil)

	require.Len(t, actions, 2)
	assert.Contains(t, actions[0].Title, "Suppress hadolint/DL3027 for this line")
	assert.Contains(t, actions[1].Title, "Suppress hadolint/DL3027 for this file")

	// Verify stable Data field.
	for _, a := range actions {
		require.NotNil(t, a.Data)
		data, ok := (*a.Data).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "hadolint/DL3027", data["ruleCode"])
	}
}

func TestFindCommentBlockStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		lines            []string
		instructionLine0 int
		floorLine0       int
		want             int
	}{
		{
			name:             "no comment above",
			lines:            []string{"FROM alpine", "RUN make"},
			instructionLine0: 1,
			want:             1,
		},
		{
			name:             "single comment",
			lines:            []string{"FROM alpine", "# comment", "RUN make"},
			instructionLine0: 2,
			want:             1,
		},
		{
			name:             "multi-line comment block",
			lines:            []string{"FROM alpine", "# comment1", "# comment2", "RUN make"},
			instructionLine0: 3,
			want:             1,
		},
		{
			name:             "blank line separates",
			lines:            []string{"FROM alpine", "", "# comment", "RUN make"},
			instructionLine0: 3,
			want:             2,
		},
		{
			name:             "first line of file",
			lines:            []string{"FROM alpine"},
			instructionLine0: 0,
			want:             0,
		},
		{
			name:             "stops at floor (parser directives)",
			lines:            []string{"# syntax=docker/dockerfile:1", "# escape=\\", "# comment", "FROM alpine"},
			instructionLine0: 3,
			floorLine0:       3, // FROM is the first instruction
			want:             3,
		},
		{
			name:             "stops at bare hash",
			lines:            []string{"FROM alpine", "#", "# description comment", "RUN make"},
			instructionLine0: 3,
			want:             2,
		},
		{
			name:             "parser directives contiguous with instruction",
			lines:            []string{"# syntax=docker/dockerfile:1", "FROM alpine"},
			instructionLine0: 1,
			floorLine0:       1,
			want:             1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, findCommentBlockStart(tt.instructionLine0, tt.floorLine0, tt.lines))
		})
	}
}

func TestLeadingWhitespace(t *testing.T) {
	t.Parallel()
	assert.Empty(t, leadingWhitespace("RUN make"))
	assert.Equal(t, "  ", leadingWhitespace("  RUN make"))
	assert.Equal(t, "\t", leadingWhitespace("\tRUN make"))
	assert.Equal(t, "\t  ", leadingWhitespace("\t  RUN make"))
}

// parseDockerfile parses a Dockerfile from source bytes for testing.
func parseDockerfile(t *testing.T, source []byte) *dockerfile.ParseResult {
	t.Helper()
	result, err := dockerfile.Parse(bytes.NewReader(source), nil)
	require.NoError(t, err)
	return result
}
