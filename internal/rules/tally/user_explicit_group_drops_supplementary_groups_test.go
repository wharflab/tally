package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestUserExplicitGroupDropsSupplementaryGroupsMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewUserExplicitGroupDropsSupplementaryGroupsRule().Metadata())
}

func TestUserExplicitGroupDropsSupplementaryGroupsCheck(t *testing.T) {
	t.Parallel()

	rule := NewUserExplicitGroupDropsSupplementaryGroupsRule()

	testutil.RunRuleTests(t, rule, []testutil.RuleTestCase{
		// --- Linux fires ---
		{
			Name: "useradd -G single group fires",
			Content: `FROM ubuntu:22.04
RUN useradd -G docker app
USER app:app
`,
			WantViolations: 1,
			WantMessages:   []string{`USER "app" specifies explicit group "app"`},
		},
		{
			Name: "useradd -G comma list fires",
			Content: `FROM ubuntu:22.04
RUN useradd -G docker,wheel app
USER app:app
`,
			WantViolations: 1,
			WantMessages:   []string{"(docker, wheel)"},
		},
		{
			Name: "useradd --groups=value fires",
			Content: `FROM ubuntu:22.04
RUN useradd --groups=docker app
USER app:app
`,
			WantViolations: 1,
		},
		{
			Name: "useradd -g primary and -G supplementary fires",
			Content: `FROM ubuntu:22.04
RUN useradd -g app -G docker app
USER app:app
`,
			WantViolations: 1,
		},
		{
			Name: "usermod -aG fires",
			Content: `FROM ubuntu:22.04
RUN useradd app && usermod -aG docker app
USER app:app
`,
			WantViolations: 1,
		},
		{
			Name: "usermod -a -G split fires",
			Content: `FROM ubuntu:22.04
RUN useradd app && usermod -a -G docker app
USER app:app
`,
			WantViolations: 1,
		},
		{
			Name: "usermod -G replace form fires",
			Content: `FROM ubuntu:22.04
RUN useradd app && usermod -G docker app
USER app:app
`,
			WantViolations: 1,
		},
		{
			Name: "gpasswd -a fires",
			Content: `FROM ubuntu:22.04
RUN useradd alice && gpasswd -a alice wheel
USER alice:alice
`,
			WantViolations: 1,
		},
		{
			Name: "adduser USER GROUP BusyBox membership fires",
			Content: `FROM alpine:3.19
RUN adduser -D alice && adduser alice wheel
USER alice:alice
`,
			WantViolations: 1,
		},
		{
			Name: "addgroup USER GROUP membership fires",
			Content: `FROM alpine:3.19
RUN adduser -D alice && addgroup alice docker
USER alice:alice
`,
			WantViolations: 1,
		},
		{
			Name: "FROM ancestry parent useradd -G child USER",
			Content: `FROM ubuntu:22.04 AS base
RUN useradd -G docker app

FROM base AS final
USER app:app
`,
			WantViolations: 1,
		},
		{
			Name: "heredoc useradd -G fires",
			Content: `FROM ubuntu:22.04
RUN <<EOF
useradd -G docker app
EOF
USER app:app
`,
			WantViolations: 1,
		},

		// --- Linux no-fires ---
		{
			Name: "useradd -g primary only no fire",
			Content: `FROM ubuntu:22.04
RUN useradd -g app app
USER app:app
`,
			WantViolations: 0,
		},
		{
			Name: "bare USER name no fire",
			Content: `FROM ubuntu:22.04
RUN useradd -G docker app
USER app
`,
			WantViolations: 0,
		},
		{
			Name: "different username in USER no fire",
			Content: `FROM ubuntu:22.04
RUN useradd -G docker app
USER alice:alice
`,
			WantViolations: 0,
		},
		{
			Name: "addgroup creation only no fire",
			Content: `FROM alpine:3.19
RUN addgroup -S docker && adduser -D alice
USER alice:alice
`,
			WantViolations: 0,
		},
		{
			Name: "numeric USER no fire",
			Content: `FROM ubuntu:22.04
RUN useradd -u 1000 -G docker app
USER 1000:1000
`,
			WantViolations: 0,
		},
		{
			Name: "root USER no fire",
			Content: `FROM ubuntu:22.04
RUN useradd -G docker app
USER root:root
`,
			WantViolations: 0,
		},
		{
			Name: "passwd-less scratch stage yields to named-identity rule",
			Content: `FROM golang:1.22 AS builder
RUN useradd -G docker app

FROM scratch
COPY --from=builder /myapp /myapp
USER app:app
`,
			WantViolations: 0,
		},
		{
			Name: "parent supplementary but child USER without group",
			Content: `FROM ubuntu:22.04 AS base
RUN useradd -G docker app

FROM base AS final
USER app
`,
			WantViolations: 0,
		},

		// --- Windows fires ---
		{
			Name: "windows cmd net localgroup fires",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN net user app password /add && net localgroup Administrators app /add
USER app:Administrators
`,
			WantViolations: 1,
			WantMessages:   []string{`USER "app" specifies explicit group "Administrators"`},
		},
		{
			Name: "windows PowerShell Add-LocalGroupMember fires",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["pwsh", "-Command"]
RUN New-LocalUser -Name app -NoPassword -AccountNeverExpires ; Add-LocalGroupMember -Group docker -Member app
USER app:docker
`,
			WantViolations: 1,
		},
		{
			Name: "windows case-insensitive matching fires",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["pwsh", "-Command"]
RUN Add-LocalGroupMember -Group docker -Member App
USER app:docker
`,
			WantViolations: 1,
		},

		// --- Windows no-fires ---
		{
			Name: "windows bare USER no fire",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN net user app password /add && net localgroup Administrators app /add
USER app
`,
			WantViolations: 0,
		},
	})
}

