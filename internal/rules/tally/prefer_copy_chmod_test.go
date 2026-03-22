package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferCopyChmodRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferCopyChmodRule().Metadata())
}

func TestPreferCopyChmodRule_Check(t *testing.T) {
	t.Parallel()
	testutil.RunRuleTests(t, NewPreferCopyChmodRule(), []testutil.RuleTestCase{
		// === Positive cases (should flag) ===
		{
			Name: "basic COPY + RUN chmod octal",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod 755 /app/entrypoint.sh
`,
			WantViolations: 1,
		},
		{
			Name: "COPY + RUN chmod symbolic +x",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
`,
			WantViolations: 1,
		},
		{
			Name: "COPY to directory + matching chmod",
			Content: `FROM alpine
COPY entrypoint.sh /app/
RUN chmod +x /app/entrypoint.sh
`,
			WantViolations: 1,
		},
		{
			Name: "COPY with --chown + RUN chmod",
			Content: `FROM alpine
COPY --chown=myuser:mygroup entrypoint.sh /app/entrypoint.sh
RUN chmod 755 /app/entrypoint.sh
`,
			WantViolations: 1,
		},
		{
			Name: "COPY with --from + RUN chmod",
			Content: `FROM alpine AS builder
RUN echo "build"
FROM alpine
COPY --from=builder /build/app /usr/local/bin/app
RUN chmod +x /usr/local/bin/app
`,
			WantViolations: 1,
		},
		{
			Name: "COPY with --link + RUN chmod",
			Content: `FROM alpine
COPY --link entrypoint.sh /app/entrypoint.sh
RUN chmod 755 /app/entrypoint.sh
`,
			WantViolations: 1,
		},
		{
			Name: "two COPY+chmod pairs in same stage",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
COPY healthcheck.sh /app/healthcheck.sh
RUN chmod +x /app/healthcheck.sh
`,
			WantViolations: 2,
		},
		{
			Name: "violations in separate stages",
			Content: `FROM alpine AS base
COPY script1.sh /app/script1.sh
RUN chmod +x /app/script1.sh
FROM base
COPY script2.sh /app/script2.sh
RUN chmod +x /app/script2.sh
`,
			WantViolations: 2,
		},
		{
			Name: "octal 0755 with leading zero",
			Content: `FROM alpine
COPY start.sh /start.sh
RUN chmod 0755 /start.sh
`,
			WantViolations: 1,
		},
		{
			Name: "symbolic u+x",
			Content: `FROM alpine
COPY script.sh /usr/local/bin/script.sh
RUN chmod u+x /usr/local/bin/script.sh
`,
			WantViolations: 1,
		},
		{
			Name: "COPY with leading whitespace",
			Content: `FROM alpine
 COPY entrypoint.sh /app/entrypoint.sh
 RUN chmod +x /app/entrypoint.sh
`,
			WantViolations: 1,
		},
		{
			Name: "WORKDIR relative COPY dest resolved",
			Content: `FROM alpine
WORKDIR /app
COPY script.sh .
RUN chmod +x /app/script.sh
`,
			WantViolations: 1,
		},
		{
			Name: "WORKDIR relative COPY dest with trailing slash",
			Content: `FROM alpine
WORKDIR /opt
COPY run.sh ./bin/
RUN chmod 755 /opt/bin/run.sh
`,
			WantViolations: 1,
		},

		// === Merge cases (COPY already has --chmod + RUN chmod) ===
		{
			Name: "existing --chmod + symbolic overlay",
			Content: `FROM alpine
COPY --chmod=644 script.sh /app/script.sh
RUN chmod +x /app/script.sh
`,
			WantViolations: 1,
		},
		{
			Name: "existing --chmod + octal override",
			Content: `FROM alpine
COPY --chmod=644 script.sh /app/script.sh
RUN chmod 755 /app/script.sh
`,
			WantViolations: 1,
		},
		{
			Name: "existing --chmod + redundant chmod",
			Content: `FROM alpine
COPY --chmod=777 script.sh /app/script.sh
RUN chmod +x /app/script.sh
`,
			WantViolations: 1, // still flag — RUN is redundant
		},

		// === Negative cases (should not flag) ===
		{
			Name: "COPY already has --chmod with no following RUN",
			Content: `FROM alpine
COPY --chmod=755 entrypoint.sh /app/entrypoint.sh
`,
			WantViolations: 0,
		},
		{
			Name: "non-consecutive - instruction between",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN echo "hello"
RUN chmod +x /app/entrypoint.sh
`,
			WantViolations: 0,
		},
		{
			Name: "non-consecutive - ENV between",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
ENV FOO=bar
RUN chmod +x /app/entrypoint.sh
`,
			WantViolations: 0,
		},
		{
			Name: "multiple source files",
			Content: `FROM alpine
COPY file1.sh file2.sh /app/
RUN chmod +x /app/file1.sh
`,
			WantViolations: 0,
		},
		{
			Name: "glob source pattern",
			Content: `FROM alpine
COPY *.sh /app/
RUN chmod +x /app/entrypoint.sh
`,
			WantViolations: 0,
		},
		{
			Name: "destination mismatch",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/other.sh
`,
			WantViolations: 0,
		},
		{
			Name: "COPY heredoc - no flag",
			Content: `FROM alpine
COPY <<EOF /app/config.txt
hello world
EOF
RUN chmod 644 /app/config.txt
`,
			WantViolations: 0,
		},
		{
			Name: "RUN with multiple commands - not standalone chmod",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh && echo "done"
`,
			WantViolations: 0,
		},
		{
			Name: "RUN chmod recursive - skip",
			Content: `FROM alpine
COPY app /app/
RUN chmod -R 755 /app/
`,
			WantViolations: 0,
		},
		{
			Name: "no RUN after COPY",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
`,
			WantViolations: 0,
		},
		{
			Name: "RUN after COPY is not chmod",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN echo "hello"
`,
			WantViolations: 0,
		},
		{
			Name: "relative chmod target - skip",
			Content: `FROM alpine
WORKDIR /app
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x entrypoint.sh
`,
			WantViolations: 0,
		},
		{
			Name: "exec form RUN - skip",
			Content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN ["chmod", "+x", "/app/entrypoint.sh"]
`,
			WantViolations: 0,
		},
		{
			Name: "COPY directory dest mismatch",
			Content: `FROM alpine
COPY entrypoint.sh /app/
RUN chmod +x /other/entrypoint.sh
`,
			WantViolations: 0,
		},
	})
}

