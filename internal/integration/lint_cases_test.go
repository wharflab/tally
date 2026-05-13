package integration

import (
	"strings"
	"testing"
)

func lintCases(t *testing.T) []lintCase {
	t.Helper()

	mustSelectRules := func(rules ...string) []string {
		t.Helper()
		args, err := selectRules(rules...)
		if err != nil {
			t.Fatalf("build rule-selection args: %v", err)
		}
		return args
	}

	return []lintCase{
		{
			name:     "config-file-discovery",
			dir:      "with-config",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/max-lines")...),
			wantExit: 1,
		},
		{
			name: "config-skip-options",
			dir:  "with-blanks-and-comments",
			args: append([]string{"--format", "json"}, mustSelectRules("tally/max-lines")...),
		},
		{
			name: "cli-overrides-config",
			dir:  "with-config",
			args: append([]string{"--max-lines", "100", "--format", "json"}, mustSelectRules("tally/max-lines")...),
		},
		{
			name:     "env-var-override",
			dir:      "simple",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/max-lines")...),
			env:      []string{"TALLY_RULES_MAX_LINES_MAX=2"},
			wantExit: 1,
		},
		{
			name: "deprecated-rule-alias-inline",
			dir:  "deprecated-rule-alias-inline",
			args: append([]string{"--format", "json"},
				mustSelectRules("buildkit/ReservedStageName")...),
			afterLint: func(t *testing.T, stderr string) {
				t.Helper()
				if !strings.Contains(stderr, "rule hadolint/DL3063 is deprecated; use buildkit/ReservedStageName instead") {
					t.Fatalf("expected deprecated rule warning in stderr, got:\n%s", stderr)
				}
			},
		},
		{
			name:     "inline-unused-directive",
			dir:      "inline-unused-directive",
			args:     append([]string{"--format", "json", "--warn-unused-directives"}, mustSelectRules("hadolint/DL3006")...),
			wantExit: 1,
		},
		{
			name:     "inline-directives-disabled",
			dir:      "inline-directives-disabled",
			args:     append([]string{"--format", "json", "--no-inline-directives"}, mustSelectRules("buildkit/StageNameCasing")...),
			wantExit: 1,
		},
		{
			name: "inline-require-reason",
			dir:  "inline-require-reason",
			args: append(
				[]string{"--format", "json", "--require-reason"},
				mustSelectRules("buildkit/StageNameCasing", "tally/max-lines")...),
			wantExit: 1,
		},
		{
			name: "format-sarif",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "sarif"}, mustSelectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
			wantExit: 1,
			snapExt:  ".sarif",
		},
		{
			name: "format-github-actions",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "github-actions"}, mustSelectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
			wantExit: 1,
			snapExt:  ".txt",
			snapRaw:  true,
		},
		{
			name: "format-markdown",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "markdown"}, mustSelectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
			wantExit: 1,
			snapExt:  ".md",
			snapRaw:  true,
		},
		{
			name:       "context-copy-ignored",
			dir:        "context-copy-ignored",
			args:       append([]string{"--format", "json"}, mustSelectRules("buildkit/CopyIgnoredFile")...),
			wantExit:   1,
			useContext: true,
		},
		{
			name:       "context-copy-heredoc",
			dir:        "context-copy-heredoc",
			args:       append([]string{"--format", "json"}, mustSelectRules("buildkit/CopyIgnoredFile")...),
			useContext: true,
		},
		{
			name:  "discovery-directory",
			dir:   "discovery-directory",
			args:  append([]string{"--format", "json"}, mustSelectRules("tally/max-lines")...),
			isDir: true,
		},
		{
			name: "discovery-exclude",
			dir:  "discovery-exclude",
			args: append(
				[]string{"--format", "json", "--exclude", "test/*", "--exclude", "vendor/*"},
				mustSelectRules("tally/max-lines")...),
			isDir: true,
		},
		{
			name:     "per-file-configs",
			dir:      "per-file-configs",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/max-lines")...),
			isDir:    true,
			wantExit: 1,
		},
	}
}
