package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferCopyHeredocRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferCopyHeredocRule().Metadata())
}

func TestPreferCopyHeredocRule_DefaultConfig(t *testing.T) {
	t.Parallel()
	rule := NewPreferCopyHeredocRule()
	cfg, ok := rule.DefaultConfig().(PreferCopyHeredocConfig)
	if !ok {
		t.Fatal("DefaultConfig did not return PreferCopyHeredocConfig")
	}

	if cfg.CheckSingleRun == nil || !*cfg.CheckSingleRun {
		t.Errorf("CheckSingleRun = %v, want true", cfg.CheckSingleRun)
	}
	if cfg.CheckConsecutiveRuns == nil || !*cfg.CheckConsecutiveRuns {
		t.Errorf("CheckConsecutiveRuns = %v, want true", cfg.CheckConsecutiveRuns)
	}
}

func TestPreferCopyHeredocRule_Check(t *testing.T) {
	t.Parallel()
	testutil.RunRuleTests(t, NewPreferCopyHeredocRule(), []testutil.RuleTestCase{
		{
			Name: "simple echo to file",
			Content: `FROM alpine
RUN echo "hello world" > /app/config.txt
`,
			WantViolations: 1,
		},
		{
			Name: "echo with chmod",
			Content: `FROM alpine
RUN echo "#!/bin/bash\necho hello" > /app/script.sh && chmod 755 /app/script.sh
`,
			WantViolations: 1,
		},
		{
			Name: "echo with symbolic chmod +x",
			Content: `FROM alpine
RUN echo "#!/bin/bash" > /app/script.sh && chmod +x /app/script.sh
`,
			WantViolations: 1,
		},
		{
			Name: "relative path - no violation",
			Content: `FROM alpine
RUN echo "data" > config.txt
`,
			WantViolations: 0,
		},
		{
			Name: "append mode in single run - no violation",
			Content: `FROM alpine
RUN echo "line1" >> /app/log.txt
`,
			WantViolations: 0,
		},
		{
			Name: "consecutive file creations to same file",
			Content: `FROM alpine
RUN echo "line1" > /app/config
RUN echo "line2" >> /app/config
`,
			WantViolations: 1,
		},
		{
			Name: "consecutive append-only runs - no violation",
			Content: `FROM alpine
RUN echo "line1" >> /app/log.txt
RUN echo "line2" >> /app/log.txt
`,
			WantViolations: 0, // Can't fold append-only into sequence (unknown base content)
		},
		{
			Name: "mixed-command run not folded into sequence",
			Content: `FROM alpine
RUN apt-get update && echo "a" > /app/log
RUN echo "b" >> /app/log
`,
			WantViolations: 1, // Only the mixed-command single RUN, not a sequence
		},
		{
			Name: "consecutive to different files - two violations",
			Content: `FROM alpine
RUN echo "a" > /app/file1
RUN echo "b" > /app/file2
`,
			WantViolations: 2,
		},
		{
			Name: "non-file-creation command - no violation",
			Content: `FROM alpine
RUN apt-get update
`,
			WantViolations: 0,
		},
		{
			Name: "exec form - no violation",
			Content: `FROM alpine
RUN ["echo", "hello"]
`,
			WantViolations: 0,
		},
		{
			Name: "heredoc already - no violation",
			Content: `FROM alpine
RUN <<EOF
echo "hello" > /app/file
EOF
`,
			WantViolations: 0,
		},
		{
			Name: "cat heredoc to file",
			Content: `FROM alpine
RUN cat <<EOF > /app/config.txt
hello world
EOF
`,
			WantViolations: 1, // cat <<EOF > file should be converted to COPY <<EOF
		},
		{
			Name: "printf with escape sequences to file",
			Content: `FROM alpine
RUN printf '#ifndef H\n#define H\n#endif\n' > /usr/include/h.h
`,
			WantViolations: 1,
		},
		{
			Name: "printf with percent-s and newline escape to file",
			Content: `FROM alpine
RUN printf '%s\n' 'hello world' > /app/greeting.txt
`,
			WantViolations: 1,
		},
		{
			Name: "printf with literal percent to file",
			Content: `FROM alpine
RUN printf 'rate=100%%\n' > /app/status
`,
			WantViolations: 1,
		},
		{
			Name: "disable single run check",
			Content: `FROM alpine
RUN echo "hello" > /app/config
`,
			Config: map[string]any{
				"check-single-run": false,
			},
			WantViolations: 0,
		},
		{
			Name: "disable consecutive check - still catches single",
			Content: `FROM alpine
RUN echo "hello" > /app/config
`,
			Config: map[string]any{
				"check-consecutive-runs": false,
			},
			WantViolations: 1,
		},
		{
			Name: "disable consecutive check - sequence RUNs reported individually",
			Content: `FROM alpine
RUN echo "line1" > /app/config
RUN chmod 755 /app/config
`,
			Config: map[string]any{
				"check-consecutive-runs": false,
			},
			WantViolations: 1, // First RUN reported as single violation; chmod alone can't convert
		},
		{
			Name: "disable consecutive check - consecutive overwrites still flagged",
			Content: `FROM alpine
RUN echo "a" > /app/config
RUN echo "b" > /app/config
`,
			Config: map[string]any{
				"check-consecutive-runs": false,
			},
			WantViolations: 2, // Each overwrite reported as single violation
		},
		{
			Name: "mixed commands with file creation",
			Content: `FROM alpine
RUN apt-get update && echo "done" > /app/log
`,
			WantViolations: 1, // File creation can be extracted from mixed commands
		},
		// Mount handling tests
		{
			Name: "bind mount - skip (content might depend on bound files)",
			Content: `FROM alpine
RUN --mount=type=bind,source=./config,target=/mnt/config echo "data" > /app/file
`,
			WantViolations: 0, // Skip: bind mount might affect content
		},
		{
			Name: "cache mount - file target under cache path - skip",
			Content: `FROM alpine
RUN --mount=type=cache,target=/var/cache/apt echo "marker" > /var/cache/apt/done
`,
			WantViolations: 0, // Skip: file in cache won't persist
		},
		{
			Name: "cache mount - file target outside cache - detect",
			Content: `FROM alpine
RUN --mount=type=cache,target=/var/cache/apt apt-get update && echo "done" > /app/status
`,
			WantViolations: 1, // Safe: file creation is outside cache mount
		},
		{
			Name: "tmpfs mount - file target under tmpfs - skip",
			Content: `FROM alpine
RUN --mount=type=tmpfs,target=/tmp/build echo "temp" > /tmp/build/data
`,
			WantViolations: 0, // Skip: file in tmpfs won't persist
		},
		{
			Name: "tmpfs mount - file target outside tmpfs - detect",
			Content: `FROM alpine
RUN --mount=type=tmpfs,target=/tmp/build compile && echo "done" > /app/status
`,
			WantViolations: 1, // Safe: file creation is outside tmpfs
		},
		{
			Name: "secret mount - file target outside secret - detect",
			Content: `FROM alpine
RUN --mount=type=secret,id=npm,target=/root/.npmrc npm install && echo "installed" > /app/status
`,
			WantViolations: 1, // Safe: literal content, not using secret
		},
		{
			Name: "ssh mount - always safe for literal content",
			Content: `FROM alpine
RUN --mount=type=ssh git clone git@github.com:user/repo && echo "cloned" > /app/status
`,
			WantViolations: 1, // Safe: SSH doesn't affect file content
		},
	})
}