func TestPreferCopyChmodRule_CheckWithFixes(t *testing.T) {
	t.Parallel()
	rule := NewPreferCopyChmodRule()

	tests := []struct {
		name           string
		content        string
		wantHasFix     bool
		wantFixContain string
	}{
		{
			name: "basic fix adds --chmod",
			content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod 755 /app/entrypoint.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=755",
		},
		{
			name: "symbolic mode preserved in fix",
			content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=+x",
		},
		{
			name: "fix preserves existing --chown",
			content: `FROM alpine
COPY --chown=myuser:mygroup entrypoint.sh /app/entrypoint.sh
RUN chmod 755 /app/entrypoint.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=755",
		},
		{
			name: "fix with directory destination",
			content: `FROM alpine
COPY entrypoint.sh /app/
RUN chmod +x /app/entrypoint.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=+x",
		},
		{
			name: "fix with 0755 octal preserves leading zero",
			content: `FROM alpine
COPY start.sh /start.sh
RUN chmod 0755 /start.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=0755",
		},
		{
			name: "fix with u+x symbolic mode",
			content: `FROM alpine
COPY script.sh /usr/local/bin/script.sh
RUN chmod u+x /usr/local/bin/script.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=u+x",
		},
		{
			name: "fix description includes mode",
			content: `FROM alpine
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
`,
			wantHasFix:     true,
			wantFixContain: "COPY --chmod=+x",
		},
		{
			name: "fix handles leading whitespace on COPY line",
			content: `FROM alpine
 COPY entrypoint.sh /app/entrypoint.sh
 RUN chmod +x /app/entrypoint.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=+x ",
		},
		{
			name: "merge symbolic +x onto existing octal 644",
			content: `FROM alpine
COPY --chmod=644 script.sh /app/script.sh
RUN chmod +x /app/script.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=0755",
		},
		{
			name: "octal override replaces existing",
			content: `FROM alpine
COPY --chmod=644 script.sh /app/script.sh
RUN chmod 755 /app/script.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=755",
		},
		{
			name: "redundant chmod on 777 still produces fix",
			content: `FROM alpine
COPY --chmod=777 script.sh /app/script.sh
RUN chmod +x /app/script.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=0777",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, nil)
			violations := rule.Check(input)

			if len(violations) == 0 {
				if tt.wantHasFix {
					t.Fatal("expected violation with fix, got none")
				}
				return
			}

			v := violations[0]
			if tt.wantHasFix {
				if v.SuggestedFix == nil {
					t.Fatal("expected suggested fix, got nil")
				}
				// Check that fix description or edit text contains expected substring
				var sb strings.Builder
				sb.WriteString(v.SuggestedFix.Description)
				for _, edit := range v.SuggestedFix.Edits {
					sb.WriteString(" ")
					sb.WriteString(edit.NewText)
				}
				fixText := sb.String()
				if !strings.Contains(fixText, tt.wantFixContain) {
					t.Errorf("fix text %q does not contain %q", fixText, tt.wantFixContain)
				}
			}
		})
	}
}
