package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestUndefinedVarRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewUndefinedVarRule().Metadata())
}

func TestUndefinedVarRule_Check(t *testing.T) {
	t.Parallel()
	r := NewUndefinedVarRule()

	cases := []struct {
		name        string
		content     string
		wantCount   int
		wantMessage string
	}{
		{
			name: "warns for undefined var in COPY",
			content: `FROM alpine
COPY $foo .
`,
			wantCount:   1,
			wantMessage: "Usage of undefined variable '$foo'",
		},
		{
			name: "allows var after ARG declaration",
			content: `FROM alpine
ARG foo
COPY $foo .
`,
			wantCount: 0,
		},
		{
			name: "warns for undefined var in ARG default",
			content: `FROM alpine
ARG VERSION=$foo
`,
			wantCount:   1,
			wantMessage: "Usage of undefined variable '$foo'",
		},
		{
			name: "suggests for common typo",
			content: `FROM alpine
ENV PATH=$PAHT:/app/bin
`,
			wantCount:   1,
			wantMessage: "Usage of undefined variable '$PAHT' (did you mean $PATH?)",
		},
		{
			name: "inherits ENV from base stage",
			content: `FROM alpine AS base
ENV FOO=bar

FROM base
COPY $FOO .
`,
			wantCount: 0,
		},
		{
			name: "does not treat global ARG as defined in stage",
			content: `ARG foo=bar
FROM alpine
COPY $foo .
`,
			wantCount:   1,
			wantMessage: "Usage of undefined variable '$foo'",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tc.content)
			violations := r.Check(input)

			if len(violations) != tc.wantCount {
				t.Fatalf("got %d violations, want %d", len(violations), tc.wantCount)
			}

			if tc.wantMessage != "" {
				if tc.wantCount == 0 {
					t.Fatalf("test case expects no violations but has wantMessage=%q", tc.wantMessage)
				}
				if violations[0].Message != tc.wantMessage {
					t.Fatalf("expected message %q, got %q", tc.wantMessage, violations[0].Message)
				}
			}
		})
	}
}
