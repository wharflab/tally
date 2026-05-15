package integration

import "testing"

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
			// Context-aware refinement for tally/ruby/bootsnap-precompile-without-j1:
			// Gemfile.lock lists bootsnap, so the rule fires.
			name:       "ruby-bootsnap-with-lockfile",
			dir:        "ruby-bootsnap-with-lockfile",
			args:       append([]string{"--format", "json"}, mustSelectRules("tally/ruby/bootsnap-precompile-without-j1")...),
			wantExit:   1,
			useContext: true,
		},
		{
			// Context-aware refinement for tally/ruby/bootsnap-precompile-without-j1:
			// Gemfile.lock does NOT list bootsnap, so the rule must suppress.
			name:       "ruby-bootsnap-without-lockfile",
			dir:        "ruby-bootsnap-without-lockfile",
			args:       append([]string{"--format", "json"}, mustSelectRules("tally/ruby/bootsnap-precompile-without-j1")...),
			useContext: true,
		},
		{
			// Context-aware refinement for tally/ruby/missing-bundle-without-development:
			// Gemfile shows both :development and :test groups, so the fix should
			// recommend `BUNDLE_WITHOUT="development:test"` (not just "development").
			name:       "ruby-bundle-without-dev-and-test",
			dir:        "ruby-bundle-without-dev-and-test",
			args:       append([]string{"--format", "json"}, mustSelectRules("tally/ruby/missing-bundle-without-development")...),
			wantExit:   1,
			useContext: true,
		},
		{
			// Context-aware refinement for tally/ruby/missing-bundle-without-development:
			// Gemfile has no :development group, so the rule must suppress entirely.
			name:       "ruby-bundle-without-no-dev-group",
			dir:        "ruby-bundle-without-no-dev-group",
			args:       append([]string{"--format", "json"}, mustSelectRules("tally/ruby/missing-bundle-without-development")...),
			useContext: true,
		},
		{
			// Context-aware refinement for tally/ruby/asset-precompile-without-dummy-key:
			// Gemfile.lock shows Rails 7.0 (older than 7.1), so the rule should
			// emit FixSuggestion (not FixSafe) and recommend BuildKit secret mounts.
			name:       "ruby-asset-precompile-rails-old",
			dir:        "ruby-asset-precompile-rails-old",
			args:       append([]string{"--format", "json"}, mustSelectRules("tally/ruby/asset-precompile-without-dummy-key")...),
			wantExit:   1,
			useContext: true,
		},
		{
			// Context-aware refinement for tally/ruby/asset-precompile-without-dummy-key:
			// No Rails encrypted credentials file exists, so the rule should
			// demote severity from warning to info.
			name:       "ruby-asset-precompile-no-credentials",
			dir:        "ruby-asset-precompile-no-credentials",
			args:       append([]string{"--format", "json"}, mustSelectRules("tally/ruby/asset-precompile-without-dummy-key")...),
			wantExit:   1,
			useContext: true,
		},
		{
			// Context-aware refinement for tally/ruby/missing-bundle-deployment:
			// no Gemfile.lock observable in the build context — severity should
			// escalate to error.
			name:       "ruby-bundle-deployment-no-lockfile",
			dir:        "ruby-bundle-deployment-no-lockfile",
			args:       append([]string{"--format", "json"}, mustSelectRules("tally/ruby/missing-bundle-deployment")...),
			wantExit:   1,
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
