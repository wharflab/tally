package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDL3045Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3045Rule().Metadata())
}

func TestDL3045Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// --- Cases from Hadolint spec: ruleCatchesNot (expect 0 violations) ---
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set",
			dockerfile: "FROM scratch\nCOPY bla.sh /usr/local/bin/blubb.sh\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set with quotes",
			dockerfile: "FROM scratch\nCOPY bla.sh \"/usr/local/bin/blubb.sh\"\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set - windows",
			dockerfile: "FROM scratch\nCOPY bla.sh c:\\system32\\blubb.sh\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set - windows with quotes",
			dockerfile: "FROM scratch\nCOPY bla.sh \"c:\\system32\\blubb.sh\"\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set - windows with alternative paths",
			dockerfile: "FROM scratch\nCOPY bla.sh c:/system32/blubb.sh\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with relative destination and WORKDIR set",
			dockerfile: "FROM scratch\nWORKDIR /usr\nCOPY bla.sh blubb.sh\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with destination being env variable braces",
			dockerfile: "FROM scratch\nCOPY src.sh ${SRC_BASE_ENV}\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with destination being env variable dollar",
			dockerfile: "FROM scratch\nCOPY src.sh $SRC_BASE_ENV\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with destination being env variable braces in quotes",
			dockerfile: "FROM scratch\nCOPY src.sh \"${SRC_BASE_ENV}\"\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with destination being env variable dollar in quotes",
			dockerfile: "FROM scratch\nCOPY src.sh \"$SRC_BASE_ENV\"\n",
			wantCount:  0,
		},

		// --- Cases from Hadolint spec: ruleCatches (expect violations) ---
		{
			name:       "not ok: COPY with relative destination and no WORKDIR set",
			dockerfile: "FROM scratch\nCOPY bla.sh blubb.sh\n",
			wantCount:  1,
		},
		{
			name:       "not ok: COPY with relative destination and no WORKDIR set with quotes",
			dockerfile: "FROM scratch\nCOPY bla.sh \"blubb.sh\"\n",
			wantCount:  1,
		},
		{
			name: "not ok: COPY to relative destination if WORKDIR is set in previous stage but not inherited",
			dockerfile: `FROM debian:buster as stage1
WORKDIR /usr
FROM debian:buster
COPY foo bar
`,
			wantCount: 1,
		},
		{
			name: "not ok: COPY to relative destination if WORKDIR is set in previous stage but not inherited - windows",
			dockerfile: `FROM microsoft/windowsservercore as stage1
WORKDIR c:\system32
FROM microsoft/windowsservercore
COPY foo bar
`,
			wantCount: 1,
		},

		// --- Inheritance cases from Hadolint spec ---
		{
			name: "ok: COPY to relative destination if WORKDIR has been set in base image",
			dockerfile: `FROM debian:buster as base
WORKDIR /usr
FROM debian:buster as stage-in-between
RUN foo
FROM base
COPY foo bar
`,
			wantCount: 0,
		},
		{
			name: "ok: COPY to relative destination if WORKDIR has been set in previous stage deep case",
			dockerfile: `FROM debian:buster as base1
WORKDIR /usr
FROM base1 as base2
RUN foo
FROM base2
COPY foo bar
`,
			wantCount: 0,
		},

		// --- ONBUILD cases from Hadolint spec ---
		{
			name: "ok: COPY to relative destination if WORKDIR has been set both within ONBUILD context",
			dockerfile: `FROM debian:buster
ONBUILD WORKDIR /usr/local/lib
ONBUILD COPY foo bar
`,
			wantCount: 0,
		},
		{
			name:       "not ok: ONBUILD COPY with relative destination and no WORKDIR",
			dockerfile: "FROM debian:buster\nONBUILD COPY foo bar\n",
			wantCount:  1,
		},

		// --- Regression from Hadolint spec ---
		{
			name:       "regression: don't crash with single character paths",
			dockerfile: "FROM scratch\nCOPY a b\n",
			wantCount:  1,
		},

		// --- Additional edge cases ---
		{
			name: "multiple COPY instructions some ok some not",
			dockerfile: `FROM scratch
COPY a /absolute/path
COPY b relative-path
`,
			wantCount: 1,
		},
		{
			name: "WORKDIR set then COPY is ok",
			dockerfile: `FROM ubuntu:22.04
WORKDIR /app
COPY . .
`,
			wantCount: 0,
		},
		{
			name: "COPY before WORKDIR triggers violation",
			dockerfile: `FROM ubuntu:22.04
COPY . .
WORKDIR /app
`,
			wantCount: 1,
		},
		{
			name: "multi-stage with mixed WORKDIR status",
			dockerfile: `FROM ubuntu:22.04 AS builder
WORKDIR /build
COPY . .

FROM alpine:3.18
COPY --from=builder /build/app .
`,
			// Second stage has no WORKDIR set but COPY destination "." is relative
			wantCount: 1,
		},
		{
			name: "COPY to dot in stage with WORKDIR is ok",
			dockerfile: `FROM alpine:3.18
WORKDIR /app
COPY . .
`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)

			r := NewDL3045Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s (line %d)", v.RuleCode, v.Message, v.Location.Start.Line)
				}
			}

			for i, v := range violations {
				if v.RuleCode != rules.HadolintRulePrefix+"DL3045" {
					t.Errorf("violations[%d].RuleCode = %q, want %q", i, v.RuleCode, rules.HadolintRulePrefix+"DL3045")
				}
			}
		})
	}
}

func TestIsAbsoluteOrVariableDest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dest string
		want bool
	}{
		// Absolute Unix paths
		{"/usr/local/bin/foo", true},
		{"/foo", true},

		// Quoted absolute paths
		{"\"/usr/local/bin/foo\"", true},

		// Windows absolute paths
		{"c:\\system32\\foo", true},
		{"D:\\mypath\\foo", true},
		{"c:/system32/foo", true},

		// Quoted Windows paths
		{"\"c:\\system32\\foo\"", true},

		// Environment variables
		{"$SRC_BASE_ENV", true},
		{"${SRC_BASE_ENV}", true},
		{"\"$SRC_BASE_ENV\"", true},
		{"\"${SRC_BASE_ENV}\"", true},

		// Relative paths (should return false)
		{"foo", false},
		{".", false},
		{"./foo", false},
		{"\"foo\"", false},
		{"bar/baz", false},

		// Edge cases
		{"b", false},
		{"a", false},
	}

	for _, tt := range tests {
		t.Run(tt.dest, func(t *testing.T) {
			t.Parallel()
			got := isAbsoluteOrVariableDest(tt.dest)
			if got != tt.want {
				t.Errorf("isAbsoluteOrVariableDest(%q) = %v, want %v", tt.dest, got, tt.want)
			}
		})
	}
}
