package directive

import (
	"math"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

func TestParseTallyNextLine(t *testing.T) {
	content := `# tally ignore=DL3006
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Type != TypeNextLine {
		t.Errorf("expected TypeNextLine, got %v", d.Type)
	}
	if len(d.Rules) != 1 || d.Rules[0] != "DL3006" {
		t.Errorf("expected [DL3006], got %v", d.Rules)
	}
	if d.Source != SourceTally {
		t.Errorf("expected SourceTally, got %v", d.Source)
	}
	if d.AppliesTo.Start != 1 || d.AppliesTo.End != 1 {
		t.Errorf("expected AppliesTo {1, 1}, got %v", d.AppliesTo)
	}
}

func TestParseTallyMultipleRules(t *testing.T) {
	content := `# tally ignore=DL3006,DL3008,max-lines
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if len(d.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(d.Rules))
	}
	expected := []string{"DL3006", "DL3008", "max-lines"}
	for i, r := range expected {
		if d.Rules[i] != r {
			t.Errorf("expected rule %d to be %s, got %s", i, r, d.Rules[i])
		}
	}
}

func TestParseTallyGlobal(t *testing.T) {
	content := `# tally global ignore=max-lines
FROM alpine
RUN echo hello`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Type != TypeGlobal {
		t.Errorf("expected TypeGlobal, got %v", d.Type)
	}
	if d.AppliesTo.Start != 0 || d.AppliesTo.End != math.MaxInt {
		t.Errorf("expected global range, got %v", d.AppliesTo)
	}
}

func TestParseHadolint(t *testing.T) {
	content := `# hadolint ignore=DL3006
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Source != SourceHadolint {
		t.Errorf("expected SourceHadolint, got %v", d.Source)
	}
	if d.Type != TypeNextLine {
		t.Errorf("expected TypeNextLine, got %v", d.Type)
	}
}

func TestParseHadolintGlobal(t *testing.T) {
	content := `# hadolint global ignore=DL3006,DL3008
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Source != SourceHadolint {
		t.Errorf("expected SourceHadolint, got %v", d.Source)
	}
	if d.Type != TypeGlobal {
		t.Errorf("expected TypeGlobal, got %v", d.Type)
	}
}

func TestParseBuildx(t *testing.T) {
	content := `# check=skip=DL3006,DL3008
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Source != SourceBuildx {
		t.Errorf("expected SourceBuildx, got %v", d.Source)
	}
	if d.Type != TypeGlobal {
		t.Errorf("buildx directives should always be global, got %v", d.Type)
	}
}

func TestParseIgnoreAll(t *testing.T) {
	content := `# tally ignore=all
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if !d.SuppressesRule("any-rule") {
		t.Error("ignore=all should suppress any rule")
	}
}

func TestParseCaseInsensitive(t *testing.T) {
	content := `# TALLY IGNORE=DL3006
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Errorf("expected 1 directive, got %d", len(result.Directives))
	}
}

func TestParseWithSpaces(t *testing.T) {
	content := `#  tally   ignore=DL3006,DL3008
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if len(d.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(d.Rules))
	}
}

func TestParseDirectiveAtEOF(t *testing.T) {
	content := `FROM ubuntu
# tally ignore=DL3006`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	// AppliesTo should be {-1, -1} - no valid target
	if d.AppliesTo.Start != -1 || d.AppliesTo.End != -1 {
		t.Errorf("directive at EOF should have no target, got %v", d.AppliesTo)
	}
}

func TestParseMultipleDirectives(t *testing.T) {
	content := `# tally global ignore=max-lines
# hadolint ignore=DL3006
FROM ubuntu
# tally ignore=DL3008
RUN echo hello`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 3 {
		t.Errorf("expected 3 directives, got %d", len(result.Directives))
	}
}

func TestParseRegularComment(t *testing.T) {
	content := `# This is a regular comment`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 0 {
		t.Errorf("expected 0 directives, got %d", len(result.Directives))
	}
}

func TestParseSkipsBlankLinesAndComments(t *testing.T) {
	content := `# tally ignore=DL3006

# another comment
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	// Should apply to line 3 (0-based), the FROM line
	if d.AppliesTo.Start != 3 || d.AppliesTo.End != 3 {
		t.Errorf("expected AppliesTo {3, 3}, got %v", d.AppliesTo)
	}
}

func TestParseEmptyRuleList(t *testing.T) {
	// Test tally format with empty rule list (only commas)
	content := `# tally ignore=,
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Message != "empty rule list" {
		t.Errorf("expected 'empty rule list' error, got %q", result.Errors[0].Message)
	}
}

