package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNamedIdentityInPasswdlessStageMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNamedIdentityInPasswdlessStageRule().Metadata())
}

func TestNamedIdentityInPasswdlessStageCheck(t *testing.T) {
	t.Parallel()

	rule := NewNamedIdentityInPasswdlessStageRule()

	testutil.RunRuleTests(t, rule, []testutil.RuleTestCase{
		// --- Violations expected ---
		{
			Name: "scratch with named USER",
			Content: `FROM scratch
USER appuser
`,
			WantViolations: 1,
			WantMessages:   []string{`named user "appuser"`},
		},
		{
			Name: "scratch with named USER and group",
			Content: `FROM scratch
USER appuser:appgroup
`,
			WantViolations: 1,
			WantMessages:   []string{`named user "appuser" and group "appgroup"`},
		},
		{
			Name: "scratch with named group only",
			Content: `FROM scratch
USER 1000:appgroup
`,
			WantViolations: 1,
			WantMessages:   []string{`named group "appgroup"`},
		},
		{
			Name: "scratch with COPY --chown named user",
			Content: `FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY --chown=appuser:appgroup --from=builder /app /app
`,
			WantViolations: 1,
			WantMessages:   []string{`COPY --chown uses named user "appuser"`},
		},
		{
			Name: "scratch with ADD --chown named user",
			Content: `FROM scratch
ADD --chown=myuser https://example.com/app /app
`,
			WantViolations: 1,
			WantMessages:   []string{`ADD --chown uses named user "myuser"`},
		},
		{
			Name: "multi-stage scratch chain without passwd",
			Content: `FROM scratch AS base
COPY --from=builder /myapp /myapp

FROM base
USER appuser
`,
			WantViolations: 1,
			WantMessages:   []string{`named user "appuser"`},
		},
		{
			Name: "multiple violations in same scratch stage",
			Content: `FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
USER appuser
COPY --chown=myuser --from=builder /app /app
`,
			WantViolations: 2,
		},

		// --- Directory dest false positive regression ---
		{
			Name: "COPY to /etc/ with unrelated source does not suppress",
			Content: `FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/
USER appuser
`,
			WantViolations: 1,
			WantMessages:   []string{`named user "appuser"`},
		},

		// --- Incremental state tracking ---
		{
			Name: "named chown before passwd copied - violation then suppressed",
			Content: `FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY --chown=appuser --from=builder /app /app
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
USER appuser
`,
			WantViolations: 1,
			WantMessages:   []string{`COPY --chown uses named user "appuser"`},
		},
		{
			Name: "named USER after passwd copied - no violation",
			Content: `FROM golang:1.22 AS builder
RUN useradd -r appuser

FROM scratch
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
USER appuser
`,
			WantViolations: 0,
		},

		// --- No violations expected ---
		{
			Name: "scratch with numeric USER",
			Content: `FROM scratch
USER 65532
`,
			WantViolations: 0,
		},
		{
			Name: "scratch with numeric USER and group",
			Content: `FROM scratch
USER 1000:1000
`,
			WantViolations: 0,
		},
		{
			Name: "scratch with numeric COPY --chown",
			Content: `FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY --chown=1000:1000 --from=builder /app /app
`,
			WantViolations: 0,
		},
		{
			Name: "non-scratch stage with named USER",
			Content: `FROM alpine:3.19
USER appuser
`,
			WantViolations: 0,
		},
		{
			Name: "debian with named USER",
			Content: `FROM debian:12
RUN useradd -r appuser
USER appuser
`,
			WantViolations: 0,
		},
		{
			Name: "distroless with named USER",
			Content: `FROM gcr.io/distroless/static:nonroot
USER nonroot
`,
			WantViolations: 0,
		},
		{
			Name: "scratch with passwd copied from builder via COPY dest",
			Content: `FROM golang:1.22 AS builder
RUN useradd -r appuser

FROM scratch
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
USER appuser
`,
			WantViolations: 0,
		},
		{
			Name: "scratch with SHELL instruction suppresses",
			Content: `FROM scratch
COPY --from=builder /bin/sh /bin/sh
SHELL ["/bin/sh", "-c"]
USER appuser
`,
			WantViolations: 0,
		},
		{
			Name: "scratch with named USER before SHELL still fires",
			Content: `FROM scratch
USER appuser
COPY --from=builder /bin/sh /bin/sh
SHELL ["/bin/sh", "-c"]
`,
			WantViolations: 1,
		},
		{
			Name: "multi-stage with passwd in parent",
			Content: `FROM golang:1.22 AS builder
RUN useradd -r appuser

FROM scratch AS base
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

FROM base
USER appuser
`,
			WantViolations: 0,
		},
		{
			Name: "split inheritance: passwd in base, group in mid",
			Content: `FROM golang:1.22 AS builder
RUN useradd -r appuser

FROM scratch AS base
COPY --from=builder /etc/passwd /etc/passwd

FROM base AS mid
COPY --from=builder /etc/group /etc/group

FROM mid
USER appuser:appgroup
`,
			WantViolations: 0,
		},
		{
			Name: "scratch with no USER or chown - no violation",
			Content: `FROM scratch
COPY --from=builder /myapp /myapp
ENTRYPOINT ["/myapp"]
`,
			WantViolations: 0,
		},
		{
			Name: "scratch with root USER - no violation",
			Content: `FROM scratch
USER root
`,
			WantViolations: 0,
		},
		{
			Name: "scratch with USER 0 - no violation",
			Content: `FROM scratch
USER 0
`,
			WantViolations: 0,
		},
	})
}

