package integration

import (
	"strings"
	"testing"
)

//nolint:funlen // large table-driven lint case catalog
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
		// Total rules enabled test - validates rule count (no --ignore/--select)
		{name: "total-rules-enabled", dir: "total-rules-enabled", args: []string{"--format", "json", "--slow-checks=off"}},

		// Basic tests (isolated to max-lines rule)
		{name: "simple", dir: "simple", args: append([]string{"--format", "json"}, mustSelectRules("tally/max-lines")...)},
		{
			name: "simple-max-lines-pass",
			dir:  "simple",
			args: append([]string{"--max-lines", "100", "--format", "json"}, mustSelectRules("tally/max-lines")...),
		},
		{
			name:     "simple-max-lines-fail",
			dir:      "simple",
			args:     append([]string{"--max-lines", "2", "--format", "json"}, mustSelectRules("tally/max-lines")...),
			wantExit: 1,
		},

		// Config file discovery tests (isolated to max-lines rule)
		{
			name:     "config-file-discovery",
			dir:      "with-config",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/max-lines")...),
			wantExit: 1,
		},
		{
			name:     "config-cascading-discovery",
			dir:      "nested/subdir",
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

		// Environment variable tests (isolated to max-lines rule)
		{
			name:     "env-var-override",
			dir:      "simple",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/max-lines")...),
			env:      []string{"TALLY_RULES_MAX_LINES_MAX=2"},
			wantExit: 1,
		},

		// BuildKit linter warnings tests (isolated to the rules this fixture triggers)
		{
			name: "buildkit-warnings",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "json"}, mustSelectRules(
				"buildkit/InvalidDefinitionDescription",
				"buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated",
				"buildkit/JSONArgsRecommended",
			)...),
			wantExit: 1,
		},
		{
			name:     "empty-continuation",
			dir:      "empty-continuation",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/NoEmptyContinuation")...),
			wantExit: 1,
		},
		{
			name:     "maintainer-deprecated",
			dir:      "maintainer-deprecated",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/MaintainerDeprecated")...),
			wantExit: 1,
		},
		{
			name:     "consistent-instruction-casing",
			dir:      "consistent-instruction-casing",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/ConsistentInstructionCasing")...),
			wantExit: 1,
		},
		{
			name:     "invalid-definition-description",
			dir:      "invalid-definition-description",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/InvalidDefinitionDescription")...),
			wantExit: 1,
		},
		{
			name:     "legacy-key-value-format",
			dir:      "legacy-key-value-format",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/LegacyKeyValueFormat")...),
			wantExit: 1,
		},

		{
			name:     "multiple-instructions-disallowed",
			dir:      "multiple-instructions-disallowed",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/MultipleInstructionsDisallowed")...),
			wantExit: 1,
		},
		{
			name:     "expose-proto-casing",
			dir:      "expose-proto-casing",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/ExposeProtoCasing")...),
			wantExit: 1,
		},
		{
			name:     "expose-invalid-format",
			dir:      "expose-invalid-format",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/ExposeInvalidFormat")...),
			wantExit: 1,
		},
		// Cross-rule: ExposeInvalidFormat + ExposeProtoCasing overlap on the same EXPOSE line
		{
			name: "expose-cross-rules",
			dir:  "expose-cross-rules",
			args: append([]string{"--format", "json"},
				mustSelectRules("buildkit/ExposeInvalidFormat", "buildkit/ExposeProtoCasing")...),
			wantExit: 1,
		},

		// Reserved stage name test (isolated to ReservedStageName rule)
		{
			name:     "reserved-stage-name",
			dir:      "reserved-stage-name",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/ReservedStageName")...),
			wantExit: 1,
		},
		// Cross-rule: StageNameCasing lowercases "Scratch"→"scratch", ReservedStageName flags it
		{
			name: "reserved-stage-name-casing",
			dir:  "reserved-stage-name-casing",
			args: append([]string{"--format", "json"},
				mustSelectRules("buildkit/ReservedStageName", "buildkit/StageNameCasing")...),
			wantExit: 1,
		},

		// Semantic model construction-time violations
		{
			name: "duplicate-stage-name",
			dir:  "duplicate-stage-name",
			args: append(
				[]string{"--format", "json"},
				mustSelectRules("buildkit/DuplicateStageName", "tally/no-unreachable-stages")...),
			wantExit: 1,
		},
		{
			name:     "multiple-healthcheck",
			dir:      "multiple-healthcheck",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/MultipleInstructionsDisallowed")...),
			wantExit: 1,
		},
		{
			name:     "copy-from-own-alias",
			dir:      "copy-from-own-alias",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3022", "hadolint/DL3023")...),
			wantExit: 1,
		},
		{
			name:     "onbuild-forbidden",
			dir:      "onbuild-forbidden",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3043")...),
			wantExit: 1,
		},
		{
			name:     "invalid-instruction-order",
			dir:      "invalid-instruction-order",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3061")...),
			wantExit: 1,
		},
		{
			name:     "no-from-instruction",
			dir:      "no-from-instruction",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3061")...),
			wantExit: 1,
		},

		// Unreachable stage detection
		{
			name:     "unreachable-stage",
			dir:      "unreachable-stage",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/no-unreachable-stages")...),
			wantExit: 1,
		},

		// Inline directive tests (need specific rules to test against)
		{
			name: "inline-ignore-single",
			dir:  "inline-ignore-single",
			args: append([]string{"--format", "json"}, mustSelectRules("buildkit/StageNameCasing", "hadolint/DL3006")...),
		},
		{
			name: "inline-ignore-global",
			dir:  "inline-ignore-global",
			args: append([]string{"--format", "json"}, mustSelectRules("buildkit/StageNameCasing", "hadolint/DL3006")...),
		},
		{
			name: "inline-hadolint-compat",
			dir:  "inline-hadolint-compat",
			args: append([]string{"--format", "json"}, mustSelectRules("buildkit/StageNameCasing", "hadolint/DL3006")...),
		},
		{
			name: "inline-buildx-compat",
			dir:  "inline-buildx-compat",
			args: append([]string{"--format", "json"}, mustSelectRules("buildkit/StageNameCasing", "hadolint/DL3006")...),
		},

		// Hadolint rule tests (isolated to specific rules)
		{
			name:     "dl3001",
			dir:      "dl3001",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3001")...),
			wantExit: 1,
		},
		{
			name:     "dl3003",
			dir:      "dl3003",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3003")...),
			wantExit: 1,
		},
		{
			name:     "dl3010",
			dir:      "dl3010",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3010")...),
			wantExit: 1,
		},
		{
			name:     "dl3011",
			dir:      "dl3011",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3011")...),
			wantExit: 1,
		},
		// Cross-rule: DL3011 (error) + ExposeProtoCasing (warning) on same EXPOSE line.
		// ExposeProtoCasing warning is suppressed by supersession processor since DL3011 error exists.
		{
			name: "dl3011-cross-rules",
			dir:  "dl3011-cross-rules",
			args: append([]string{"--format", "json"},
				mustSelectRules("hadolint/DL3011", "buildkit/ExposeProtoCasing")...),
			wantExit: 1,
		},
		{
			name:     "dl3021",
			dir:      "dl3021",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3021")...),
			wantExit: 1,
		},
		{
			name:     "dl3022",
			dir:      "dl3022",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3022")...),
			wantExit: 1,
		},
		{
			name:     "dl3027",
			dir:      "dl3027",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3027")...),
			wantExit: 1,
		},
		{
			name:     "dl4005",
			dir:      "dl4005",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL4005")...),
			wantExit: 1,
		},
		{
			name:     "dl4006",
			dir:      "dl4006",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL4006")...),
			wantExit: 1,
		},
		{
			name: "dl4006-cross-rules",
			dir:  "dl4006-cross-rules",
			args: append([]string{"--format", "json"},
				mustSelectRules("hadolint/DL4006", "tally/prefer-run-heredoc")...),
			wantExit: 1,
		},
		{
			name:     "dl3014",
			dir:      "dl3014",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3014")...),
			wantExit: 1,
		},
		{
			name:     "dl3030",
			dir:      "dl3030",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3030")...),
			wantExit: 1,
		},
		{
			name:     "dl3034",
			dir:      "dl3034",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3034")...),
			wantExit: 1,
		},
		{
			name:     "dl3038",
			dir:      "dl3038",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3038")...),
			wantExit: 1,
		},
		{
			name:     "dl3046",
			dir:      "dl3046",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3046")...),
			wantExit: 1,
		},
		{
			name:     "dl3057",
			dir:      "dl3057",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3057")...),
			wantExit: 1,
		},
		{
			name:     "dl3047",
			dir:      "dl3047",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3047")...),
			wantExit: 1,
		},
		// Combined: DL3047 + DL4001 + prefer-add-unpack (all fire on same wget usage)
		{
			name: "dl3047-cross-rules",
			dir:  "dl3047-cross-rules",
			args: append([]string{"--format", "json"},
				mustSelectRules("hadolint/DL3047", "hadolint/DL4001", "tally/prefer-add-unpack")...),
			wantExit: 1,
		},
		{
			name: "inline-ignore-multiple-max-lines",
			dir:  "inline-ignore-multiple",
			args: append([]string{"--format", "json"}, mustSelectRules("tally/max-lines", "hadolint/DL3006")...),
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

		// Output format tests (same fixture as buildkit-warnings)
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

		// Fail-level tests (same fixture as buildkit-warnings)
		{
			name: "fail-level-none",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "json", "--fail-level", "none"}, mustSelectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
		},
		{
			name: "fail-level-error",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "json", "--fail-level", "error"}, mustSelectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
		},
		{
			name: "fail-level-warning",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "json", "--fail-level", "warning"}, mustSelectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
			wantExit: 1,
		},

		// Context-aware rule tests (isolated to CopyIgnoredFile rule)
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
			name: "context-no-context-flag",
			dir:  "context-copy-ignored",
			args: append([]string{"--format", "json"}, mustSelectRules("buildkit/CopyIgnoredFile")...),
		},

		// Discovery tests (isolated to max-lines rule)
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

		// Rule-specific tests (isolated to specific rules)
		{
			name: "trusted-registries-allowed",
			dir:  "trusted-registries-allowed",
			args: append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3026")...),
		},
		{
			name:     "trusted-registries-untrusted",
			dir:      "trusted-registries-untrusted",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3026")...),
			wantExit: 1,
		},
		{
			name:     "avoid-latest-tag",
			dir:      "avoid-latest-tag",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3007")...),
			wantExit: 1,
		},
		{
			name:     "non-posix-shell",
			dir:      "non-posix-shell",
			args:     append([]string{"--format", "json"}, mustSelectRules("hadolint/DL3027")...),
			wantExit: 0, // Should pass - shell rules disabled for PowerShell
		},

		// Prefer heredoc syntax tests (isolated to prefer-run-heredoc rule)
		{
			name:     "prefer-run-heredoc",
			dir:      "prefer-run-heredoc",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/prefer-run-heredoc")...),
			wantExit: 1,
		},

		{
			name:     "prefer-add-unpack",
			dir:      "prefer-add-unpack",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/prefer-add-unpack")...),
			wantExit: 1,
		},

		{
			name:     "prefer-vex-attestation",
			dir:      "prefer-vex-attestation",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/prefer-vex-attestation")...),
			wantExit: 1,
		},

		// Combined: prefer-add-unpack with prefer-run-heredoc (both should fire)
		{
			name: "prefer-add-unpack-heredoc",
			dir:  "prefer-add-unpack-heredoc",
			args: append([]string{"--format", "json"},
				mustSelectRules("tally/prefer-add-unpack", "tally/prefer-run-heredoc")...),
			wantExit: 1,
		},

		// Combined heredoc tests: both prefer-copy-heredoc and prefer-run-heredoc enabled
		{
			name: "heredoc-combined",
			dir:  "heredoc-combined",
			args: append([]string{"--format", "json"},
				mustSelectRules("tally/prefer-copy-heredoc", "tally/prefer-run-heredoc")...),
			wantExit: 1,
		},

		// FROM --platform constant disallowed test (isolated to FromPlatformFlagConstDisallowed rule)
		{
			name:     "from-platform-flag-const-disallowed",
			dir:      "from-platform-flag-const-disallowed",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/FromPlatformFlagConstDisallowed")...),
			wantExit: 1,
		},
		{
			name:     "invalid-default-arg-in-from",
			dir:      "invalid-default-arg-in-from",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/InvalidDefaultArgInFrom")...),
			wantExit: 1,
		},
		{
			name:     "undefined-arg-in-from",
			dir:      "undefined-arg-in-from",
			args:     append([]string{"--format", "json"}, mustSelectRules("buildkit/UndefinedArgInFrom")...),
			wantExit: 1,
		},
		{
			name:     "undefined-var",
			dir:      "undefined-var",
			args:     append([]string{"--format", "json", "--slow-checks=off"}, mustSelectRules("buildkit/UndefinedVar")...),
			wantExit: 1,
		},

		// Slow checks (async) tests — mock registry is set via CONTAINERS_REGISTRIES_CONF
		// at the process level in TestMain.
		{
			name: "slow-checks-platform-mismatch",
			dir:  "slow-checks-platform-mismatch",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("buildkit/InvalidBaseImagePlatform")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-platform-index-mismatch",
			dir:  "slow-checks-platform-index-mismatch",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("buildkit/InvalidBaseImagePlatform")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-platform-meta-arg",
			dir:  "slow-checks-platform-meta-arg",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("buildkit/InvalidBaseImagePlatform")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-platform-target-arg",
			dir:  "slow-checks-platform-target-arg",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("buildkit/InvalidBaseImagePlatform")...),
		},
		{
			name: "slow-checks-undefined-var-enhanced",
			dir:  "slow-checks-undefined-var-enhanced",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("buildkit/UndefinedVar")...),
		},
		{
			name: "slow-checks-undefined-var-still-caught",
			dir:  "slow-checks-undefined-var-still-caught",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("buildkit/UndefinedVar")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-undefined-var-multi-stage",
			dir:  "slow-checks-undefined-var-multi-stage",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("buildkit/UndefinedVar")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-off",
			dir:  "slow-checks-off",
			args: append(
				[]string{"--format", "json", "--slow-checks=off"},
				mustSelectRules("buildkit/InvalidBaseImagePlatform")...),
		},
		{
			name: "slow-checks-fail-fast",
			dir:  "slow-checks-fail-fast",
			args: append(
				[]string{"--format", "json", "--slow-checks=on", "--slow-checks-timeout=2s"},
				mustSelectRules("buildkit/DuplicateStageName", "buildkit/InvalidBaseImagePlatform")...),
			wantExit: 1,
			afterLint: func(t *testing.T, _ string) {
				t.Helper()
				if mockRegistry.HasRequest("library/slowfailfast") {
					t.Error("fail-fast should have prevented async check from fetching the slow image")
				}
			},
		},
		{
			name: "slow-checks-timeout",
			dir:  "slow-checks-timeout",
			args: append(
				[]string{"--format", "json", "--slow-checks=on", "--slow-checks-timeout=1s"},
				mustSelectRules("buildkit/InvalidBaseImagePlatform")...),
			afterLint: func(t *testing.T, stderr string) {
				t.Helper()
				if !strings.Contains(stderr, "timed out") {
					t.Errorf("expected timeout note in stderr, got: %q", stderr)
				}
			},
		},

		// DL3057 async HEALTHCHECK tests — mock registry provides image metadata
		{
			name: "slow-checks-healthcheck-inherited",
			dir:  "slow-checks-healthcheck-inherited",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("hadolint/DL3057")...),
		},
		{
			name: "slow-checks-healthcheck-none-useless",
			dir:  "slow-checks-healthcheck-none-useless",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("hadolint/DL3057")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-healthcheck-missing-confirmed",
			dir:  "slow-checks-healthcheck-missing-confirmed",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				mustSelectRules("hadolint/DL3057")...),
			wantExit: 1,
		},

		// Consistent indentation tests (isolated to consistent-indentation rule)
		{
			name:     "consistent-indentation",
			dir:      "consistent-indentation",
			args:     append([]string{"--format", "json"}, mustSelectRules("tally/consistent-indentation")...),
			wantExit: 1,
		},
	}
}
