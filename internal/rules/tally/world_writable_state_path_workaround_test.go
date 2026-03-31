package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestWorldWritableStatePathWorkaroundMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewWorldWritableStatePathWorkaroundRule().Metadata())
}

func TestWorldWritableStatePathWorkaroundCheck(t *testing.T) {
	t.Parallel()
	rule := NewWorldWritableStatePathWorkaroundRule()
	testutil.RunRuleTests(t, rule, worldWritableCheckCases())
}

func TestWorldWritableStatePathWorkaroundCheckWithFixes(t *testing.T) {
	t.Parallel()
	rule := NewWorldWritableStatePathWorkaroundRule()

	tests := []struct {
		name           string
		content        string
		wantHasFix     bool
		wantFixContain string
	}{
		{
			name: "fix replaces 777 with 775",
			content: `FROM ubuntu:22.04
RUN chmod 777 /data
`,
			wantHasFix:     true,
			wantFixContain: "775",
		},
		{
			name: "fix replaces 0777 with 0775 (preserves 4-digit format)",
			content: `FROM ubuntu:22.04
RUN chmod 0777 /data
`,
			wantHasFix:     true,
			wantFixContain: "0775",
		},
		{
			name: "fix replaces 666 with 664",
			content: `FROM ubuntu:22.04
RUN chmod 666 /app/config
`,
			wantHasFix:     true,
			wantFixContain: "664",
		},
		{
			name: "no fix for symbolic mode",
			content: `FROM ubuntu:22.04
RUN chmod a+rwx /data
`,
			wantHasFix: false,
		},
		{
			name: "no fix for recursive chmod",
			content: `FROM ubuntu:22.04
RUN chmod -R 777 /data
`,
			wantHasFix: false,
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

			fix := violations[0].SuggestedFix
			if tt.wantHasFix && fix == nil {
				t.Fatal("expected a fix but got nil")
			}
			if !tt.wantHasFix && fix != nil {
				t.Fatalf("expected no fix but got: %s", fix.Description)
			}
			if tt.wantHasFix && tt.wantFixContain != "" {
				found := false
				for _, edit := range fix.Edits {
					if edit.NewText == tt.wantFixContain {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("fix edits should contain %q, got %v", tt.wantFixContain, fix.Edits)
				}
			}
		})
	}
}