func TestPreferCopyHeredocRule_CheckWithFixes(t *testing.T) {
	t.Parallel()
	rule := NewPreferCopyHeredocRule()

	tests := []struct {
		name           string
		content        string
		wantHasFix     bool
		wantFixContain string
	}{
		{
			name: "simple echo has fix with COPY",
			content: `FROM alpine
RUN echo "hello" > /app/config
`,
			wantHasFix:     true,
			wantFixContain: "COPY <<EOF /app/config",
		},
		{
			name: "echo with chmod has fix with --chmod",
			content: `FROM alpine
RUN echo "script" > /app/run.sh && chmod 755 /app/run.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=755",
		},
		{
			name: "multi-line RUN with continuation",
			content: `FROM alpine
RUN echo "hello" \
  > /app/config
`,
			wantHasFix:     true,
			wantFixContain: "COPY <<EOF /app/config",
		},
		{
			name: "cat heredoc followed by separate chmod",
			content: `FROM alpine
RUN cat <<EOF > /app/script.sh
#!/bin/bash
echo hello
EOF
RUN chmod 755 /app/script.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=755",
		},
		{
			name: "symbolic chmod +x preserved as-is",
			content: `FROM alpine
RUN echo "#!/bin/bash" > /app/script.sh && chmod +x /app/script.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=+x",
		},
		{
			name: "symbolic chmod u+x preserved as-is",
			content: `FROM alpine
RUN echo "data" > /app/file && chmod u+x /app/file
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=u+x",
		},
		{
			name: "cache mount preserved on remaining commands",
			content: `FROM alpine
RUN --mount=type=cache,target=/var/cache/apt apt-get update && echo "done" > /app/status && apt-get clean
`,
			wantHasFix:     true,
			wantFixContain: "--mount=type=cache,target=/var/cache/apt",
		},
		{
			name: "ssh mount preserved on remaining commands",
			content: `FROM alpine
RUN --mount=type=ssh git clone git@github.com:user/repo && echo "cloned" > /app/status
`,
			wantHasFix:     true,
			wantFixContain: "--mount=type=ssh",
		},
		{
			name: "printf with escapes has fix with COPY",
			content: `FROM alpine
RUN printf '#ifndef H\n#define H\n#endif\n' > /usr/include/h.h
`,
			wantHasFix:     true,
			wantFixContain: "COPY <<EOF /usr/include/h.h",
		},
		{
			name: "printf with chmod has fix with --chmod",
			content: `FROM alpine
RUN printf '#!/bin/sh\nexec app\n' > /app/run.sh && chmod +x /app/run.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=+x",
		},
		{
			name: "printf literal percent has fix with correct content",
			content: `FROM alpine
RUN printf 'rate=100%%\n' > /app/status
`,
			wantHasFix:     true,
			wantFixContain: "rate=100%",
		},
		{
			name: "chmod between writes preserved",
			content: `FROM alpine
RUN echo "a" > /app/file
RUN chmod 755 /app/file
RUN echo "b" >> /app/file
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=755",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, nil)
			violations := rule.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			hasFix := v.SuggestedFix != nil
			if hasFix != tt.wantHasFix {
				t.Errorf("violation has fix = %v, want %v", hasFix, tt.wantHasFix)
			}

			if hasFix && tt.wantFixContain != "" {
				if len(v.SuggestedFix.Edits) == 0 {
					t.Error("expected fix to have edits")
				} else if !strings.Contains(v.SuggestedFix.Edits[0].NewText, tt.wantFixContain) {
					t.Errorf("fix text = %q, want to contain %q", v.SuggestedFix.Edits[0].NewText, tt.wantFixContain)
				}
			}
		})
	}
}

