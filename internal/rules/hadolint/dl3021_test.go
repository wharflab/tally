package hadolint

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3021Rule_Metadata(t *testing.T) {
	r := NewDL3021Rule()
	meta := r.Metadata()

	if meta.Code != rules.HadolintRulePrefix+"DL3021" {
		t.Errorf("Code = %q, want %q", meta.Code, rules.HadolintRulePrefix+"DL3021")
	}
	if meta.DefaultSeverity != rules.SeverityError {
		t.Errorf("DefaultSeverity = %v, want %v", meta.DefaultSeverity, rules.SeverityError)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want %q", meta.Category, "correctness")
	}
}

func TestDL3021Rule_Check(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Original Hadolint test cases
		{
			name: "no warn on 2 args",
			dockerfile: `FROM alpine
COPY foo bar
`,
			wantCount: 0,
		},
		{
			name: "warn on 3 args",
			dockerfile: `FROM alpine
COPY foo bar baz
`,
			wantCount: 1,
		},
		{
			name: "no warn on 3 args with trailing slash",
			dockerfile: `FROM alpine
COPY foo bar baz/
`,
			wantCount: 0,
		},
		{
			name: "warn on 3 args with quotes",
			dockerfile: `FROM alpine
COPY foo bar "baz"
`,
			wantCount: 1,
		},
		{
			name: "no warn on 3 args with quotes and trailing slash",
			dockerfile: `FROM alpine
COPY foo bar "baz/"
`,
			wantCount: 0,
		},
		// Additional edge cases
		{
			name: "no warn on single source",
			dockerfile: `FROM alpine
COPY src /dest
`,
			wantCount: 0,
		},
		{
			name: "warn on 4 args without trailing slash",
			dockerfile: `FROM alpine
COPY a b c dest
`,
			wantCount: 1,
		},
		{
			name: "no warn on 4 args with trailing slash",
			dockerfile: `FROM alpine
COPY a b c dest/
`,
			wantCount: 0,
		},
		{
			name: "multiple COPY instructions",
			dockerfile: `FROM alpine
COPY a b c dest
COPY x y z dir/
COPY p q r target
`,
			wantCount: 2,
		},
		{
			name: "COPY with --from does not exempt from rule",
			dockerfile: `FROM alpine AS builder
COPY a b c dest

FROM alpine
COPY --from=builder a b c /dest
`,
			wantCount: 2,
		},
		{
			name: "single source with dest ending in slash is fine",
			dockerfile: `FROM alpine
COPY src /dest/
`,
			wantCount: 0,
		},
		{
			name: "no warn with single quoted destination",
			dockerfile: `FROM alpine
COPY foo bar 'baz/'
`,
			wantCount: 0,
		},
		{
			name: "warn with single quoted destination without slash",
			dockerfile: `FROM alpine
COPY foo bar 'baz'
`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3021Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}

			// Verify rule code for violations
			for _, v := range violations {
				if v.RuleCode != rules.HadolintRulePrefix+"DL3021" {
					t.Errorf("RuleCode = %q, want %q", v.RuleCode, rules.HadolintRulePrefix+"DL3021")
				}
			}
		})
	}
}

