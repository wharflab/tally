package processor

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/rules"
)

func TestSnippetAttachment_Name(t *testing.T) {
	t.Parallel()
	p := NewSnippetAttachment()
	if p.Name() != "snippet-attachment" {
		t.Errorf("expected snippet-attachment, got %s", p.Name())
	}
}

func TestSnippetAttachment_SkipsExistingSnippet(t *testing.T) {
	t.Parallel()
	p := NewSnippetAttachment()

	violations := []rules.Violation{
		{
			Location:   rules.NewLineLocation("file.txt", 1),
			RuleCode:   "test-rule",
			Message:    "test",
			Severity:   rules.SeverityWarning,
			SourceCode: "existing snippet",
		},
	}

	ctx := NewContext(nil, config.Default(), map[string][]byte{
		"file.txt": []byte("line1\nline2\n"),
	})

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result))
	}
	if result[0].SourceCode != "existing snippet" {
		t.Errorf("expected existing snippet preserved, got %s", result[0].SourceCode)
	}
}

func TestSnippetAttachment_SkipsFileLevelViolations(t *testing.T) {
	t.Parallel()
	p := NewSnippetAttachment()

	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewFileLocation("file.txt"),
			"test-rule",
			"file-level issue",
			rules.SeverityWarning,
		),
	}

	ctx := NewContext(nil, config.Default(), map[string][]byte{
		"file.txt": []byte("line1\nline2\n"),
	})

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result))
	}
	if result[0].SourceCode != "" {
		t.Errorf("expected no snippet for file-level violation, got %s", result[0].SourceCode)
	}
}

func TestSnippetAttachment_SkipsWhenSourceNotAvailable(t *testing.T) {
	t.Parallel()
	p := NewSnippetAttachment()

	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("missing-file.txt", 1),
			"test-rule",
			"test",
			rules.SeverityWarning,
		),
	}

	ctx := NewContext(nil, config.Default(), map[string][]byte{
		"other-file.txt": []byte("content"),
	})

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result))
	}
	if result[0].SourceCode != "" {
		t.Errorf("expected no snippet when file not in sources, got %s", result[0].SourceCode)
	}
}

func TestSnippetAttachment_ExtractsPointLocation(t *testing.T) {
	t.Parallel()
	p := NewSnippetAttachment()

	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 2),
			"test-rule",
			"test",
			rules.SeverityWarning,
		),
	}

	ctx := NewContext(nil, config.Default(), map[string][]byte{
		"file.txt": []byte("line1\nline2\nline3\n"),
	})

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result))
	}
	if result[0].SourceCode != "line2" {
		t.Errorf("expected 'line2', got %s", result[0].SourceCode)
	}
}

func TestSnippetAttachment_ExtractsRangeLocation(t *testing.T) {
	t.Parallel()
	p := NewSnippetAttachment()

	// Range from line 2 to 4
	loc := rules.Location{
		File:  "file.txt",
		Start: rules.Position{Line: 2, Column: 0},
		End:   rules.Position{Line: 4, Column: 0},
	}

	violations := []rules.Violation{
		rules.NewViolation(loc, "test-rule", "test", rules.SeverityWarning),
	}

	ctx := NewContext(nil, config.Default(), map[string][]byte{
		"file.txt": []byte("line1\nline2\nline3\nline4\nline5\n"),
	})

	result := p.Process(violations, ctx)
	if len(result) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result))
	}

	// End.Column=0 means exclusive, so should be lines 2-3
	expected := "line2\nline3"
	if result[0].SourceCode != expected {
		t.Errorf("expected %q, got %q", expected, result[0].SourceCode)
	}
}

func TestSnippetAttachment_HandlesInvalidLineNumbers(t *testing.T) {
	t.Parallel()
	p := NewSnippetAttachment()

	// Line 0 and negative lines
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("file.txt", 0),
			"test-rule",
			"test",
			rules.SeverityWarning,
		),
		rules.NewViolation(
			rules.NewLineLocation("file.txt", -1),
			"test-rule2",
			"test",
			rules.SeverityWarning,
		),
	}

	ctx := NewContext(nil, config.Default(), map[string][]byte{
		"file.txt": []byte("line1\nline2\n"),
	})

	result := p.Process(violations, ctx)
	if len(result) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(result))
	}

	// Both should have empty snippets
	for i, v := range result {
		if v.SourceCode != "" {
			t.Errorf("violation[%d]: expected no snippet for invalid line, got %s", i, v.SourceCode)
		}
	}
}

func TestExtractSnippet_RangeWithColumn(t *testing.T) {
	t.Parallel()
	source := []byte("line1\nline2\nline3\nline4\n")
	sm := &mockSourceMap{source: source}

	// Range where End.Column > 0 means the end line is included
	loc := rules.Location{
		File:  "test.txt",
		Start: rules.Position{Line: 2, Column: 0},
		End:   rules.Position{Line: 4, Column: 5},
	}

	snippet := extractSnippet(sm, loc)
	expected := "line2\nline3\nline4"
	if snippet != expected {
		t.Errorf("expected %q, got %q", expected, snippet)
	}
}

func TestExtractSnippet_SingleLineRange(t *testing.T) {
	t.Parallel()
	source := []byte("line1\nline2\nline3\n")
	sm := &mockSourceMap{source: source}

	// Start and End on same line with Column=0
	loc := rules.Location{
		File:  "test.txt",
		Start: rules.Position{Line: 2, Column: 0},
		End:   rules.Position{Line: 2, Column: 0},
	}

	snippet := extractSnippet(sm, loc)
	// End.Column=0 on same line means just that line
	expected := "line2"
	if snippet != expected {
		t.Errorf("expected %q, got %q", expected, snippet)
	}
}

// mockSourceMap implements the interface needed by extractSnippet
type mockSourceMap struct {
	source []byte
}

func (m *mockSourceMap) Line(lineNum int) string {
	lines := splitLines(m.source)
	if lineNum < 0 || lineNum >= len(lines) {
		return ""
	}
	return lines[lineNum]
}

func (m *mockSourceMap) Snippet(startLine, endLine int) string {
	lines := splitLines(m.source)
	if startLine < 0 || startLine >= len(lines) || endLine >= len(lines) || startLine > endLine {
		return ""
	}
	var result []string
	for i := startLine; i <= endLine; i++ {
		result = append(result, lines[i])
	}
	return joinLines(result)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(lines[0])
	for i := 1; i < len(lines); i++ {
		b.WriteString("\n")
		b.WriteString(lines[i])
	}
	return b.String()
}

func splitLines(source []byte) []string {
	var lines []string
	start := 0
	for i, b := range source {
		if b == '\n' {
			lines = append(lines, string(source[start:i]))
			start = i + 1
		}
	}
	if start < len(source) {
		lines = append(lines, string(source[start:]))
	}
	return lines
}
