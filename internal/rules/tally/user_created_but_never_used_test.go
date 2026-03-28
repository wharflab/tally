package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestUserCreatedButNeverUsedMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewUserCreatedButNeverUsedRule().Metadata())
}

func TestUserCreatedButNeverUsedCheck(t *testing.T) {
	t.Parallel()

	rule := NewUserCreatedButNeverUsedRule()

	testutil.RunRuleTests(t, rule, userCreatedButNeverUsedCases())
}

func TestUserCreatedButNeverUsedCheckWithFixes(t *testing.T) {
	t.Parallel()

	rule := NewUserCreatedButNeverUsedRule()

	tests := []struct {
		name           string
		content        string
		wantHasFix     bool
		wantFixContain string
	}{
		{
			name: "fix inserts USER before CMD",
			content: `FROM ubuntu:22.04
RUN useradd -r appuser
CMD ["app"]
`,
			wantHasFix:     true,
			wantFixContain: "USER appuser",
		},
		{
			name: "fix inserts USER before ENTRYPOINT",
			content: `FROM ubuntu:22.04
RUN useradd -r appuser
ENTRYPOINT ["app"]
`,
			wantHasFix:     true,
			wantFixContain: "USER appuser",
		},
		{
			name: "fix uses FixUnsafe safety",
			content: `FROM ubuntu:22.04
RUN useradd -r appuser
CMD ["app"]
`,
			wantHasFix:     true,
			wantFixContain: "USER appuser",
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
				var sb strings.Builder
				for _, edit := range v.SuggestedFix.Edits {
					sb.WriteString(edit.NewText)
				}
				if !strings.Contains(sb.String(), tt.wantFixContain) {
					t.Errorf("fix text %q does not contain %q", sb.String(), tt.wantFixContain)
				}
			}
		})
	}
}

func userCreatedButNeverUsedCases() []testutil.RuleTestCase {
	return []testutil.RuleTestCase{
		// === Violations ===
		{
			Name: "useradd with no USER instruction",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{`user "appuser" is created but the final stage never switches to it`},
		},
		{
			Name: "useradd with explicit USER root",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
USER root
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"USER is root"},
		},
		{
			Name: "adduser (Alpine) with no USER",
			Content: `FROM alpine:3.18
RUN adduser -S -D appuser
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{`user "appuser" is created`},
		},
		{
			Name: "useradd with USER 0 (numeric root)",
			Content: `FROM ubuntu:22.04
RUN useradd appuser
USER 0
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"USER is 0"},
		},
		{
			Name: "useradd with USER root:root",
			Content: `FROM ubuntu:22.04
RUN useradd appuser
USER root:root
CMD ["app"]
`,
			WantViolations: 1,
		},
		{
			Name: "useradd in chained command",
			Content: `FROM ubuntu:22.04
RUN apt-get update && useradd -r -u 1000 -g users appuser && echo done
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{`user "appuser" is created`},
		},
		{
			Name: "useradd with UID flags",
			Content: `FROM ubuntu:22.04
RUN useradd -r -u 1000 -g mygroup -s /sbin/nologin appuser
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{`"appuser"`},
		},

		// === No violations ===
		{
			Name: "useradd with correct USER switch",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
USER appuser
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "useradd with numeric non-root USER",
			Content: `FROM ubuntu:22.04
RUN useradd -r -u 1000 appuser
USER 1000
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "gosu in ENTRYPOINT suppresses",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
ENTRYPOINT ["gosu", "appuser", "docker-entrypoint.sh"]
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "su-exec in CMD suppresses (no ENTRYPOINT)",
			Content: `FROM alpine:3.18
RUN adduser -S appuser
CMD ["su-exec", "appuser", "app"]
`,
			WantViolations: 0,
		},
		{
			Name: "setpriv in ENTRYPOINT suppresses",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
ENTRYPOINT ["setpriv", "--reuid=appuser", "--regid=appuser", "--", "app"]
CMD ["--flag"]
`,
			WantViolations: 0,
		},
		{
			Name: "no user creation at all",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "known non-root base (distroless nonroot)",
			Content: `FROM gcr.io/distroless/static:nonroot
COPY app /app
CMD ["/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "known non-root base (chainguard)",
			Content: `FROM cgr.dev/chainguard/static:latest
COPY app /app
CMD ["/app"]
`,
			WantViolations: 0,
		},

		// === Cross-stage inheritance ===
		{
			Name: "FROM parent with useradd but no USER → violation",
			Content: `FROM ubuntu:22.04 AS base
RUN useradd -r appuser

FROM base
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{`user "appuser" is created`},
		},
		{
			Name: "FROM parent with useradd + USER appuser → no violation",
			Content: `FROM ubuntu:22.04 AS base
RUN useradd -r appuser
USER appuser

FROM base
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "COPY --from builder with useradd → no violation (no passwd inheritance)",
			Content: `FROM ubuntu:22.04 AS builder
RUN useradd -r builduser

FROM ubuntu:22.04
COPY --from=builder /app /app
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "multi-stage chain: useradd in grandparent, no USER anywhere",
			Content: `FROM ubuntu:22.04 AS base
RUN useradd -r appuser

FROM base AS middle
RUN echo something

FROM middle
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{`"appuser"`},
		},

		// === Ownership suppression ===
		{
			Name: "useradd + COPY --chown=same-user → no violation",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
COPY --chown=appuser:appuser app /app
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "useradd + ADD --chown=same-user → no violation",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
ADD --chown=appuser:appuser archive.tar.gz /app
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "useradd + RUN chown same-user → no violation",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
RUN chown -R appuser:appuser /app
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "useradd + COPY --chown=different-user → violation",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
COPY --chown=nobody:nogroup app /app
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{`"appuser"`},
		},
		{
			Name: "two users created, only one referenced → violation for unreferenced",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser && useradd -r dbuser
COPY --chown=appuser:appuser app /app
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{`"dbuser"`},
		},
		{
			Name: "useradd + chown to root → still a violation",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
RUN chown -R root:root /app
CMD ["app"]
`,
			WantViolations: 1,
		},

		// === Observable script cases ===
		{
			Name: "observable script useradd with RUN in stage does not panic",
			Content: `FROM ubuntu:22.04
COPY <<EOF /setup.sh
#!/bin/sh
useradd -r appuser
EOF
RUN /setup.sh
CMD ["app"]
`,
			WantViolations: 1,
		},
		{
			Name: "observable entrypoint script with chown suppresses",
			Content: `FROM ubuntu:22.04
RUN useradd -r appuser
COPY <<EOF /entrypoint.sh
#!/bin/sh
chown -R appuser:appuser /app
exec "$@"
EOF
CMD ["app"]
`,
			WantViolations: 0,
		},

		// === Message quality ===
		{
			Name: "implicit root message",
			Content: `FROM ubuntu:22.04
RUN useradd appuser
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"no USER instruction (defaults to root)"},
		},
		{
			Name: "explicit root message",
			Content: `FROM ubuntu:22.04
RUN useradd appuser
USER root
CMD ["app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"USER is root"},
		},
	}
}
