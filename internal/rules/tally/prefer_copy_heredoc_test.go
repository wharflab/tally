package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/testutil"
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
			wantFixContain: "--chmod=0755",
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
			wantFixContain: "--chmod=0755",
		},
		{
			name: "symbolic chmod +x converts to 0755",
			content: `FROM alpine
RUN echo "#!/bin/bash" > /app/script.sh && chmod +x /app/script.sh
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=0755",
		},
		{
			name: "symbolic chmod u+x converts to 0744",
			content: `FROM alpine
RUN echo "data" > /app/file && chmod u+x /app/file
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=0744",
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
			name: "chmod between writes preserved",
			content: `FROM alpine
RUN echo "a" > /app/file
RUN chmod 755 /app/file
RUN echo "b" >> /app/file
`,
			wantHasFix:     true,
			wantFixContain: "--chmod=0755",
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
		name       string
		targetPath string
		content    string
		chmodMode  uint16
		want       string
	}{
		{
			name:       "simple content",
			targetPath: "/app/config",
			content:    "hello world\n",
			chmodMode:  0,
			want:       "COPY <<EOF /app/config\nhello world\nEOF",
		},
		{
			name:       "with chmod",
			targetPath: "/app/script.sh",
			content:    "#!/bin/bash\necho hello\n",
			chmodMode:  0o755,
			want:       "COPY --chmod=0755 <<EOF /app/script.sh\n#!/bin/bash\necho hello\nEOF",
		},
		{
			name:       "content containing EOF",
			targetPath: "/app/file",
			content:    "Some EOF text\n",
			chmodMode:  0,
			want:       "COPY <<CONTENT /app/file\nSome EOF text\nCONTENT",
		},
		{
			name:       "empty content creates 0-byte file",
			targetPath: "/app/empty",
			content:    "",
			chmodMode:  0,
			want:       "COPY <<EOF /app/empty\nEOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildCopyHeredoc(tt.targetPath, tt.content, tt.chmodMode)
			if got != tt.want {
				t.Errorf("buildCopyHeredoc() =\n%s\nwant:\n%s", got, tt.want)
			}
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