func TestPreferCopyHeredocRule_RunShellContext(t *testing.T) {
	t.Parallel()

	content := `FROM ubuntu:22.04
RUN echo "#! /bin/bash\n\n# script to activate the conda environment" > /app/default-shell
SHELL ["/bin/bash", "-c"]
RUN echo "#! /bin/bash\n\n# script to activate the conda environment" > /app/bash-shell
`

	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", content, nil)
	violations := NewPreferCopyHeredocRule().Check(input)
	if len(violations) != 2 {
		t.Fatalf("got %d violations, want 2", len(violations))
	}

	firstFix := violations[0].SuggestedFix
	if firstFix == nil || len(firstFix.Edits) == 0 {
		t.Fatal("expected first violation to have a fix")
	}
	if got := firstFix.Edits[0].NewText; !strings.Contains(got, "#! /bin/bash\n\n# script to activate the conda environment") {
		t.Fatalf("first fix text = %q, want actual newlines in COPY content", got)
	}

	secondFix := violations[1].SuggestedFix
	if secondFix == nil || len(secondFix.Edits) == 0 {
		t.Fatal("expected second violation to have a fix")
	}
	if got := secondFix.Edits[0].NewText; !strings.Contains(got, "#! /bin/bash\\n\\n# script to activate the conda environment") {
		t.Fatalf("second fix text = %q, want literal \\\\n escapes preserved under bash", got)
	}
}