// TestWorldWritableStatePathWorkaroundDistinctFixPositions verifies that two
// same-mode chmods in one RUN get distinct fix positions (regression).
func TestWorldWritableStatePathWorkaroundDistinctFixPositions(t *testing.T) {
	t.Parallel()
	rule := NewWorldWritableStatePathWorkaroundRule()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM ubuntu:22.04
RUN chmod 777 /app && chmod 777 /data
`)
	violations := rule.Check(input)
	if len(violations) != 2 {
		t.Fatalf("got %d violations, want 2", len(violations))
	}

	for i, v := range violations {
		if v.SuggestedFix == nil {
			t.Errorf("violation[%d] has no fix", i)
			continue
		}
		if len(v.SuggestedFix.Edits) == 0 {
			t.Errorf("violation[%d] fix has no edits", i)
		}
	}

	if violations[0].SuggestedFix != nil && violations[1].SuggestedFix != nil {
		col0 := violations[0].SuggestedFix.Edits[0].Location.Start.Column
		col1 := violations[1].SuggestedFix.Edits[0].Location.Start.Column
		if col0 == col1 {
			t.Errorf("both fixes target the same column %d; expected distinct positions", col0)
		}
	}
}

func worldWritableCheckCases() []testutil.RuleTestCase {
	return []testutil.RuleTestCase{
		// === Violations: octal modes ===
		{
			Name: "chmod 777 on state path",
			Content: `FROM ubuntu:22.04
RUN chmod 777 /data
`,
			WantViolations: 1,
			WantMessages:   []string{"chmod 777 on state path /data sets world-writable"},
		},
		{
			Name: "chmod 777 on /var/lib path",
			Content: `FROM ubuntu:22.04
RUN chmod 777 /var/lib/app
`,
			WantViolations: 1,
			WantMessages:   []string{"state path"},
		},
		{
			Name: "chmod 666 on state path",
			Content: `FROM ubuntu:22.04
RUN chmod 666 /var/lib/app/config
`,
			WantViolations: 1,
			WantMessages:   []string{"666"},
		},
		{
			Name: "chmod 776 (others rw) on app path",
			Content: `FROM ubuntu:22.04
RUN chmod 776 /app
`,
			WantViolations: 1,
			WantMessages:   []string{"776"},
		},
		{
			Name: "chmod 757 (others write+exec) on app path",
			Content: `FROM ubuntu:22.04
RUN chmod 757 /app
`,
			WantViolations: 1,
			WantMessages:   []string{"757"},
		},
		{
			Name: "chmod 0777 (4-digit octal)",
			Content: `FROM ubuntu:22.04
RUN chmod 0777 /data
`,
			WantViolations: 1,
			WantMessages:   []string{"0777"},
		},
		{
			Name: "chmod 777 on non-state path",
			Content: `FROM ubuntu:22.04
RUN chmod 777 /app
`,
			WantViolations: 1,
			WantMessages:   []string{"chmod 777 on /app"},
		},
		{
			Name: "chmod 777 on file path",
			Content: `FROM ubuntu:22.04
RUN chmod 777 /app/entrypoint.sh
`,
			WantViolations: 1,
			WantMessages:   []string{"/app/entrypoint.sh"},
		},

		// === Violations: symbolic modes ===
		{
			Name: "chmod a+rwx",
			Content: `FROM ubuntu:22.04
RUN chmod a+rwx /app
`,
			WantViolations: 1,
			WantMessages:   []string{"a+rwx"},
		},
		{
			Name: "chmod o+w (others write)",
			Content: `FROM ubuntu:22.04
RUN chmod o+w /app
`,
			WantViolations: 1,
			WantMessages:   []string{"o+w"},
		},
		{
			Name: "chmod +w (no who prefix = all)",
			Content: `FROM ubuntu:22.04
RUN chmod +w /app
`,
			WantViolations: 1,
			WantMessages:   []string{"+w"},
		},
		{
			Name: "chmod a=rwx (symbolic assign all)",
			Content: `FROM ubuntu:22.04
RUN chmod a=rwx /app
`,
			WantViolations: 1,
			WantMessages:   []string{"a=rwx"},
		},
		{
			Name: "chmod o+rw (others read+write)",
			Content: `FROM ubuntu:22.04
RUN chmod o+rw /app
`,
			WantViolations: 1,
			WantMessages:   []string{"o+rw"},
		},

		// === Violations: mkdir -m ===
		{
			Name: "mkdir -m 777",
			Content: `FROM ubuntu:22.04
RUN mkdir -m 777 /data
`,
			WantViolations: 1,
			WantMessages:   []string{"mkdir -m 777"},
		},
		{
			Name: "mkdir -pm 777",
			Content: `FROM ubuntu:22.04
RUN mkdir -pm 777 /data/logs
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},
		{
			Name: "mkdir --mode=777",
			Content: `FROM ubuntu:22.04
RUN mkdir --mode=777 /srv/data
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},
		{
			Name: "mkdir --mode 777",
			Content: `FROM ubuntu:22.04
RUN mkdir --mode 777 /srv/data
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},

		// === Violations: multiple targets/commands ===
		{
			Name: "chmod 777 multiple targets",
			Content: `FROM ubuntu:22.04
RUN chmod 777 /app /data
`,
			WantViolations: 2,
		},
		{
			Name: "multiple chmod in chained commands",
			Content: `FROM ubuntu:22.04
RUN chmod 777 /app && chmod 666 /data
`,
			WantViolations: 2,
		},

		// === Violations: multi-stage ===
		{
			Name: "world-writable in builder stage",
			Content: `FROM ubuntu:22.04 AS builder
RUN chmod 777 /build
FROM alpine:3.18
COPY --from=builder /build /app
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},

		// === Violations: recursive chmod (detected but no fix offered) ===
		{
			Name: "recursive chmod -R 777",
			Content: `FROM ubuntu:22.04
RUN chmod -R 777 /data
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},
		{
			Name: "recursive chmod --recursive 777",
			Content: `FROM ubuntu:22.04
RUN chmod --recursive 777 /data
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},

		// === No violations: safe octal modes ===
		{
			Name: "chmod 755 (safe)",
			Content: `FROM ubuntu:22.04
RUN chmod 755 /app
`,
			WantViolations: 0,
		},
		{
			Name: "chmod 644 (safe)",
			Content: `FROM ubuntu:22.04
RUN chmod 644 /app/config
`,
			WantViolations: 0,
		},
		{
			Name: "chmod 700 (owner only)",
			Content: `FROM ubuntu:22.04
RUN chmod 700 /app
`,
			WantViolations: 0,
		},
		{
			Name: "chmod 770 (no world write)",
			Content: `FROM ubuntu:22.04
RUN chmod 770 /data
`,
			WantViolations: 0,
		},
		{
			Name: "chmod 775 (no world write)",
			Content: `FROM ubuntu:22.04
RUN chmod 775 /data
`,
			WantViolations: 0,
		},
		{
			Name: "chmod 664 (no world write)",
			Content: `FROM ubuntu:22.04
RUN chmod 664 /app/config
`,
			WantViolations: 0,
		},

		// === No violations: safe symbolic modes ===
		{
			Name: "chmod u+x (user only)",
			Content: `FROM ubuntu:22.04
RUN chmod u+x /app/script.sh
`,
			WantViolations: 0,
		},
		{
			Name: "chmod g+w (group only, not others)",
			Content: `FROM ubuntu:22.04
RUN chmod g+w /app
`,
			WantViolations: 0,
		},
		{
			Name: "chmod g+rwx (group only)",
			Content: `FROM ubuntu:22.04
RUN chmod g+rwx /app
`,
			WantViolations: 0,
		},
		{
			Name: "chmod o+r (others read, not write)",
			Content: `FROM ubuntu:22.04
RUN chmod o+r /app
`,
			WantViolations: 0,
		},
		{
			Name: "chmod o+x (others execute, not write)",
			Content: `FROM ubuntu:22.04
RUN chmod o+x /app
`,
			WantViolations: 0,
		},
		{
			Name: "chmod +x (all execute, not write)",
			Content: `FROM ubuntu:22.04
RUN chmod +x /app/script.sh
`,
			WantViolations: 0,
		},

		// === No violations: group-only and unrecognized modes ===
		{
			Name: "chmod g=u is not flagged (unrecognized mode)",
			Content: `FROM ubuntu:22.04
RUN chmod g=u /app
`,
			WantViolations: 0,
		},

		// === Violations: chgrp does NOT suppress world-writable chmod ===
		// chgrp only changes group ownership. chmod 777 is still world-writable
		// regardless of chgrp. Valid OpenShift patterns use group-only modes
		// (g=u, g+rwx, 775) which don't trigger this rule in the first place.
		{
			Name: "chgrp + chmod 777 still flagged (world-writable despite chgrp)",
			Content: `FROM ubuntu:22.04
RUN chgrp 0 /data && chmod 777 /data
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},
		{
			Name: "chgrp parent + chmod 777 subpath still flagged",
			Content: `FROM ubuntu:22.04
RUN chgrp -R 0 /app && chmod 777 /app/data
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},

		// === No violations: mkdir safe modes ===
		{
			Name: "mkdir -m 755 (safe)",
			Content: `FROM ubuntu:22.04
RUN mkdir -m 755 /data
`,
			WantViolations: 0,
		},
		{
			Name: "mkdir without -m flag",
			Content: `FROM ubuntu:22.04
RUN mkdir -p /data
`,
			WantViolations: 0,
		},

		// === No violations: empty/trivial ===
		{
			Name: "no RUN instructions",
			Content: `FROM ubuntu:22.04
CMD ["app"]
`,
			WantViolations: 0,
		},
		{
			Name: "RUN without chmod",
			Content: `FROM ubuntu:22.04
RUN echo hello
`,
			WantViolations: 0,
		},

		// === Exec form ===
		{
			Name: "exec form chmod 777",
			Content: `FROM ubuntu:22.04
RUN ["chmod", "777", "/app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},
		{
			Name: "exec form chmod 755 (safe)",
			Content: `FROM ubuntu:22.04
RUN ["chmod", "755", "/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "exec form chmod with absolute path",
			Content: `FROM ubuntu:22.04
RUN ["/usr/bin/chmod", "777", "/app"]
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},
		{
			Name: "exec form mkdir -m 777",
			Content: `FROM ubuntu:22.04
RUN ["mkdir", "-m", "777", "/data"]
`,
			WantViolations: 1,
			WantMessages:   []string{"mkdir -m"},
		},
		{
			Name: "exec form mkdir -m 755 (safe)",
			Content: `FROM ubuntu:22.04
RUN ["mkdir", "-m", "755", "/data"]
`,
			WantViolations: 0,
		},

		// === Continuation lines ===
		{
			Name: "chmod in continuation line",
			Content: `FROM ubuntu:22.04
RUN mkdir -p /data && \
    chmod 777 /data
`,
			WantViolations: 1,
			WantMessages:   []string{"777"},
		},
	}
}
