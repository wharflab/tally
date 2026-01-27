package reporter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestPrintTextPlain_SingleViolation(t *testing.T) {
	source := []byte("FROM alpine\nRUN echo hello\nCMD [\"sh\"]")
	violations := []rules.Violation{
		{
			Location: rules.NewRangeLocation("Dockerfile", 1, 0, 1, 14),
			RuleCode: "TestRule",
			Message:  "Test message",
			Severity: rules.SeverityWarning,
			DocURL:   "https://example.com/rule",
		},
	}
	sources := map[string][]byte{
		"Dockerfile": source,
	}

	var buf bytes.Buffer
	err := PrintTextPlain(&buf, violations, sources)
	if err != nil {
		t.Fatalf("PrintTextPlain failed: %v", err)
	}

	output := buf.String()

	// Check header format (uses severity label)
	if !strings.Contains(output, "WARNING: TestRule") {
		t.Errorf("Missing warning header, got:\n%s", output)
	}
	if !strings.Contains(output, "https://example.com/rule") {
		t.Errorf("Missing URL, got:\n%s", output)
	}
	if !strings.Contains(output, "Test message") {
		t.Errorf("Missing message, got:\n%s", output)
	}

	// Check snippet format
	if !strings.Contains(output, "Dockerfile:2") {
		t.Errorf("Missing file:line header, got:\n%s", output)
	}
	if !strings.Contains(output, "--------------------") {
		t.Errorf("Missing separator, got:\n%s", output)
	}
	if !strings.Contains(output, ">>>") {
		t.Errorf("Missing line marker, got:\n%s", output)
	}
}

func TestPrintTextPlain_DifferentSeverities(t *testing.T) {
	source := []byte("FROM alpine")
	tests := []struct {
		severity rules.Severity
		want     string
	}{
		{rules.SeverityError, "ERROR:"},
		{rules.SeverityWarning, "WARNING:"},
		{rules.SeverityInfo, "INFO:"},
		{rules.SeverityStyle, "STYLE:"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			violations := []rules.Violation{
				{
					Location: rules.NewLineLocation("Dockerfile", 0),
					RuleCode: "TestRule",
					Message:  "Test",
					Severity: tt.severity,
				},
			}
			sources := map[string][]byte{"Dockerfile": source}

			var buf bytes.Buffer
			err := PrintTextPlain(&buf, violations, sources)
			if err != nil {
				t.Fatalf("PrintTextPlain failed: %v", err)
			}

			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("Expected %q in output, got:\n%s", tt.want, buf.String())
			}
		})
	}
}

func TestPrintTextPlain_NoURL(t *testing.T) {
	source := []byte("FROM alpine\nRUN echo hello")
	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 0),
			RuleCode: "TestRule",
			Message:  "Test message",
			Severity: rules.SeverityWarning,
			// No DocURL
		},
	}
	sources := map[string][]byte{
		"Dockerfile": source,
	}

	var buf bytes.Buffer
	err := PrintTextPlain(&buf, violations, sources)
	if err != nil {
		t.Fatalf("PrintTextPlain failed: %v", err)
	}

	output := buf.String()

	// Should have rule name but no URL (check no " - " after rule code on same line)
	if !strings.Contains(output, "WARNING: TestRule\n") {
		t.Errorf("Expected 'WARNING: TestRule\\n' (no URL), got:\n%s", output)
	}
}

func TestPrintTextPlain_FileLevel(t *testing.T) {
	source := []byte("FROM alpine")
	violations := []rules.Violation{
		{
			Location: rules.NewFileLocation("Dockerfile"),
			RuleCode: "TestRule",
			Message:  "File-level issue",
			Severity: rules.SeverityWarning,
		},
	}
	sources := map[string][]byte{
		"Dockerfile": source,
	}

	var buf bytes.Buffer
	err := PrintTextPlain(&buf, violations, sources)
	if err != nil {
		t.Fatalf("PrintTextPlain failed: %v", err)
	}

	output := buf.String()

	// Should have warning but no snippet
	if !strings.Contains(output, "WARNING: TestRule") {
		t.Errorf("Missing warning, got:\n%s", output)
	}
	// Should NOT have separator (no snippet for file-level)
	if strings.Contains(output, "--------------------") {
		t.Errorf("File-level violation should not have snippet, got:\n%s", output)
	}
}