func TestParseEmptyRuleListHadolint(t *testing.T) {
	// Test hadolint format with empty rule list
	content := `# hadolint ignore=,,
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Message != "empty rule list" {
		t.Errorf("expected 'empty rule list' error, got %q", result.Errors[0].Message)
	}
}

func TestParseEmptyRuleListBuildx(t *testing.T) {
	// Test buildx format with empty rule list
	content := `# check=skip=,
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Message != "empty rule list" {
		t.Errorf("expected 'empty rule list' error, got %q", result.Errors[0].Message)
	}
}

func TestParseRuleListError(t *testing.T) {
	// Directly test parseRuleList error cases
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"", true},         // empty string
		{",", true},        // only comma
		{",,", true},       // only commas
		{", , ,", true},    // commas with spaces
		{"DL3006", false},  // valid single rule
		{"DL3006,", false}, // trailing comma is OK (rule is extracted)
		{",DL3006", false}, // leading comma is OK (rule is extracted)
		{"a,b,c", false},   // multiple rules
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseRuleList(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRuleList(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			// Verify error message if error expected
			if tt.wantErr && err != nil {
				if err.Error() != "empty rule list" {
					t.Errorf("expected 'empty rule list', got %q", err.Error())
				}
			}
		})
	}
}

func TestParseWithValidation(t *testing.T) {
	knownRules := map[string]bool{
		"DL3006":    true,
		"max-lines": true,
	}
	validator := func(code string) bool {
		return knownRules[code]
	}

	tests := []struct {
		name       string
		content    string
		wantErrors int
	}{
		{
			name: "valid rule codes",
			content: `# tally ignore=DL3006,max-lines
FROM ubuntu`,
			wantErrors: 0,
		},
		{
			name: "unknown rule codes",
			content: `# tally ignore=UNKNOWN,max-lines
FROM ubuntu`,
			wantErrors: 1,
		},
		{
			name: "multiple unknown codes",
			content: `# tally ignore=UNKNOWN1,UNKNOWN2
FROM ubuntu`,
			wantErrors: 1, // Single error with multiple codes listed
		},
		{
			name: "all is always valid",
			content: `# tally ignore=all
FROM ubuntu`,
			wantErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := sourcemap.New([]byte(tt.content))
			result := Parse(sm, validator)

			if len(result.Errors) != tt.wantErrors {
				t.Errorf("expected %d errors, got %d: %v",
					tt.wantErrors, len(result.Errors), result.Errors)
			}
		})
	}
}

func TestFilterSuppressSingle(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 2),
			"DL3006", "test", rules.SeverityWarning,
		),
	}
	directives := []Directive{
		{
			Type:      TypeNextLine,
			Rules:     []string{"DL3006"},
			Line:      0,
			AppliesTo: LineRange{Start: 1, End: 1},
		},
	}

	result := Filter(violations, directives)

	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
	if len(result.Suppressed) != 1 {
		t.Errorf("expected 1 suppressed, got %d", len(result.Suppressed))
	}
	if len(result.UnusedDirectives) != 0 {
		t.Errorf("expected 0 unused, got %d", len(result.UnusedDirectives))
	}
}

func TestFilterSuppressAll(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 2),
			"DL3006", "test", rules.SeverityWarning,
		),
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 2),
			"DL3008", "test", rules.SeverityWarning,
		),
	}
	directives := []Directive{
		{
			Type:      TypeNextLine,
			Rules:     []string{"all"},
			Line:      0,
			AppliesTo: LineRange{Start: 1, End: 1},
		},
	}

	result := Filter(violations, directives)

	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
	if len(result.Suppressed) != 2 {
		t.Errorf("expected 2 suppressed, got %d", len(result.Suppressed))
	}
}

func TestFilterGlobalDirective(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 1),
			"DL3006", "test", rules.SeverityWarning,
		),
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 5),
			"DL3006", "test", rules.SeverityWarning,
		),
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 100),
			"DL3006", "test", rules.SeverityWarning,
		),
	}
	directives := []Directive{
		{
			Type:      TypeGlobal,
			Rules:     []string{"DL3006"},
			Line:      0,
			AppliesTo: GlobalRange(),
		},
	}

	result := Filter(violations, directives)

	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
	if len(result.Suppressed) != 3 {
		t.Errorf("expected 3 suppressed, got %d", len(result.Suppressed))
	}
}

func TestFilterNextLineOnlyAffectsOneLine(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 2),
			"DL3006", "test", rules.SeverityWarning,
		),
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 3),
			"DL3006", "test", rules.SeverityWarning,
		),
	}
	directives := []Directive{
		{
			Type:      TypeNextLine,
			Rules:     []string{"DL3006"},
			Line:      0,
			AppliesTo: LineRange{Start: 1, End: 1},
		},
	}

	result := Filter(violations, directives)

	if len(result.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(result.Violations))
	}
	if len(result.Suppressed) != 1 {
		t.Errorf("expected 1 suppressed, got %d", len(result.Suppressed))
	}
}