func TestPreferCopyHeredocRule_AshNoEscapeInterpretation(t *testing.T) {
	t.Parallel()

	// Alpine uses ash (BusyBox), where plain echo does NOT interpret
	// backslash escapes. The COPY heredoc must preserve literal \n.
	content := `FROM alpine:3.20
RUN echo "#! /bin/sh\n\nset -e" > /app/entrypoint.sh
SHELL ["/bin/ash", "-c"]
RUN echo "#! /bin/ash\n\nset -e" > /app/explicit-ash.sh
`

	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", content, nil)
	violations := NewPreferCopyHeredocRule().Check(input)
	if len(violations) != 2 {
		t.Fatalf("got %d violations, want 2", len(violations))
	}

	// Both fixes should preserve literal \n (no escape interpretation).
	for i, v := range violations {
		fix := v.SuggestedFix
		if fix == nil || len(fix.Edits) == 0 {
			t.Fatalf("violation[%d]: expected a fix with edits", i)
		}
		got := fix.Edits[0].NewText
		if strings.Contains(got, "#! /bin/sh\n\nset -e") || strings.Contains(got, "#! /bin/ash\n\nset -e") {
			t.Fatalf("violation[%d]: fix text = %q, should NOT contain actual newlines from escape interpretation on ash", i, got)
		}
		if !strings.Contains(got, `\n`) {
			t.Fatalf("violation[%d]: fix text = %q, want literal \\n preserved", i, got)
		}
	}
}

func TestPreferCopyHeredocRule_TildeTargetFixes(t *testing.T) {
	t.Parallel()
	rule := NewPreferCopyHeredocRule()

	tests := []struct {
		name       string
		content    string
		wantPath   string
		wantSafety string
		wantFix    bool
		wantCount  int
	}{
		{
			name: "implicit root home resolves to /root",
			content: `FROM alpine
RUN echo "hello" > ~/.bashrc
`,
			wantPath:   "COPY <<EOF /root/.bashrc",
			wantSafety: "unsafe",
			wantFix:    true,
			wantCount:  1,
		},
		{
			name: "named user falls back to /home/<user>",
			content: `FROM alpine
USER app
RUN echo "hello" > ~/.bashrc
`,
			wantPath:   "COPY <<EOF /home/app/.bashrc",
			wantSafety: "unsafe",
			wantFix:    true,
			wantCount:  1,
		},
		{
			name: "useradd custom home is preserved",
			content: `FROM alpine
RUN useradd -m -d /srv/app app
USER app
RUN echo "hello" > ~/.bashrc
`,
			wantPath:   "COPY <<EOF /srv/app/.bashrc",
			wantSafety: "unsafe",
			wantFix:    true,
			wantCount:  1,
		},
		{
			name: "numeric user home is not guessed",
			content: `FROM alpine
USER 1000
RUN echo "hello" > ~/.bashrc
`,
			wantFix:   false,
			wantCount: 0,
		},
		{
			name: "variable user home is not guessed",
			content: `FROM alpine
ARG APP_USER=app
USER $APP_USER
RUN echo "hello" > ~/.bashrc
`,
			wantFix:   false,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, nil)
			violations := rule.Check(input)

			if len(violations) != tt.wantCount {
				t.Fatalf("got %d violations, want %d", len(violations), tt.wantCount)
			}
			if !tt.wantFix {
				return
			}

			fix := violations[0].SuggestedFix
			if fix == nil {
				t.Fatal("expected suggested fix")
			}
			if got := fix.Safety.String(); got != tt.wantSafety {
				t.Fatalf("fix safety = %q, want %q", got, tt.wantSafety)
			}
			if len(fix.Edits) == 0 || !strings.Contains(fix.Edits[0].NewText, tt.wantPath) {
				t.Fatalf("fix text = %q, want to contain %q", fix.Edits[0].NewText, tt.wantPath)
			}
		})
	}
}

