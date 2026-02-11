package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestPreferAddUnpackRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferAddUnpackRule().Metadata())
}

func TestPreferAddUnpackRule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		config     any
		wantCount  int
	}{
		// Pipe patterns
		{
			name: "catch: curl pipe to tar",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/
`,
			wantCount: 1,
		},
		{
			name: "catch: wget pipe to tar",
			dockerfile: `FROM ubuntu:22.04
RUN wget -qO- https://example.com/app.tar.gz | tar -xz -C /opt/
`,
			wantCount: 1,
		},
		{
			name: "catch: curl with long extract flag",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.gz | tar --extract -z -C /opt/
`,
			wantCount: 1,
		},
		// Download-then-extract patterns
		{
			name: "catch: curl download then tar extract",
			dockerfile: `FROM ubuntu:22.04
RUN curl -o /tmp/app.tar.gz https://example.com/app.tar.gz && tar -xf /tmp/app.tar.gz -C /opt/
`,
			wantCount: 1,
		},
		{
			name: "catch: wget download then tar extract",
			dockerfile: `FROM ubuntu:22.04
RUN wget -O /tmp/app.tar.gz https://example.com/app.tar.gz && tar -xf /tmp/app.tar.gz
`,
			wantCount: 1,
		},
		// Various archive extensions
		{
			name: "catch: .tar.bz2 URL",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.bz2 | tar -xj -C /opt/
`,
			wantCount: 1,
		},
		{
			name: "catch: .tar.xz URL",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.xz | tar -xJ -C /opt/
`,
			wantCount: 1,
		},
		{
			name: "catch: .tgz URL",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tgz | tar -xz -C /opt/
`,
			wantCount: 1,
		},
		{
			name: "ignore: .gz URL with gunzip (not tar-based)",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/data.gz -o /tmp/data.gz && gunzip /tmp/data.gz
`,
			wantCount: 0,
		},
		// URL with query string
		{
			name: "catch: URL with query string",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL "https://example.com/app.tar.gz?token=abc" | tar -xz -C /opt/
`,
			wantCount: 1,
		},
		// Non-matching patterns
		{
			name: "ignore: curl without extraction",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/script.sh | bash
`,
			wantCount: 0,
		},
		{
			name: "ignore: tar without download",
			dockerfile: `FROM ubuntu:22.04
RUN tar -xf /tmp/app.tar.gz -C /opt/
`,
			wantCount: 0,
		},
		{
			name: "ignore: curl non-archive URL",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/setup.sh -o /tmp/setup.sh && chmod +x /tmp/setup.sh
`,
			wantCount: 0,
		},
		{
			name: "ignore: wget non-archive URL with tar on different file",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://example.com/config.json -O /tmp/config.json && tar -xf /local/app.tar
`,
			wantCount: 0,
		},
		{
			name: "ignore: tar create (no extract)",
			dockerfile: `FROM ubuntu:22.04
RUN curl -o /tmp/app.tar.gz https://example.com/app.tar.gz && tar -cf /tmp/backup.tar /data
`,
			wantCount: 0,
		},
		// Config: disabled
		{
			name: "config: disabled skips detection",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/
`,
			config:    PreferAddUnpackConfig{Enabled: new(false)},
			wantCount: 0,
		},
		// Config: explicitly enabled
		{
			name: "config: explicitly enabled",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/
`,
			config:    PreferAddUnpackConfig{Enabled: new(true)},
			wantCount: 1,
		},
		// Multi-stage
		{
			name: "catch: in multi-stage build",
			dockerfile: `FROM ubuntu:22.04 AS builder
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/

FROM alpine:3.18
COPY --from=builder /opt/app /opt/app
`,
			wantCount: 1,
		},
		// Complex real-world patterns
		{
			name: "catch: real-world Go install pattern",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | tar -xz -C /usr/local
`,
			wantCount: 1,
		},
		{
			name: "catch: real-world Node.js install",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz && \
    tar -xJf node-v20.11.0-linux-x64.tar.xz -C /usr/local --strip-components=1
`,
			wantCount: 1,
		},
		{
			name: "catch: ftp URL",
			dockerfile: `FROM ubuntu:22.04
RUN curl ftp://mirror.example.com/data.tar.gz -o /tmp/data.tar.gz && tar -xf /tmp/data.tar.gz
`,
			wantCount: 1,
		},
		// URL without archive extension, but output filename has one
		{
			name: "catch: curl -o archive name, URL has no extension",
			dockerfile: `FROM ubuntu:22.04
RUN curl https://foo.com/latest -o foo.tar && tar -xf foo.tar
`,
			wantCount: 1,
		},
		{
			name: "catch: wget -O archive name, URL has no extension",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://foo.com/latest -O /tmp/app.tar.gz && tar -xf /tmp/app.tar.gz -C /opt
`,
			wantCount: 1,
		},
		{
			name: "ignore: curl -o non-archive name, URL has no extension",
			dockerfile: `FROM ubuntu:22.04
RUN curl https://foo.com/latest -o setup.sh && chmod +x setup.sh
`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var input rules.LintInput
			if tt.config != nil {
				input = testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.dockerfile, tt.config)
			} else {
				input = testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			}

			r := NewPreferAddUnpackRule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s (line %d): %s", v.RuleCode, v.Location.Start.Line, v.Message)
				}
			}

			if tt.wantCount > 0 && len(violations) > 0 {
				wantCode := rules.TallyRulePrefix + "prefer-add-unpack"
				if violations[0].RuleCode != wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, wantCode)
				}
			}
		})
	}
}

func TestPreferAddUnpackRule_Fix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantFix    bool   // whether a SuggestedFix should be attached
		wantURL    string // expected URL in fix (empty if wantFix==false)
		wantDest   string // expected dest in fix
	}{
		{
			name: "simple pipe: curl | tar -C /usr/local",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | tar -xz -C /usr/local
`,
			wantFix:  true,
			wantURL:  "https://go.dev/dl/go1.22.0.linux-amd64.tar.gz",
			wantDest: "/usr/local",
		},
		{
			name: "simple download-then-extract: curl -o && tar -xf -C /opt/",
			dockerfile: `FROM ubuntu:22.04
RUN curl -o /tmp/app.tar.gz https://example.com/app.tar.gz && tar -xf /tmp/app.tar.gz -C /opt/
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tar.gz",
			wantDest: "/opt/",
		},
		{
			name: "no fix: --strip-components cannot be replicated",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz && \
    tar -xJf node-v20.11.0-linux-x64.tar.xz -C /usr/local --strip-components=1
`,
			wantFix: false, // --strip-components changes semantics
		},
		{
			name: "complex chain: extra commands beyond download+extract",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt/ && chmod +x /opt/app/bin/start
`,
			wantFix: false, // chmod is not a download/extract command
		},
		{
			name: "dest from --directory=",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.gz | tar --extract --directory=/srv
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tar.gz",
			wantDest: "/srv",
		},
		{
			name: "dest from --directory <dir>",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.gz | tar -x --directory /var/lib
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tar.gz",
			wantDest: "/var/lib",
		},
		{
			name: "default dest when no -C",
			dockerfile: `FROM ubuntu:22.04
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tar.gz",
			wantDest: "/",
		},
		{
			name: "wget pipe",
			dockerfile: `FROM ubuntu:22.04
RUN wget -qO- https://example.com/app.tgz | tar -xz -C /opt
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tgz",
			wantDest: "/opt",
		},
		{
			name: "wget download-then-extract",
			dockerfile: `FROM ubuntu:22.04
RUN wget -O /tmp/app.tar.gz https://example.com/app.tar.gz && tar -xf /tmp/app.tar.gz -C /opt
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tar.gz",
			wantDest: "/opt",
		},
		{
			name: "WORKDIR used as default dest when no -C",
			dockerfile: `FROM ubuntu:22.04
WORKDIR /app
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tar.gz",
			wantDest: "/app",
		},
		{
			name: "explicit -C overrides WORKDIR",
			dockerfile: `FROM ubuntu:22.04
WORKDIR /app
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /usr/local
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tar.gz",
			wantDest: "/usr/local",
		},
		{
			name: "relative WORKDIR resolved against previous",
			dockerfile: `FROM ubuntu:22.04
WORKDIR /opt
WORKDIR myapp
RUN curl -fsSL https://example.com/app.tar.gz | tar -xz
`,
			wantFix:  true,
			wantURL:  "https://example.com/app.tar.gz",
			wantDest: "/opt/myapp",
		},
		{
			name: "curl -o archive name, URL has no extension",
			dockerfile: `FROM ubuntu:22.04
RUN curl https://foo.com/latest -o foo.tar && tar -xf foo.tar
`,
			wantFix:  true,
			wantURL:  "https://foo.com/latest",
			wantDest: "/",
		},
		{
			name: "wget -O archive name, URL has no extension",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://foo.com/latest -O /tmp/app.tar.gz && tar -xf /tmp/app.tar.gz -C /opt
`,
			wantFix:  true,
			wantURL:  "https://foo.com/latest",
			wantDest: "/opt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewPreferAddUnpackRule()
			violations := r.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			if tt.wantFix {
				if v.SuggestedFix == nil {
					t.Fatal("expected SuggestedFix, got nil")
				}
				if v.SuggestedFix.Safety != rules.FixSuggestion {
					t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
				}
				if v.SuggestedFix.Priority != 95 {
					t.Errorf("Priority = %d, want 95", v.SuggestedFix.Priority)
				}
				if len(v.SuggestedFix.Edits) != 1 {
					t.Fatalf("Edits count = %d, want 1", len(v.SuggestedFix.Edits))
				}
				wantNewText := "ADD --unpack " + tt.wantURL + " " + tt.wantDest
				if v.SuggestedFix.Edits[0].NewText != wantNewText {
					t.Errorf("NewText = %q, want %q", v.SuggestedFix.Edits[0].NewText, wantNewText)
				}
			} else if v.SuggestedFix != nil {
				t.Errorf("expected no SuggestedFix, got %+v", v.SuggestedFix)
			}
		})
	}
}