func TestUserExplicitGroupDropsSupplementaryGroupsCheckWithFixes(t *testing.T) {
	t.Parallel()

	rule := NewUserExplicitGroupDropsSupplementaryGroupsRule()

	tests := []struct {
		name           string
		content        string
		wantHasFix     bool
		wantFixContain string
		wantSafety     rules.FixSafety
	}{
		{
			name: "linux USER name:group dropped to USER name",
			content: `FROM ubuntu:22.04
RUN useradd -G docker app
USER app:app
`,
			wantHasFix:     true,
			wantFixContain: "app",
			wantSafety:     rules.FixSuggestion,
		},
		{
			name: "linux USER name:different-group dropped",
			content: `FROM ubuntu:22.04
RUN useradd -g staff -G docker app
USER app:docker
`,
			wantHasFix:     true,
			wantFixContain: "app",
			wantSafety:     rules.FixSuggestion,
		},
		{
			name: "windows USER app:Administrators dropped",
			content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN net user app pass /add && net localgroup Administrators app /add
USER app:Administrators
`,
			wantHasFix:     true,
			wantFixContain: "app",
			wantSafety:     rules.FixSuggestion,
		},
		{
			name: "continuation line USER operand on second line",
			content: `FROM ubuntu:22.04
RUN useradd -G docker app
USER \
  app:app
`,
			wantHasFix:     true,
			wantFixContain: "app",
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
			if !hasFix {
				return
			}
			if v.SuggestedFix.Safety != tt.wantSafety {
				t.Errorf("safety = %v, want %v", v.SuggestedFix.Safety, tt.wantSafety)
			}
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
		})
	}
}

func TestUserExplicitGroupDropsSupplementaryGroupsFixApplies(t *testing.T) {
	t.Parallel()

	rule := NewUserExplicitGroupDropsSupplementaryGroupsRule()

	tests := []struct {
		name   string
		before string
		after  string
	}{
		{
			name: "linux simple drop",
			before: `FROM ubuntu:22.04
RUN useradd -G docker app
USER app:app
`,
			after: `FROM ubuntu:22.04
RUN useradd -G docker app
USER app
`,
		},
		{
			name: "linux preserves indentation",
			before: `FROM ubuntu:22.04
RUN useradd -G docker app
   USER app:docker
`,
			after: `FROM ubuntu:22.04
RUN useradd -G docker app
   USER app
`,
		},
		{
			name: "windows net localgroup",
			before: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN net user app pass /add && net localgroup Administrators app /add
USER app:Administrators
`,
			after: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN net user app pass /add && net localgroup Administrators app /add
USER app
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.before)
			violations := rule.Check(input)
			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}
			got := string(fix.ApplyFix([]byte(tt.before), violations[0].SuggestedFix))
			if got != tt.after {
				t.Errorf("after-fix mismatch\ngot:\n%s\nwant:\n%s", got, tt.after)
			}
		})
	}
}