func TestNamedIdentityInPasswdlessStageCheckWithFixes(t *testing.T) {
	t.Parallel()

	rule := NewNamedIdentityInPasswdlessStageRule()

	tests := []struct {
		name           string
		content        string
		wantHasFix     bool
		wantFixContain string
		wantSafety     rules.FixSafety
	}{
		{
			name: "USER fix replaces named with numeric",
			content: `FROM scratch
USER appuser
`,
			wantHasFix:     true,
			wantFixContain: "65532",
			wantSafety:     rules.FixSuggestion,
		},
		{
			name: "USER fix preserves numeric group",
			content: `FROM scratch
USER appuser:1000
`,
			wantHasFix:     true,
			wantFixContain: "65532:1000",
			wantSafety:     rules.FixSuggestion,
		},
		{
			name: "USER fix replaces both named user and group",
			content: `FROM scratch
USER appuser:appgroup
`,
			wantHasFix:     true,
			wantFixContain: "65532:65532",
			wantSafety:     rules.FixSuggestion,
		},
		{
			name: "COPY --chown fix replaces named with numeric",
			content: `FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY --chown=appuser --from=builder /app /app
`,
			wantHasFix:     true,
			wantFixContain: "--chown=65532",
			wantSafety:     rules.FixSuggestion,
		},
		{
			name: "COPY --chown fix replaces user:group",
			content: `FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY --chown=appuser:appgroup --from=builder /app /app
`,
			wantHasFix:     true,
			wantFixContain: "--chown=65532:65532",
			wantSafety:     rules.FixSuggestion,
		},
		{
			name: "COPY --chown fix works on continuation line",
			content: `FROM golang:1.22 AS builder
RUN echo hello > /app

FROM scratch
COPY \
  --chown=appuser \
  --from=builder /app /app
`,
			wantHasFix:     true,
			wantFixContain: "--chown=65532",
			wantSafety:     rules.FixSuggestion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			violations := rule.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			hasFix := v.SuggestedFix != nil
			if hasFix != tt.wantHasFix {
				t.Errorf("hasFix = %v, want %v", hasFix, tt.wantHasFix)
			}

			if hasFix && tt.wantFixContain != "" {
				found := false
				for _, edit := range v.SuggestedFix.Edits {
					if strings.Contains(edit.NewText, tt.wantFixContain) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("fix edits do not contain %q", tt.wantFixContain)
					for _, edit := range v.SuggestedFix.Edits {
						t.Logf("  edit: %q", edit.NewText)
					}
				}
			}

			if hasFix && v.SuggestedFix.Safety != tt.wantSafety {
				t.Errorf("fix safety = %v, want %v", v.SuggestedFix.Safety, tt.wantSafety)
			}
		})
	}
}

func TestSplitUserGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input     string
		wantUser  string
		wantGroup string
	}{
		{"appuser", "appuser", ""},
		{"appuser:appgroup", "appuser", "appgroup"},
		{"1000:1000", "1000", "1000"},
		{"1000", "1000", ""},
		{" appuser : appgroup ", "appuser", "appgroup"},
		{"", "", ""},
		{"user:", "user", ""},
		{":group", "", "group"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			user, group := splitUserGroup(tt.input)
			if user != tt.wantUser {
				t.Errorf("splitUserGroup(%q) user = %q, want %q", tt.input, user, tt.wantUser)
			}
			if group != tt.wantGroup {
				t.Errorf("splitUserGroup(%q) group = %q, want %q", tt.input, group, tt.wantGroup)
			}
		})
	}
}

func TestNumericReplacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		user, group        string
		namedUser, namedGr bool
		want               string
	}{
		{"appuser", "", true, false, "65532"},
		{"appuser", "appgroup", true, true, "65532:65532"},
		{"1000", "appgroup", false, true, "1000:65532"},
		{"appuser", "1000", true, false, "65532:1000"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := numericReplacement(tt.user, tt.group, tt.namedUser, tt.namedGr)
			if got != tt.want {
				t.Errorf("numericReplacement() = %q, want %q", got, tt.want)
			}
		})
	}
}