func TestPrintTextPlain_Sorted(t *testing.T) {
	source := []byte("line1\nline2\nline3\nline4\nline5")
	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("b.dockerfile", 2),
			RuleCode: "Rule2",
			Message:  "Second file",
			Severity: rules.SeverityWarning,
		},
		{
			Location: rules.NewLineLocation("a.dockerfile", 4),
			RuleCode: "Rule3",
			Message:  "First file, later line",
			Severity: rules.SeverityWarning,
		},
		{
			Location: rules.NewLineLocation("a.dockerfile", 1),
			RuleCode: "Rule1",
			Message:  "First file, earlier line",
			Severity: rules.SeverityWarning,
		},
	}
	sources := map[string][]byte{
		"a.dockerfile": source,
		"b.dockerfile": source,
	}

	var buf bytes.Buffer
	err := PrintTextPlain(&buf, violations, sources)
	if err != nil {
		t.Fatalf("PrintTextPlain failed: %v", err)
	}

	output := buf.String()

	// Check order: Rule1 should come before Rule3 (same file, earlier line)
	// Rule1 and Rule3 should come before Rule2 (different file, alphabetically first)
	idx1 := strings.Index(output, "Rule1")
	idx3 := strings.Index(output, "Rule3")
	idx2 := strings.Index(output, "Rule2")

	if idx1 > idx3 {
		t.Errorf("Rule1 should come before Rule3, got:\n%s", output)
	}
	if idx3 > idx2 {
		t.Errorf("Rule3 should come before Rule2, got:\n%s", output)
	}
}

func TestPrintTextPlain_MultipleLines(t *testing.T) {
	source := []byte("FROM alpine\nRUN echo 1\nRUN echo 2\nRUN echo 3\nCMD [\"sh\"]")
	violations := []rules.Violation{
		{
			Location: rules.NewRangeLocation("Dockerfile", 1, 0, 3, 10),
			RuleCode: "MultiLine",
			Message:  "Spans multiple lines",
			Severity: rules.SeverityWarning,
		},
	}
	sources := map[string][]byte{
		"Dockerfile": source,
	}

	var buf bytes.Buffer
	err := PrintTextPlain(&buf, violations, sources)
	if err != nil {
		t.Fatalf("PrintTextPlain failed: %v", err)
	}

	output := buf.String()

	// Should mark lines 2, 3, and 4 (1-based) with >>>
	lines := strings.Split(output, "\n")
	markedCount := 0
	for _, line := range lines {
		if strings.Contains(line, ">>>") {
			markedCount++
		}
	}

	if markedCount != 3 {
		t.Errorf("Expected 3 marked lines, got %d:\n%s", markedCount, output)
	}
}

func TestPrintTextPlain_Padding(t *testing.T) {
	// Test that we get context padding around the violation
	source := []byte("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8")
	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("test", 4), // Line 5 (0-based: 4)
			RuleCode: "Test",
			Message:  "Middle line",
			Severity: rules.SeverityWarning,
		},
	}
	sources := map[string][]byte{
		"test": source,
	}

	var buf bytes.Buffer
	err := PrintTextPlain(&buf, violations, sources)
	if err != nil {
		t.Fatalf("PrintTextPlain failed: %v", err)
	}

	output := buf.String()

	// Should show context lines around line 5
	// With padding of 4 for single-line violations, should see lines 3-7 or similar
	if !strings.Contains(output, "line3") || !strings.Contains(output, "line7") {
		t.Errorf("Missing context padding, got:\n%s", output)
	}
}

func TestLineInRange(t *testing.T) {
	tests := []struct {
		line, start, end int
		want             bool
	}{
		{5, 3, 7, true},  // In range
		{3, 3, 7, true},  // At start
		{7, 3, 7, true},  // At end
		{2, 3, 7, false}, // Before
		{8, 3, 7, false}, // After
		{5, 5, 5, true},  // Single line
		{7, 7, 3, true},  // Inverted range (7,3): treated as point at start (7)
		{3, 7, 3, false}, // Line 3 not in inverted range (7,3) -> becomes (7,7)
	}

	for _, tt := range tests {
		got := lineInRange(tt.line, tt.start, tt.end)
		if got != tt.want {
			t.Errorf("lineInRange(%d, %d, %d) = %v, want %v", tt.line, tt.start, tt.end, got, tt.want)
		}
	}
}

func TestNewTextReporter_Options(t *testing.T) {
	// Test with explicit options
	colorOn := true
	colorOff := false

	tests := []struct {
		name string
		opts TextOptions
	}{
		{"default", DefaultTextOptions()},
		{"color on", TextOptions{Color: &colorOn, SyntaxHighlight: true}},
		{"color off", TextOptions{Color: &colorOff, SyntaxHighlight: false}},
		{"custom style", TextOptions{SyntaxHighlight: true, ChromaStyle: "dracula"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewTextReporter(tt.opts)
			if r == nil {
				t.Fatal("NewTextReporter returned nil")
			}
		})
	}
}

func TestTextReporter_Print(t *testing.T) {
	source := []byte("FROM alpine\nRUN echo hello")
	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 0),
			RuleCode: "TestRule",
			Message:  "Test message",
			Severity: rules.SeverityError,
		},
	}
	sources := map[string][]byte{"Dockerfile": source}

	// Test with reporter instance
	r := NewTextReporter(DefaultTextOptions())
	var buf bytes.Buffer
	err := r.Print(&buf, violations, sources)
	if err != nil {
		t.Fatalf("Print failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "TestRule") {
		t.Errorf("Missing rule code in output:\n%s", output)
	}
}