func TestFilterUnusedDirective(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 2),
			"DL3006", "test", rules.SeverityWarning,
		),
	}
	directives := []Directive{
		{
			Type:      TypeNextLine,
			Rules:     []string{"DL3008"}, // Different rule
			Line:      0,
			AppliesTo: LineRange{Start: 1, End: 1},
		},
	}

	result := Filter(violations, directives)

	if len(result.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(result.Violations))
	}
	if len(result.UnusedDirectives) != 1 {
		t.Errorf("expected 1 unused directive, got %d", len(result.UnusedDirectives))
	}
}

func TestFilterNoDirectives(t *testing.T) {
	violations := []rules.Violation{
		rules.NewViolation(
			rules.NewLineLocation("Dockerfile", 1),
			"DL3006", "test", rules.SeverityWarning,
		),
	}
	result := Filter(violations, []Directive{})

	if len(result.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(result.Violations))
	}
}

func TestFilterNoViolations(t *testing.T) {
	directives := []Directive{
		{Type: TypeGlobal, Rules: []string{"DL3006"}, AppliesTo: GlobalRange()},
	}
	result := Filter([]rules.Violation{}, directives)

	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
	if len(result.UnusedDirectives) != 1 {
		t.Errorf("expected 1 unused directive, got %d", len(result.UnusedDirectives))
	}
}

