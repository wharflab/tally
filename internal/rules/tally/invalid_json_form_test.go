package tally

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestInvalidJSONFormRule_Metadata(t *testing.T) {
	t.Parallel()
	r := NewInvalidJSONFormRule()
	meta := r.Metadata()

	if meta.Code != InvalidJSONFormRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, InvalidJSONFormRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityError {
		t.Errorf("DefaultSeverity = %v, want Error", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want correctness", meta.Category)
	}
	if meta.DocURL == "" {
		t.Error("DocURL is empty")
	}
}

func TestInvalidJSONFormRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewInvalidJSONFormRule(), []testutil.RuleTestCase{
		// === No violations ===
		{
			Name:           "valid CMD JSON form",
			Content:        "FROM alpine:3.20\nCMD [\"bash\", \"-lc\", \"echo hi\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "valid ENTRYPOINT JSON form",
			Content:        "FROM alpine:3.20\nENTRYPOINT [\"/app\", \"--serve\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "valid SHELL JSON form",
			Content:        "FROM alpine:3.20\nSHELL [\"/bin/bash\", \"-c\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "valid RUN JSON form",
			Content:        "FROM alpine:3.20\nRUN [\"echo\", \"hello\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "valid VOLUME JSON form",
			Content:        "FROM alpine:3.20\nVOLUME [\"/data\", \"/logs\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "valid CMD shell form",
			Content:        "FROM alpine:3.20\nCMD echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "valid RUN shell form",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "RUN with bash [[ test",
			Content:        "FROM alpine:3.20\nRUN [[ -f /etc/hosts ]] && echo found\n",
			WantViolations: 0,
		},
		{
			Name:           "RUN with POSIX single-bracket test",
			Content:        "FROM alpine:3.20\nRUN [ -f /etc/hosts ] && echo found\n",
			WantViolations: 0,
		},
		{
			Name:           "valid empty JSON array",
			Content:        "FROM alpine:3.20\nCMD []\n",
			WantViolations: 0,
		},
		{
			Name:           "no JSON-form instructions",
			Content:        "FROM alpine:3.20\nWORKDIR /app\nENV FOO=bar\n",
			WantViolations: 0,
		},
		{
			Name:           "valid HEALTHCHECK CMD JSON form",
			Content:        "FROM alpine:3.20\nHEALTHCHECK CMD [\"curl\", \"-f\", \"http://localhost/\"]\n",
			WantViolations: 0,
		},
		{
			// Dockerfile syntax does NOT support inline comments. The # character
			// within an instruction is a literal, not a comment delimiter. Valid
			// JSON containing # should not be flagged.
			Name:           "valid JSON with hash in argument",
			Content:        "FROM alpine:3.20\nCMD [\"bash\", \"-c\", \"echo #hello\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "valid ONBUILD CMD JSON form",
			Content:        "FROM alpine:3.20\nONBUILD CMD [\"echo\", \"hello\"]\n",
			WantViolations: 0,
		},

		// === Violations ===
		{
			Name:           "CMD unquoted strings",
			Content:        "FROM alpine:3.20\nCMD [bash, -lc, \"echo hi\"]\n",
			WantViolations: 1,
			WantCodes:      []string{InvalidJSONFormRuleCode},
			WantMessages:   []string{"invalid JSON in exec-form arguments for CMD"},
		},
		{
			Name:           "CMD single quotes",
			Content:        "FROM alpine:3.20\nCMD ['bash', '-lc']\n",
			WantViolations: 1,
			WantCodes:      []string{InvalidJSONFormRuleCode},
		},
		{
			Name:           "CMD trailing comma",
			Content:        "FROM alpine:3.20\nCMD [\"bash\", \"-lc\",]\n",
			WantViolations: 1,
			WantCodes:      []string{InvalidJSONFormRuleCode},
		},
		{
			Name:           "ENTRYPOINT unquoted",
			Content:        "FROM alpine:3.20\nENTRYPOINT [/app]\n",
			WantViolations: 1,
			WantMessages:   []string{"ENTRYPOINT"},
		},
		{
			Name:           "RUN unquoted",
			Content:        "FROM alpine:3.20\nRUN [echo, hello]\n",
			WantViolations: 1,
			WantMessages:   []string{"RUN"},
		},
		// NOTE: SHELL with invalid JSON causes instructions.Parse to hard-fail
		// ("SHELL requires the arguments to be in JSON form"), so the rule
		// never runs for SHELL through the normal pipeline. The detection
		// logic is tested separately in TestInvalidJSONFormRule_ShellDirect.
		{
			Name:           "VOLUME unquoted",
			Content:        "FROM alpine:3.20\nVOLUME [/data, /logs]\n",
			WantViolations: 1,
			WantMessages:   []string{"VOLUME"},
		},
		{
			Name:           "ADD unquoted",
			Content:        "FROM alpine:3.20\nADD [src, dst]\n",
			WantViolations: 1,
			WantMessages:   []string{"ADD"},
		},
		{
			Name:           "COPY unquoted",
			Content:        "FROM alpine:3.20\nCOPY [src, dst]\n",
			WantViolations: 1,
			WantMessages:   []string{"COPY"},
		},
		{
			Name: "multiple invalid instructions",
			Content: "FROM alpine:3.20\n" +
				"CMD [bash, -lc]\n" +
				"ENTRYPOINT [/app]\n",
			WantViolations: 2,
		},
		{
			Name:           "ONBUILD CMD unquoted",
			Content:        "FROM alpine:3.20\nONBUILD CMD [bash, -lc]\n",
			WantViolations: 1,
			WantMessages:   []string{"CMD"},
		},
		{
			Name:           "ONBUILD ENTRYPOINT unquoted",
			Content:        "FROM alpine:3.20\nONBUILD ENTRYPOINT [/app]\n",
			WantViolations: 1,
			WantMessages:   []string{"ENTRYPOINT"},
		},
		{
			Name:           "HEALTHCHECK CMD unquoted",
			Content:        "FROM alpine:3.20\nHEALTHCHECK CMD [curl, -f, http://localhost/]\n",
			WantViolations: 1,
			WantMessages:   []string{"HEALTHCHECK CMD"},
		},
		{
			Name:           "HEALTHCHECK with flags CMD unquoted",
			Content:        "FROM alpine:3.20\nHEALTHCHECK --interval=30s CMD [curl, -f, http://localhost/]\n",
			WantViolations: 1,
			WantMessages:   []string{"HEALTHCHECK CMD"},
		},
		{
			Name:           "ONBUILD non-JSON instruction — no violation",
			Content:        "FROM alpine:3.20\nONBUILD WORKDIR /app\n",
			WantViolations: 0,
		},
		{
			Name:           "HEALTHCHECK NONE — no violation",
			Content:        "FROM alpine:3.20\nHEALTHCHECK NONE\n",
			WantViolations: 0,
		},
	})
}

func TestInvalidJSONFormRule_ViolationLocation(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nCMD [bash, -lc, \"echo hi\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewInvalidJSONFormRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	// CMD is on line 2 (1-based).
	if violations[0].Location.Start.Line != 2 {
		t.Errorf("Start.Line = %d, want 2", violations[0].Location.Start.Line)
	}
}

func TestInvalidJSONFormRule_SuggestedFix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantNewText string
	}{
		{
			name:        "fix unquoted strings",
			content:     "FROM alpine:3.20\nCMD [bash, -lc, \"echo hi\"]\n",
			wantNewText: `["bash", "-lc", "echo hi"]`,
		},
		{
			name:        "fix single quotes",
			content:     "FROM alpine:3.20\nENTRYPOINT ['/app', '--serve']\n",
			wantNewText: `["/app", "--serve"]`,
		},
		{
			name:        "fix trailing comma",
			content:     "FROM alpine:3.20\nCMD [\"bash\", \"-lc\",]\n",
			wantNewText: `["bash", "-lc"]`,
		},
		{
			name:        "fix all unquoted paths",
			content:     "FROM alpine:3.20\nRUN [/bin/echo, hello]\n",
			wantNewText: `["/bin/echo", "hello"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			r := NewInvalidJSONFormRule()
			violations := r.Check(input)

			if len(violations) != 1 {
				t.Fatalf("expected 1 violation, got %d", len(violations))
			}

			v := violations[0]
			if v.SuggestedFix == nil {
				t.Fatal("expected a SuggestedFix")
			}
			if v.SuggestedFix.Safety != rules.FixSuggestion {
				t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
			}
			if !v.SuggestedFix.IsPreferred {
				t.Error("expected IsPreferred to be true")
			}
			if len(v.SuggestedFix.Edits) != 1 {
				t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
			}

			edit := v.SuggestedFix.Edits[0]
			if edit.NewText != tt.wantNewText {
				t.Errorf("NewText = %q, want %q", edit.NewText, tt.wantNewText)
			}
		})
	}
}

// TestInvalidJSONFormRule_ShellDirect tests SHELL detection directly because
// SHELL with invalid JSON causes instructions.Parse to hard-fail, so it can't
// be tested through the normal MakeLintInput pipeline. This tests that the
// detection logic itself works correctly for SHELL instructions.
func TestInvalidJSONFormRule_ShellDirect(t *testing.T) {
	t.Parallel()

	// Build a minimal AST manually to bypass instructions.Parse.
	content := "FROM alpine:3.20\nSHELL [/bin/bash, -c]\n"
	ast, err := parser.Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parser.Parse failed: %v", err)
	}

	input := rules.LintInput{
		File:   "Dockerfile",
		AST:    ast,
		Source: []byte(content),
	}

	r := NewInvalidJSONFormRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if !strings.Contains(violations[0].Message, "SHELL requires valid JSON exec-form") {
		t.Errorf("message = %q, want substring %q",
			violations[0].Message, "SHELL requires valid JSON exec-form")
	}

	// Fix should also be present.
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix for SHELL")
	}
	if violations[0].SuggestedFix.Edits[0].NewText != `["/bin/bash", "-c"]` {
		t.Errorf("fix NewText = %q, want %q",
			violations[0].SuggestedFix.Edits[0].NewText, `["/bin/bash", "-c"]`)
	}
}

func TestInvalidJSONFormRule_NilAST(t *testing.T) {
	t.Parallel()
	r := NewInvalidJSONFormRule()

	violations := r.Check(rules.LintInput{})
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for nil AST, got %d", len(violations))
	}
}

func TestExtractArgsText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		original string
		want     string
	}{
		{"CMD [bash, -lc]", "[bash, -lc]"},
		{"cmd [bash, -lc]", "[bash, -lc]"},
		{"RUN --mount=type=cache [echo, hi]", "[echo, hi]"},
		{"CMD echo hello", "echo hello"},
		{"SHELL [/bin/bash, -c]", "[/bin/bash, -c]"},
		{"COPY --chown=user:group [src, dst]", "[src, dst]"},
		{"CMD", ""},
		{"RUN --mount=type=cache", ""},
	}

	for _, tt := range tests {
		t.Run(tt.original, func(t *testing.T) {
			t.Parallel()
			got := extractArgsText(tt.original)
			if got != tt.want {
				t.Errorf("extractArgsText(%q) = %q, want %q", tt.original, got, tt.want)
			}
		})
	}
}

func TestExtractHealthcheckArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		original string
		want     string
	}{
		{"HEALTHCHECK CMD [curl, -f]", "[curl, -f]"},
		{"HEALTHCHECK --interval=30s CMD [curl, -f]", "[curl, -f]"},
		{"HEALTHCHECK --interval=30s --timeout=5s CMD [curl]", "[curl]"},
		{"HEALTHCHECK NONE", ""},
	}

	for _, tt := range tests {
		t.Run(tt.original, func(t *testing.T) {
			t.Parallel()
			got := extractHealthcheckArgs(tt.original)
			if got != tt.want {
				t.Errorf("extractHealthcheckArgs(%q) = %q, want %q", tt.original, got, tt.want)
			}
		})
	}
}

func TestExtractOnbuildArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		original   string
		subKeyword string
		want       string
	}{
		{"ONBUILD CMD [bash, -lc]", "cmd", "[bash, -lc]"},
		{"ONBUILD ENTRYPOINT [/app]", "entrypoint", "[/app]"},
		{"onbuild cmd [bash]", "cmd", "[bash]"},
	}

	for _, tt := range tests {
		t.Run(tt.original, func(t *testing.T) {
			t.Parallel()
			got := extractOnbuildArgs(tt.original, tt.subKeyword)
			if got != tt.want {
				t.Errorf("extractOnbuildArgs(%q, %q) = %q, want %q",
					tt.original, tt.subKeyword, got, tt.want)
			}
		})
	}
}

func TestTryRepairJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{
			name:   "unquoted strings",
			input:  `[bash, -lc, "echo hi"]`,
			want:   `["bash", "-lc", "echo hi"]`,
			wantOK: true,
		},
		{
			name:   "single quotes",
			input:  `['bash', '-lc']`,
			want:   `["bash", "-lc"]`,
			wantOK: true,
		},
		{
			name:   "trailing comma",
			input:  `["bash", "-lc",]`,
			want:   `["bash", "-lc"]`,
			wantOK: true,
		},
		{
			name:   "mixed unquoted and quoted",
			input:  `[bash, "echo hi", -lc]`,
			want:   `["bash", "echo hi", "-lc"]`,
			wantOK: true,
		},
		{
			name:   "all unquoted paths",
			input:  `[/bin/bash, -c]`,
			want:   `["/bin/bash", "-c"]`,
			wantOK: true,
		},
		{
			name:   "empty brackets",
			input:  `[]`,
			want:   `[]`,
			wantOK: true,
		},
		{
			name:   "single-quoted with internal double quotes",
			input:  `['say "hello"']`,
			want:   `["say \"hello\""]`,
			wantOK: true,
		},
		{
			name:   "unquoted with backslash",
			input:  `[C:\path]`,
			want:   `["C:\\path"]`,
			wantOK: true,
		},
		{
			name:   "empty string element preserved",
			input:  `["", "b"]`,
			want:   `["", "b"]`,
			wantOK: true,
		},
		{
			name:   "not brackets",
			input:  `echo hello`,
			wantOK: false,
		},
		{
			name:   "no closing bracket",
			input:  `[bash`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tryRepairJSON(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("tryRepairJSON(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("tryRepairJSON(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitJSONElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  []string
	}{
		{`bash, -lc, "echo hi"`, []string{"bash", " -lc", ` "echo hi"`}},
		{`"a", "b"`, []string{`"a"`, ` "b"`}},
		{`'a', 'b'`, []string{`'a'`, ` 'b'`}},
		{`a`, []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := splitJSONElements(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitJSONElements(%q) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitJSONElements(%q)[%d] = %q, want %q",
						tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestEnsureDoubleQuoted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{`"already"`, `"already"`},
		{`'single'`, `"single"`},
		{`unquoted`, `"unquoted"`},
		{`/bin/bash`, `"/bin/bash"`},
		{`-c`, `"-c"`},
		{``, ``},
		{`'say "hello"'`, `"say \"hello\""`},
		{`C:\path`, `"C:\\path"`},
		{`""`, `""`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := ensureDoubleQuoted(tt.input)
			if got != tt.want {
				t.Errorf("ensureDoubleQuoted(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
