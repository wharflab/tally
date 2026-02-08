package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3010Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3010Rule().Metadata())
}

func TestDL3010Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Hadolint original test cases (from DL3010Spec.hs)
		{
			name: "catch: copy archive then extract 1",
			dockerfile: `FROM ubuntu:22.04
COPY packaged-app.tar /usr/src/app
RUN tar -xf /usr/src/app/packaged-app.tar
`,
			wantCount: 1,
		},
		{
			name: "catch: copy archive then extract 2",
			dockerfile: `FROM ubuntu:22.04
COPY packaged-app.tar /usr/src/app
WORKDIR /usr/src/app
RUN foo bar && echo something && tar -xf packaged-app.tar
`,
			wantCount: 1,
		},
		{
			name: "catch: copy archive then extract 3",
			dockerfile: `FROM ubuntu:22.04
COPY foo/bar/packaged-app.tar /foo.tar
RUN tar -xf /foo.tar
`,
			wantCount: 1,
		},
		{
			name: "catch: copy archive then extract windows paths 1",
			// Note: BuildKit parser requires JSON form for paths with spaces.
			// Using simple backslash paths without spaces to test basename extraction.
			dockerfile: `FROM ubuntu:22.04
COPY build\foo\bar.tar.gz /tmp/
RUN tar -xf /tmp/bar.tar.gz
`,
			wantCount: 1,
		},
		{
			name: "catch: copy archive then extract windows paths 2",
			dockerfile: `FROM ubuntu:22.04
COPY build\foo\bar.tar.gz /tmp/foo.tar.gz
RUN tar -xf foo.tar.gz
`,
			wantCount: 1,
		},
		{
			name: "ignore: copy archive without extract",
			dockerfile: `FROM ubuntu:22.04
COPY packaged-app.tar /usr/src/app
FROM debian:11 as newstage
`,
			wantCount: 0,
		},
		{
			name: "ignore: non archive",
			dockerfile: `FROM ubuntu:22.04
COPY package.json /usr/src/app
`,
			wantCount: 0,
		},
		{
			name: "ignore: copy from previous stage",
			dockerfile: `FROM ubuntu:22.04 AS builder
RUN echo hello
FROM debian:11
COPY --from=builder /usr/local/share/some.tar /opt/some.tar
`,
			wantCount: 0,
		},
		// Additional test cases
		{
			name: "catch: zcat extraction",
			dockerfile: `FROM ubuntu:22.04
COPY data.gz /tmp/
RUN zcat /tmp/data.gz > /tmp/data
`,
			wantCount: 1,
		},
		{
			name: "catch: gunzip extraction",
			dockerfile: `FROM ubuntu:22.04
COPY data.gz /tmp/
RUN gunzip /tmp/data.gz
`,
			wantCount: 1,
		},
		{
			name: "catch: tar with long extract flag",
			dockerfile: `FROM ubuntu:22.04
COPY app.tar /tmp/
RUN tar --extract -f /tmp/app.tar
`,
			wantCount: 1,
		},
		{
			name: "catch: tar with --get flag",
			dockerfile: `FROM ubuntu:22.04
COPY app.tar /tmp/
RUN tar --get -f /tmp/app.tar
`,
			wantCount: 1,
		},
		{
			name: "catch: tar with combined flags -xzf",
			dockerfile: `FROM ubuntu:22.04
COPY app.tgz /tmp/
RUN tar -xzf /tmp/app.tgz
`,
			wantCount: 1,
		},
		{
			name: "ignore: tar without extract flag (create)",
			dockerfile: `FROM ubuntu:22.04
COPY app.tar /tmp/
RUN tar -cf /tmp/output.tar /tmp/app.tar
`,
			wantCount: 0,
		},
		{
			name: "ignore: tar extracts different archive",
			dockerfile: `FROM ubuntu:22.04
COPY app.tar /tmp/
RUN tar -xf /tmp/other.tar
`,
			wantCount: 0,
		},
		{
			name: "catch: multiple archive extensions .tar.gz",
			dockerfile: `FROM ubuntu:22.04
COPY app.tar.gz /tmp/
RUN tar -xf /tmp/app.tar.gz
`,
			wantCount: 1,
		},
		{
			name: "catch: .xz extension",
			dockerfile: `FROM ubuntu:22.04
COPY data.xz /tmp/
RUN unxz /tmp/data.xz
`,
			wantCount: 1,
		},
		{
			name: "catch: .bz2 extension",
			dockerfile: `FROM ubuntu:22.04
COPY data.bz2 /tmp/
RUN bunzip2 /tmp/data.bz2
`,
			wantCount: 1,
		},
		{
			name: "ignore: different stage resets tracking",
			dockerfile: `FROM ubuntu:22.04 AS build
COPY app.tar /tmp/
FROM debian:11
RUN tar -xf /tmp/app.tar
`,
			wantCount: 0,
		},
		{
			name: "catch: multiple archives one extracted",
			dockerfile: `FROM ubuntu:22.04
COPY app.tar /tmp/
COPY data.json /tmp/
RUN tar -xf /tmp/app.tar
`,
			wantCount: 1,
		},
		{
			name: "catch: target is archive name",
			dockerfile: `FROM ubuntu:22.04
COPY src/app.tar.gz /app.tar.gz
RUN tar -xf /app.tar.gz
`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3010Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s (line %d): %s", v.RuleCode, v.Location.Start.Line, v.Message)
				}
			}

			if tt.wantCount > 0 && len(violations) > 0 {
				if violations[0].RuleCode != rules.HadolintRulePrefix+"DL3010" {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, rules.HadolintRulePrefix+"DL3010")
				}
			}
		})
	}
}
