package tally

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestPreferCopyHeredocRule_Metadata(t *testing.T) {
	rule := NewPreferCopyHeredocRule()
	meta := rule.Metadata()

	if meta.Code != "tally/prefer-copy-heredoc" {
		t.Errorf("Code = %q, want %q", meta.Code, "tally/prefer-copy-heredoc")
	}
	if meta.DefaultSeverity != rules.SeverityStyle {
		t.Errorf("DefaultSeverity = %v, want %v", meta.DefaultSeverity, rules.SeverityStyle)
	}
	if meta.Category != "style" {
		t.Errorf("Category = %q, want %q", meta.Category, "style")
	}
	if meta.FixPriority != 99 {
		t.Errorf("FixPriority = %d, want %d", meta.FixPriority, 99)
	}
}

func TestPreferCopyHeredocRule_DefaultConfig(t *testing.T) {
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
			Name: "mixed commands with file creation",
			Content: `FROM alpine
RUN apt-get update && echo "done" > /app/log
`,
			WantViolations: 1, // File creation can be extracted from mixed commands
		},
	})
}

func TestPreferCopyHeredocRule_CheckWithFixes(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			err := rule.ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildCopyHeredoc(t *testing.T) {
	tests := []struct {
		name       string
		targetPath string
		content    string
		chmodMode  string
		want       string
	}{
		{
			name:       "simple content",
			targetPath: "/app/config",
			content:    "hello world\n",
			chmodMode:  "",
			want: `COPY <<EOF /app/config
hello world
EOF`,
		},
		{
			name:       "with chmod",
			targetPath: "/app/script.sh",
			content:    "#!/bin/bash\necho hello\n",
			chmodMode:  "755",
			want: `COPY --chmod=0755 <<EOF /app/script.sh
#!/bin/bash
echo hello
EOF`,
		},
		{
			name:       "content containing EOF",
			targetPath: "/app/file",
			content:    "Some EOF text\n",
			chmodMode:  "",
			want: `COPY <<CONTENT /app/file
Some EOF text
CONTENT`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCopyHeredoc(tt.targetPath, tt.content, tt.chmodMode)
			if got != tt.want {
				t.Errorf("buildCopyHeredoc() =\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestChooseDelimiter(t *testing.T) {
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
			got := chooseDelimiter(tt.content)
			if got != tt.want {
				t.Errorf("chooseDelimiter() = %q, want %q", got, tt.want)
			}
		})
	}
}