func TestDirectiveType_String(t *testing.T) {
	tests := []struct {
		typ  DirectiveType
		want string
	}{
		{TypeNextLine, "next-line"},
		{TypeGlobal, "global"},
		{DirectiveType(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.typ.String()
		if got != tt.want {
			t.Errorf("DirectiveType(%d).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestLineRange_Contains(t *testing.T) {
	tests := []struct {
		name   string
		r      LineRange
		line   int
		expect bool
	}{
		{"single line - match", LineRange{5, 5}, 5, true},
		{"single line - no match", LineRange{5, 5}, 6, false},
		{"range - within", LineRange{5, 10}, 7, true},
		{"range - start boundary", LineRange{5, 10}, 5, true},
		{"range - end boundary", LineRange{5, 10}, 10, true},
		{"range - before", LineRange{5, 10}, 4, false},
		{"range - after", LineRange{5, 10}, 11, false},
		{"global range", GlobalRange(), 1000000, true},
		{"invalid range", LineRange{-1, -1}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Contains(tt.line)
			if got != tt.expect {
				t.Errorf("LineRange{%d,%d}.Contains(%d) = %v, want %v",
					tt.r.Start, tt.r.End, tt.line, got, tt.expect)
			}
		})
	}
}

func TestParseTallyWithReason(t *testing.T) {
	content := `# tally ignore=DL3006;reason=Legacy base image required
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Reason != "Legacy base image required" {
		t.Errorf("expected reason 'Legacy base image required', got %q", d.Reason)
	}
}

func TestParseTallyGlobalWithReason(t *testing.T) {
	content := `# tally global ignore=max-lines;reason=Generated file, size is expected
FROM alpine`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Reason != "Generated file, size is expected" {
		t.Errorf("expected reason 'Generated file, size is expected', got %q", d.Reason)
	}
	if d.Type != TypeGlobal {
		t.Errorf("expected TypeGlobal, got %v", d.Type)
	}
}

func TestParseHadolintWithReason(t *testing.T) {
	content := `# hadolint ignore=DL3006;reason=Using older ubuntu for compatibility
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Source != SourceHadolint {
		t.Errorf("expected SourceHadolint, got %v", d.Source)
	}
	if d.Reason != "Using older ubuntu for compatibility" {
		t.Errorf("expected reason, got %q", d.Reason)
	}
}

func TestParseBuildxWithReason(t *testing.T) {
	// ;reason= is a tally extension for buildx format
	content := `# check=skip=DL3006;reason=BuildKit silently ignores this
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Source != SourceBuildx {
		t.Errorf("expected SourceBuildx, got %v", d.Source)
	}
	if d.Reason != "BuildKit silently ignores this" {
		t.Errorf("expected reason 'BuildKit silently ignores this', got %q", d.Reason)
	}
}

func TestParseBuildxWithoutReason(t *testing.T) {
	content := `# check=skip=DL3006
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Reason != "" {
		t.Errorf("expected empty reason, got %q", d.Reason)
	}
}

func TestParseTallyWithoutReason(t *testing.T) {
	content := `# tally ignore=DL3006
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Reason != "" {
		t.Errorf("expected empty reason, got %q", d.Reason)
	}
}

func TestParseReasonCaseInsensitive(t *testing.T) {
	content := `# tally ignore=DL3006;REASON=Some reason here
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	if d.Reason != "Some reason here" {
		t.Errorf("expected reason 'Some reason here', got %q", d.Reason)
	}
}

func TestParseReasonWithSpecialChars(t *testing.T) {
	content := `# tally ignore=DL3006;reason=This is a reason with: colons, commas, and (parentheses)!
FROM ubuntu`
	sm := sourcemap.New([]byte(content))
	result := Parse(sm, nil)

	if len(result.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(result.Directives))
	}
	d := result.Directives[0]
	expected := "This is a reason with: colons, commas, and (parentheses)!"
	if d.Reason != expected {
		t.Errorf("expected reason %q, got %q", expected, d.Reason)
	}
}

func TestParseRulesWithSpacesAroundCommas(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name: "space after comma",
			content: `# tally ignore=DL3006, DL3008
FROM ubuntu`,
			expected: []string{"DL3006", "DL3008"},
		},
		{
			name: "space before comma",
			content: `# tally ignore=DL3006 ,DL3008
FROM ubuntu`,
			expected: []string{"DL3006", "DL3008"},
		},
		{
			name: "spaces around comma",
			content: `# tally ignore=DL3006 , DL3008
FROM ubuntu`,
			expected: []string{"DL3006", "DL3008"},
		},
		{
			name: "multiple rules with spaces",
			content: `# hadolint ignore=DL3006, DL3008, DL3009
FROM ubuntu`,
			expected: []string{"DL3006", "DL3008", "DL3009"},
		},
		{
			name: "buildx with spaces",
			content: `# check=skip=DL3006, DL3008
FROM ubuntu`,
			expected: []string{"DL3006", "DL3008"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := sourcemap.New([]byte(tt.content))
			result := Parse(sm, nil)

			if len(result.Directives) != 1 {
				t.Fatalf("expected 1 directive, got %d", len(result.Directives))
			}
			d := result.Directives[0]
			if len(d.Rules) != len(tt.expected) {
				t.Errorf("expected %d rules, got %d: %v", len(tt.expected), len(d.Rules), d.Rules)
				return
			}
			for i, rule := range tt.expected {
				if d.Rules[i] != rule {
					t.Errorf("expected rule %d to be %q, got %q", i, rule, d.Rules[i])
				}
			}
		})
	}
}

func TestParseShellDirective(t *testing.T) {
	tests := []struct {
		name    string
		content string
		shell   string
		source  DirectiveSource
	}{
		{
			name:    "tally shell bash",
			content: "# tally shell=bash\nFROM ubuntu",
			shell:   "bash",
			source:  SourceTally,
		},
		{
			name:    "hadolint shell dash",
			content: "# hadolint shell=dash\nFROM alpine",
			shell:   "dash",
			source:  SourceHadolint,
		},
		{
			name:    "tally shell with path",
			content: "# tally shell=/bin/sh\nFROM ubuntu",
			shell:   "/bin/sh",
			source:  SourceTally,
		},
		{
			name:    "case insensitive",
			content: "# TALLY SHELL=bash\nFROM ubuntu",
			shell:   "bash",
			source:  SourceTally,
		},
		{
			name:    "with extra spaces",
			content: "#  tally   shell = bash\nFROM ubuntu",
			shell:   "bash",
			source:  SourceTally,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := sourcemap.New([]byte(tt.content))
			result := Parse(sm, nil)

			if len(result.ShellDirectives) != 1 {
				t.Fatalf("expected 1 shell directive, got %d", len(result.ShellDirectives))
			}
			sd := result.ShellDirectives[0]
			if sd.Shell != tt.shell {
				t.Errorf("shell = %q, want %q", sd.Shell, tt.shell)
			}
			if sd.Source != tt.source {
				t.Errorf("source = %v, want %v", sd.Source, tt.source)
			}
		})
	}
}

func TestParseShellDirectiveNoMatch(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "ignore directive not shell",
			content: "# tally ignore=DL3006\nFROM ubuntu",
		},
		{
			name:    "regular comment with shell word",
			content: "# The shell used here is bash\nFROM ubuntu",
		},
		{
			name:    "incomplete directive",
			content: "# tally shell\nFROM ubuntu",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := sourcemap.New([]byte(tt.content))
			result := Parse(sm, nil)

			if len(result.ShellDirectives) != 0 {
				t.Errorf("expected 0 shell directives, got %d", len(result.ShellDirectives))
			}
		})
	}
}
