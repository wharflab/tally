package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestUndefinedVarRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewUndefinedVarRule().Metadata())
}

func TestUndefinedVarRule_Check(t *testing.T) {
	t.Parallel()
	r := NewUndefinedVarRule()

	cases := []struct {
		name         string
		content      string
		wantCount    int
		wantMessage  string   // checked against first violation
		wantMessages []string // checked against all violations in order
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
			name: "suggests closest match among multiple ARGs",
			content: `FROM alpine
ARG DIR_BINARIES=binaries/
ARG DIR_ASSETS=assets/
ARG DIR_CONFIG=config/
COPY $DIR_ASSET .
`,
			wantCount:   1,
			wantMessage: "Usage of undefined variable '$DIR_ASSET' (did you mean $DIR_ASSETS?)",
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
		{
			name: "multi-stage undefined vars across different targets",
			content: `FROM scratch AS first
COPY $foo .

FROM scratch AS second
COPY $bar .
`,
			wantCount: 2,
			wantMessages: []string{
				"Usage of undefined variable '$foo'",
				"Usage of undefined variable '$bar'",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tc.content)
			violations := r.Check(input)

			if len(violations) != tc.wantCount {
				t.Fatalf("got %d violations, want %d: %v", len(violations), tc.wantCount, violations)
			}

			if tc.wantMessage != "" {
				if violations[0].Message != tc.wantMessage {
					t.Fatalf("expected message %q, got %q", tc.wantMessage, violations[0].Message)
				}
			}

			for i, want := range tc.wantMessages {
				if i >= len(violations) {
					t.Errorf("violation[%d]: expected message %q, but no violation at this index", i, want)
					continue
				}
				if violations[i].Message != want {
					t.Errorf("violation[%d]: expected message %q, got %q", i, want, violations[i].Message)
				}
			}
		})
	}
}