func TestPreferCopyHeredocRule_ValidateConfig(t *testing.T) {
	t.Parallel()
	rule := NewPreferCopyHeredocRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name: "valid config",
			config: map[string]any{
				"check-single-run":       false,
				"check-consecutive-runs": true,
			},
			wantErr: false,
		},
		{
			name: "invalid additional property",
			config: map[string]any{
				"unknown-field": true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := rule.ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildCopyHeredoc(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		targetPath   string
		content      string
		rawChmodMode string
	}{
		{
			name:       "simple content",
			targetPath: "/app/config",
			content:    "hello world\n",
		},
		{
			name:         "with octal chmod",
			targetPath:   "/app/script.sh",
			content:      "#!/bin/bash\necho hello\n",
			rawChmodMode: "0755",
		},
		{
			name:         "with symbolic chmod",
			targetPath:   "/app/run.sh",
			content:      "#!/bin/sh\nexec app\n",
			rawChmodMode: "+x",
		},
		{
			name:       "content containing EOF",
			targetPath: "/app/file",
			content:    "Some EOF text\n",
		},
		{
			name:       "empty content creates 0-byte file",
			targetPath: "/app/empty",
			content:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildCopyHeredoc(tt.targetPath, tt.content, tt.rawChmodMode)
			snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, got)
		})
	}
}

func TestChooseDelimiter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "no conflict",
			content: "hello world",
			want:    "EOF",
		},
		{
			name:    "contains EOF",
			content: "Some EOF text",
			want:    "CONTENT",
		},
		{
			name:    "contains EOF and CONTENT",
			content: "EOF and CONTENT here",
			want:    "FILE",
		},
		{
			name:    "contains all standard delimiters",
			content: "EOF CONTENT FILE DATA END",
			want:    "EOF1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := chooseDelimiter(tt.content)
			if got != tt.want {
				t.Errorf("chooseDelimiter() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPreferCopyHeredocRule_CrossRuleInteraction verifies that prefer-copy-heredoc
// and prefer-run-heredoc do not both fire for printf file creation patterns.
// prefer-copy-heredoc should handle these; prefer-run-heredoc should not.
func TestPreferCopyHeredocRule_CrossRuleInteraction(t *testing.T) {
	t.Parallel()

	content := `FROM alpine
RUN printf '#!/bin/sh\nexec app\n' > /app/run.sh
`
	// prefer-copy-heredoc should detect the printf file creation
	copyRule := NewPreferCopyHeredocRule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", content, nil)
	copyViolations := copyRule.Check(input)
	if len(copyViolations) != 1 {
		t.Errorf("prefer-copy-heredoc: got %d violations, want 1", len(copyViolations))
	}

	// prefer-run-heredoc should NOT fire (single command, below min-commands threshold)
	runRule := NewPreferHeredocRule()
	runInput := testutil.MakeLintInputWithConfig(t, "Dockerfile", content, nil)
	runViolations := runRule.Check(runInput)
	if len(runViolations) != 0 {
		t.Errorf("prefer-run-heredoc: got %d violations, want 0 (should yield to prefer-copy-heredoc)", len(runViolations))
	}
}
