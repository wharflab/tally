package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestUndefinedArgInFromRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewUndefinedArgInFromRule().Metadata())
}

func TestUndefinedArgInFromRule_Check(t *testing.T) {
	t.Parallel()
	r := NewUndefinedArgInFromRule()

	cases := []struct {
		name        string
		content     string
		wantCount   int
		wantMessage string
	}{
		{
			name: "allows declared base arg with default",
			content: `ARG base=scratch
FROM $base
`,
			wantCount: 0,
		},
		{
			name: "allows declared BUILDPLATFORM arg",
			content: `ARG BUILDPLATFORM=linux/amd64
FROM --platform=$BUILDPLATFORM scratch
`,
			wantCount: 0,
		},
		{
			name: "allows automatic BUILDPLATFORM arg",
			content: `FROM --platform=$BUILDPLATFORM scratch
`,
			wantCount: 0,
		},
		{
			name: "allows declared arg used in base name",
			content: `ARG DEBUG
FROM scratch${DEBUG}
`,
			wantCount: 0,
		},
		{
			name: "allows default substitution expansion",
			content: `ARG DEBUG
FROM scra${DEBUG:-tch}
`,
			wantCount: 0,
		},
		{
			name: "allows alternate value expansion",
			content: `ARG DEBUG=""
FROM scratch${DEBUG-@bogus}
`,
			wantCount: 0,
		},
		{
			name: "warns for unknown platform arg with suggestion",
			content: `FROM --platform=$BULIDPLATFORM scratch
`,
			wantCount:   1,
			wantMessage: "FROM argument 'BULIDPLATFORM' is not declared (did you mean BUILDPLATFORM?)",
		},
		{
			name: "warns for unknown platform arg with underscore suggestion",
			content: `ARG MY_ARCH=amd64
FROM --platform=linux/${MYARCH} busybox
`,
			wantCount:   1,
			wantMessage: "FROM argument 'MYARCH' is not declared (did you mean MY_ARCH?)",
		},
		{
			name: "warns for unknown base name arg",
			content: `ARG tag=latest
FROM busybox:${tag}${version} AS b
`,
			wantCount:   1,
			wantMessage: "FROM argument 'version' is not declared",
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
