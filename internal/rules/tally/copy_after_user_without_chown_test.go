package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestCopyAfterUserWithoutChownMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewCopyAfterUserWithoutChownRule().Metadata())
}

func TestCopyAfterUserWithoutChownCheck(t *testing.T) {
	t.Parallel()

	rule := NewCopyAfterUserWithoutChownRule()

	testutil.RunRuleTests(t, rule, []testutil.RuleTestCase{
		// --- Positive cases: should flag ---
		{
			Name: "COPY after USER nonroot without chown",
			Content: `FROM ubuntu:22.04
USER nonroot
COPY app /app
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY without --chown creates root-owned files despite USER nonroot"},
		},
		{
			Name: "ADD after USER nonroot without chown",
			Content: `FROM ubuntu:22.04
USER nonroot
ADD config.tar.gz /etc/app/
`,
			WantViolations: 1,
			WantMessages:   []string{"ADD without --chown"},
		},
		{
			Name: "multiple COPY after USER",
			Content: `FROM ubuntu:22.04
USER appuser
COPY a /a
COPY b /b
`,
			WantViolations: 2,
		},
		{
			Name: "USER with group",
			Content: `FROM ubuntu:22.04
USER app:appgroup
COPY file /app/file
`,
			WantViolations: 1,
			WantMessages:   []string{"USER app:appgroup"},
		},
		{
			Name: "USER with numeric ID",
			Content: `FROM ubuntu:22.04
USER 1000
COPY app /app
`,
			WantViolations: 1,
			WantMessages:   []string{"USER 1000"},
		},
		{
			Name: "USER with UID:GID",
			Content: `FROM ubuntu:22.04
USER 1000:1000
COPY app /app
`,
			WantViolations: 1,
			WantMessages:   []string{"USER 1000:1000"},
		},
		{
			Name: "USER with variable",
			Content: `FROM ubuntu:22.04
ARG APP_USER=nonroot
USER $APP_USER
COPY app /app
`,
			WantViolations: 1,
			WantMessages:   []string{"USER $APP_USER"},
		},
		{
			Name: "COPY --from still flagged",
			Content: `FROM ubuntu:22.04 AS builder
RUN echo build

FROM ubuntu:22.04
USER appuser
COPY --from=builder /build/app /app
`,
			WantViolations: 1,
		},
		{
			Name: "COPY heredoc without chown",
			Content: `FROM ubuntu:22.04
USER appuser
COPY <<EOF /app/config.json
{"key": "value"}
EOF
`,
			WantViolations: 1,
		},
		{
			Name: "RUN between USER and COPY still flags",
			Content: `FROM ubuntu:22.04
USER appuser
RUN mkdir -p /app
COPY app /app
`,
			WantViolations: 1,
		},
		{
			Name: "inherited USER from parent stage",
			Content: `FROM ubuntu:22.04 AS base
USER appuser

FROM base
COPY app /app
`,
			WantViolations: 1,
		},
		{
			Name: "chown on different path does not suppress",
			Content: `FROM ubuntu:22.04
USER appuser
COPY app /app
RUN chown -R appuser:appuser /opt
`,
			WantViolations: 1,
		},

		// --- Negative cases: should NOT flag ---
		{
			Name: "COPY with chown already set",
			Content: `FROM ubuntu:22.04
USER appuser
COPY --chown=appuser:appuser app /app
`,
			WantViolations: 0,
		},
		{
			Name: "COPY before any USER",
			Content: `FROM ubuntu:22.04
COPY app /app
`,
			WantViolations: 0,
		},
		{
			Name: "COPY after USER root",
			Content: `FROM ubuntu:22.04
USER root
COPY app /app
`,
			WantViolations: 0,
		},
		{
			Name: "COPY after USER 0",
			Content: `FROM ubuntu:22.04
USER 0
COPY app /app
`,
			WantViolations: 0,
		},
		{
			Name: "no USER instruction at all",
			Content: `FROM ubuntu:22.04
COPY app /app
RUN echo done
`,
			WantViolations: 0,
		},
		{
			Name: "USER nonroot then USER root then COPY",
			Content: `FROM ubuntu:22.04
USER nonroot
RUN echo something
USER root
COPY app /app
`,
			WantViolations: 0,
		},
		{
			Name: "ADD with chown already set",
			Content: `FROM ubuntu:22.04
USER appuser
ADD --chown=appuser config.tar.gz /etc/app/
`,
			WantViolations: 0,
		},
		{
			Name: "chown on same path suppresses",
			Content: `FROM ubuntu:22.04
USER appuser
COPY app /app
RUN chown -R appuser:appuser /app
`,
			WantViolations: 0,
		},
		{
			Name: "chown on parent path suppresses",
			Content: `FROM ubuntu:22.04
USER appuser
COPY config /etc/app/config
RUN chown -R appuser:appuser /etc
`,
			WantViolations: 0,
		},
		{
			Name: "chown not immediately next but still suppresses",
			Content: `FROM ubuntu:22.04
USER appuser
COPY app /app
EXPOSE 8080
RUN chown -R appuser:appuser /app
`,
			WantViolations: 0,
		},
		{
			Name: "external base image without USER",
			Content: `FROM gcr.io/distroless/static:nonroot
COPY app /app
`,
			WantViolations: 0,
		},
		{
			Name: "empty dockerfile",
			Content: `FROM scratch
`,
			WantViolations: 0,
		},
	})
}

func TestCopyAfterUserWithoutChownCheckWithFixes(t *testing.T) {
	t.Parallel()

	rule := NewCopyAfterUserWithoutChownRule()

	tests := []struct {
		name            string
		content         string
		wantFixCount    int
		wantChown       string
		wantHasMoveUser bool
		wantMoveContain string
	}{
		{
			name: "Alt1 adds chown flag",
			content: `FROM ubuntu:22.04
USER appuser
COPY app /app
RUN setup.sh
`,
			wantFixCount:    2,
			wantChown:       "--chown=appuser",
			wantHasMoveUser: true,
			wantMoveContain: "Move USER appuser",
		},
		{
			name: "Alt1 preserves user:group in chown",
			content: `FROM ubuntu:22.04
USER app:appgroup
COPY app /app
RUN setup.sh
`,
			wantFixCount:    2,
			wantChown:       "--chown=app:appgroup",
			wantHasMoveUser: true,
		},
		{
			name: "Alt1 uses numeric ID",
			content: `FROM ubuntu:22.04
USER 1000
ADD config /etc/app/
RUN setup.sh
`,
			wantFixCount:    2,
			wantChown:       "--chown=1000",
			wantHasMoveUser: true,
		},
		{
			name: "no Alt2 when RUN between USER and COPY",
			content: `FROM ubuntu:22.04
USER appuser
RUN mkdir -p /app
COPY app /app
`,
			wantFixCount:    1,
			wantChown:       "--chown=appuser",
			wantHasMoveUser: false,
		},
		{
			name: "no Alt2 when no RUN/WORKDIR after COPY",
			content: `FROM ubuntu:22.04
USER appuser
COPY app /app
CMD ["app"]
`,
			wantFixCount:    1,
			wantChown:       "--chown=appuser",
			wantHasMoveUser: false,
		},
		{
			name: "no Alt2 for inherited USER",
			content: `FROM ubuntu:22.04 AS base
USER appuser

FROM base
COPY app /app
RUN setup.sh
`,
			wantFixCount:    1,
			wantChown:       "--chown=appuser",
			wantHasMoveUser: false,
		},
		{
			name: "Alt2 targets WORKDIR",
			content: `FROM ubuntu:22.04
USER appuser
COPY app /app
WORKDIR /app
`,
			wantFixCount:    2,
			wantChown:       "--chown=appuser",
			wantHasMoveUser: true,
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
			allFixes := v.AllFixes()

			if len(allFixes) != tt.wantFixCount {
				t.Errorf("got %d fixes, want %d", len(allFixes), tt.wantFixCount)
				for i, f := range allFixes {
					t.Logf("  fix[%d]: %s", i, f.Description)
				}
			}

			// Check preferred fix (Alt 1: --chown).
			preferred := v.PreferredFix()
			if preferred == nil {
				t.Fatal("expected preferred fix")
			}
			if !preferred.IsPreferred {
				t.Error("preferred fix should have IsPreferred=true")
			}
			if !strings.Contains(preferred.Edits[0].NewText, tt.wantChown) {
				t.Errorf("preferred fix NewText = %q, want containing %q",
					preferred.Edits[0].NewText, tt.wantChown)
			}

			// Check Alt 2 (move USER) if expected.
			hasMoveUser := false
			for _, fix := range allFixes {
				if strings.Contains(fix.Description, "Move USER") {
					hasMoveUser = true
					if fix.IsPreferred {
						t.Error("move-USER fix should NOT be preferred")
					}
					if tt.wantMoveContain != "" && !strings.Contains(fix.Description, tt.wantMoveContain) {
						t.Errorf("move-USER fix description = %q, want containing %q",
							fix.Description, tt.wantMoveContain)
					}
				}
			}
			if hasMoveUser != tt.wantHasMoveUser {
				t.Errorf("hasMoveUser = %v, want %v", hasMoveUser, tt.wantHasMoveUser)
			}
		})
	}
}
