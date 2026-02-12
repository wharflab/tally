package integration

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/registry/testutil"
)

var (
	binaryPath   string
	coverageDir  string
	mockRegistry *testutil.MockRegistry
)

func TestMain(m *testing.M) {
	// Build the binary once before running tests
	tmpDir, err := os.MkdirTemp("", "tally-test")
	if err != nil {
		panic(err)
	}

	binaryName := "tally"
	if runtime.GOOS == "windows" {
		binaryName = "tally.exe"
	}
	binaryPath = filepath.Join(tmpDir, binaryName)

	// Collect coverage only when GOCOVERDIR is set (Linux CI).
	// Windows CI doesn't upload coverage, so skip instrumentation to avoid
	// concurrent writes to the coverage directory from parallel subtests.
	buildArgs := []string{"build"}
	coverageDir = os.Getenv("GOCOVERDIR")
	if coverageDir != "" {
		coverageDir, err = filepath.Abs(coverageDir)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			panic("failed to get absolute coverage directory path: " + err.Error())
		}
		if err := os.MkdirAll(coverageDir, 0o750); err != nil {
			_ = os.RemoveAll(tmpDir)
			panic("failed to create coverage directory: " + err.Error())
		}
		buildArgs = append(buildArgs, "-cover")
	}
	buildArgs = append(buildArgs,
		"-tags", "containers_image_openpgp,containers_image_storage_stub,containers_image_docker_daemon_stub",
		"-o", binaryPath, "github.com/tinovyatkin/tally")

	cmd := exec.Command("go", buildArgs...)
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=jsonv2")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		panic("failed to build binary: " + string(out))
	}

	setupMockRegistry(tmpDir)

	code := m.Run()

	mockRegistry.Close()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

// setupMockRegistry starts the mock OCI registry, populates it with test
// images, writes registries.conf, and sets environment variables.
func setupMockRegistry(tmpDir string) {
	mockRegistry = testutil.New()

	// python:3.12 as single-platform linux/arm64 only — used for platform mismatch tests.
	if _, err := mockRegistry.AddImage(testutil.ImageOpts{
		Repo: "library/python",
		Tag:  "3.12",
		OS:   "linux",
		Arch: "arm64",
		Env: map[string]string{
			"PATH":           "/usr/local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"PYTHON_VERSION": "3.12.0",
			"LANG":           "C.UTF-8",
		},
	}); err != nil {
		mockRegistry.Close()
		_ = os.RemoveAll(tmpDir)
		panic("failed to add python:3.12 image: " + err.Error())
	}

	// multiarch:latest — a multi-arch manifest index with linux/amd64 and linux/arm64.
	// Used to test the collectAvailablePlatforms path when a requested platform
	// (e.g., linux/s390x) is not in the index.
	if _, err := mockRegistry.AddIndex("library/multiarch", "latest", []testutil.ImageOpts{
		{Repo: "library/multiarch", Tag: "latest", OS: "linux", Arch: "amd64",
			Env: map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"}},
		{Repo: "library/multiarch", Tag: "latest", OS: "linux", Arch: "arm64",
			Env: map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"}},
	}); err != nil {
		mockRegistry.Close()
		_ = os.RemoveAll(tmpDir)
		panic("failed to add multiarch:latest index: " + err.Error())
	}

	// Delayed images — each has a 30-second artificial delay.
	// Separate repos prevent parallel tests from interfering with each other's
	// request assertions (e.g. fail-fast asserting "no requests for this repo").
	for _, repo := range []string{"library/slowfailfast", "library/slowtimeout"} {
		if _, err := mockRegistry.AddImage(testutil.ImageOpts{
			Repo: repo,
			Tag:  "latest",
			OS:   "linux",
			Arch: "arm64",
			Env:  map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"},
		}); err != nil {
			mockRegistry.Close()
			_ = os.RemoveAll(tmpDir)
			panic("failed to add " + repo + ":latest: " + err.Error())
		}
		mockRegistry.SetDelay(repo, 30*time.Second)
	}

	// Write registries.conf redirecting docker.io to the mock server.
	confPath, err := mockRegistry.WriteRegistriesConf(tmpDir, "docker.io")
	if err != nil {
		mockRegistry.Close()
		_ = os.RemoveAll(tmpDir)
		panic("failed to create registries.conf: " + err.Error())
	}
	if err := os.Setenv("CONTAINERS_REGISTRIES_CONF", confPath); err != nil {
		mockRegistry.Close()
		_ = os.RemoveAll(tmpDir)
		panic("failed to set CONTAINERS_REGISTRIES_CONF: " + err.Error())
	}
	// Set default platform to match the mock registry's image platform (linux/arm64).
	if err := os.Setenv("DOCKER_DEFAULT_PLATFORM", "linux/arm64"); err != nil {
		mockRegistry.Close()
		_ = os.RemoveAll(tmpDir)
		panic("failed to set DOCKER_DEFAULT_PLATFORM: " + err.Error())
	}

	// Clear setup requests (image pushes) so only test-time requests are tracked.
	mockRegistry.ResetRequests()
}

// selectRules returns args to disable all rules except the specified ones.
// This isolates tests from global rule count changes.
func selectRules(rules ...string) []string {
	if len(rules) == 0 {
		panic("selectRules requires at least one rule")
	}
	args := make([]string, 0, 2+2*len(rules))
	args = append(args, "--ignore", "*")
	for _, r := range rules {
		args = append(args, "--select", r)
	}
	return args
}

//nolint:funlen // table-driven test grows with each new rule
func TestCheck(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		dir        string
		args       []string
		env        []string
		wantExit   int
		snapExt    string // Snapshot file extension override (e.g. ".sarif", ".txt", ".md")
		snapRaw    bool   // If true, use MatchStandaloneSnapshot (plain text) instead of MatchStandaloneJSON
		isDir      bool   // If true, pass the directory instead of Dockerfile
		useContext bool   // If true, add --context flag for context-aware tests
		afterCheck func(t *testing.T, stderr string)
	}{
		// Total rules enabled test - validates rule count (no --ignore/--select)
		{name: "total-rules-enabled", dir: "total-rules-enabled", args: []string{"--format", "json", "--slow-checks=off"}},

		// Basic tests (isolated to max-lines rule)
		{name: "simple", dir: "simple", args: append([]string{"--format", "json"}, selectRules("tally/max-lines")...)},
		{
			name: "simple-max-lines-pass",
			dir:  "simple",
			args: append([]string{"--max-lines", "100", "--format", "json"}, selectRules("tally/max-lines")...),
		},
		{
			name:     "simple-max-lines-fail",
			dir:      "simple",
			args:     append([]string{"--max-lines", "2", "--format", "json"}, selectRules("tally/max-lines")...),
			wantExit: 1,
		},

		// Config file discovery tests (isolated to max-lines rule)
		{
			name:     "config-file-discovery",
			dir:      "with-config",
			args:     append([]string{"--format", "json"}, selectRules("tally/max-lines")...),
			wantExit: 1,
		},
		{
			name:     "config-cascading-discovery",
			dir:      "nested/subdir",
			args:     append([]string{"--format", "json"}, selectRules("tally/max-lines")...),
			wantExit: 1,
		},
		{
			name: "config-skip-options",
			dir:  "with-blanks-and-comments",
			args: append([]string{"--format", "json"}, selectRules("tally/max-lines")...),
		},
		{
			name: "cli-overrides-config",
			dir:  "with-config",
			args: append([]string{"--max-lines", "100", "--format", "json"}, selectRules("tally/max-lines")...),
		},

		// Environment variable tests (isolated to max-lines rule)
		{
			name:     "env-var-override",
			dir:      "simple",
			args:     append([]string{"--format", "json"}, selectRules("tally/max-lines")...),
			env:      []string{"TALLY_RULES_MAX_LINES_MAX=2"},
			wantExit: 1,
		},

		// BuildKit linter warnings tests (isolated to the rules this fixture triggers)
		{
			name: "buildkit-warnings",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "json"}, selectRules(
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
			args:     append([]string{"--format", "json"}, selectRules("buildkit/NoEmptyContinuation")...),
			wantExit: 1,
		},
		{
			name:     "maintainer-deprecated",
			dir:      "maintainer-deprecated",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/MaintainerDeprecated")...),
			wantExit: 1,
		},
		{
			name:     "consistent-instruction-casing",
			dir:      "consistent-instruction-casing",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/ConsistentInstructionCasing")...),
			wantExit: 1,
		},
		{
			name:     "invalid-definition-description",
			dir:      "invalid-definition-description",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/InvalidDefinitionDescription")...),
			wantExit: 1,
		},
		{
			name:     "legacy-key-value-format",
			dir:      "legacy-key-value-format",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/LegacyKeyValueFormat")...),
			wantExit: 1,
		},

		{
			name:     "multiple-instructions-disallowed",
			dir:      "multiple-instructions-disallowed",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/MultipleInstructionsDisallowed")...),
			wantExit: 1,
		},
		{
			name:     "expose-proto-casing",
			dir:      "expose-proto-casing",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/ExposeProtoCasing")...),
			wantExit: 1,
		},
		{
			name:     "expose-invalid-format",
			dir:      "expose-invalid-format",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/ExposeInvalidFormat")...),
			wantExit: 1,
		},
		// Cross-rule: ExposeInvalidFormat + ExposeProtoCasing overlap on the same EXPOSE line
		{
			name: "expose-cross-rules",
			dir:  "expose-cross-rules",
			args: append([]string{"--format", "json"},
				selectRules("buildkit/ExposeInvalidFormat", "buildkit/ExposeProtoCasing")...),
			wantExit: 1,
		},

		// Reserved stage name test (isolated to ReservedStageName rule)
		{
			name:     "reserved-stage-name",
			dir:      "reserved-stage-name",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/ReservedStageName")...),
			wantExit: 1,
		},
		// Cross-rule: StageNameCasing lowercases "Scratch"→"scratch", ReservedStageName flags it
		{
			name: "reserved-stage-name-casing",
			dir:  "reserved-stage-name-casing",
			args: append([]string{"--format", "json"},
				selectRules("buildkit/ReservedStageName", "buildkit/StageNameCasing")...),
			wantExit: 1,
		},

		// Semantic model construction-time violations
		{
			name:     "duplicate-stage-name",
			dir:      "duplicate-stage-name",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/DuplicateStageName", "tally/no-unreachable-stages")...),
			wantExit: 1,
		},
		{
			name:     "multiple-healthcheck",
			dir:      "multiple-healthcheck",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/MultipleInstructionsDisallowed")...),
			wantExit: 1,
		},
		{
			name:     "copy-from-own-alias",
			dir:      "copy-from-own-alias",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3022", "hadolint/DL3023")...),
			wantExit: 1,
		},
		{
			name:     "onbuild-forbidden",
			dir:      "onbuild-forbidden",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3043")...),
			wantExit: 1,
		},
		{
			name:     "invalid-instruction-order",
			dir:      "invalid-instruction-order",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3061")...),
			wantExit: 1,
		},
		{
			name:     "no-from-instruction",
			dir:      "no-from-instruction",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3061")...),
			wantExit: 1,
		},

		// Unreachable stage detection
		{
			name:     "unreachable-stage",
			dir:      "unreachable-stage",
			args:     append([]string{"--format", "json"}, selectRules("tally/no-unreachable-stages")...),
			wantExit: 1,
		},

		// Inline directive tests (need specific rules to test against)
		{
			name: "inline-ignore-single",
			dir:  "inline-ignore-single",
			args: append([]string{"--format", "json"}, selectRules("buildkit/StageNameCasing", "hadolint/DL3006")...),
		},
		{
			name: "inline-ignore-global",
			dir:  "inline-ignore-global",
			args: append([]string{"--format", "json"}, selectRules("buildkit/StageNameCasing", "hadolint/DL3006")...),
		},
		{
			name: "inline-hadolint-compat",
			dir:  "inline-hadolint-compat",
			args: append([]string{"--format", "json"}, selectRules("buildkit/StageNameCasing", "hadolint/DL3006")...),
		},
		{
			name: "inline-buildx-compat",
			dir:  "inline-buildx-compat",
			args: append([]string{"--format", "json"}, selectRules("buildkit/StageNameCasing", "hadolint/DL3006")...),
		},

		// Hadolint rule tests (isolated to specific rules)
		{
			name:     "dl3003",
			dir:      "dl3003",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3003")...),
			wantExit: 1,
		},
		{
			name:     "dl3010",
			dir:      "dl3010",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3010")...),
			wantExit: 1,
		},
		{
			name:     "dl3021",
			dir:      "dl3021",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3021")...),
			wantExit: 1,
		},
		{
			name:     "dl3022",
			dir:      "dl3022",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3022")...),
			wantExit: 1,
		},
		{
			name:     "dl3027",
			dir:      "dl3027",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3027")...),
			wantExit: 1,
		},
		{
			name:     "dl4005",
			dir:      "dl4005",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL4005")...),
			wantExit: 1,
		},
		{
			name:     "dl3014",
			dir:      "dl3014",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3014")...),
			wantExit: 1,
		},
		{
			name:     "dl3030",
			dir:      "dl3030",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3030")...),
			wantExit: 1,
		},
		{
			name:     "dl3034",
			dir:      "dl3034",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3034")...),
			wantExit: 1,
		},
		{
			name:     "dl3038",
			dir:      "dl3038",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3038")...),
			wantExit: 1,
		},
		{
			name:     "dl3047",
			dir:      "dl3047",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3047")...),
			wantExit: 1,
		},
		// Combined: DL3047 + DL4001 + prefer-add-unpack (all fire on same wget usage)
		{
			name: "dl3047-cross-rules",
			dir:  "dl3047-cross-rules",
			args: append([]string{"--format", "json"},
				selectRules("hadolint/DL3047", "hadolint/DL4001", "tally/prefer-add-unpack")...),
			wantExit: 1,
		},
		{
			name: "inline-ignore-multiple-max-lines",
			dir:  "inline-ignore-multiple",
			args: append([]string{"--format", "json"}, selectRules("tally/max-lines", "hadolint/DL3006")...),
		},
		{
			name:     "inline-unused-directive",
			dir:      "inline-unused-directive",
			args:     append([]string{"--format", "json", "--warn-unused-directives"}, selectRules("hadolint/DL3006")...),
			wantExit: 1,
		},
		{
			name:     "inline-directives-disabled",
			dir:      "inline-directives-disabled",
			args:     append([]string{"--format", "json", "--no-inline-directives"}, selectRules("buildkit/StageNameCasing")...),
			wantExit: 1,
		},
		{
			name: "inline-require-reason",
			dir:  "inline-require-reason",
			args: append(
				[]string{"--format", "json", "--require-reason"},
				selectRules("buildkit/StageNameCasing", "tally/max-lines")...),
			wantExit: 1,
		},

		// Output format tests (same fixture as buildkit-warnings)
		{
			name: "format-sarif",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "sarif"}, selectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
			wantExit: 1,
			snapExt:  ".sarif",
		},
		{
			name: "format-github-actions",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "github-actions"}, selectRules(
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
			args: append([]string{"--format", "markdown"}, selectRules(
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
			args: append([]string{"--format", "json", "--fail-level", "none"}, selectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
		},
		{
			name: "fail-level-error",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "json", "--fail-level", "error"}, selectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
		},
		{
			name: "fail-level-warning",
			dir:  "buildkit-warnings",
			args: append([]string{"--format", "json", "--fail-level", "warning"}, selectRules(
				"buildkit/InvalidDefinitionDescription", "buildkit/StageNameCasing",
				"buildkit/MaintainerDeprecated", "buildkit/JSONArgsRecommended",
			)...),
			wantExit: 1,
		},

		// Context-aware rule tests (isolated to CopyIgnoredFile rule)
		{
			name:       "context-copy-ignored",
			dir:        "context-copy-ignored",
			args:       append([]string{"--format", "json"}, selectRules("buildkit/CopyIgnoredFile")...),
			wantExit:   1,
			useContext: true,
		},
		{
			name:       "context-copy-heredoc",
			dir:        "context-copy-heredoc",
			args:       append([]string{"--format", "json"}, selectRules("buildkit/CopyIgnoredFile")...),
			useContext: true,
		},
		{
			name: "context-no-context-flag",
			dir:  "context-copy-ignored",
			args: append([]string{"--format", "json"}, selectRules("buildkit/CopyIgnoredFile")...),
		},

		// Discovery tests (isolated to max-lines rule)
		{
			name:  "discovery-directory",
			dir:   "discovery-directory",
			args:  append([]string{"--format", "json"}, selectRules("tally/max-lines")...),
			isDir: true,
		},
		{
			name:  "discovery-exclude",
			dir:   "discovery-exclude",
			args:  append([]string{"--format", "json", "--exclude", "test/*", "--exclude", "vendor/*"}, selectRules("tally/max-lines")...),
			isDir: true,
		},
		{
			name:     "per-file-configs",
			dir:      "per-file-configs",
			args:     append([]string{"--format", "json"}, selectRules("tally/max-lines")...),
			isDir:    true,
			wantExit: 1,
		},

		// Rule-specific tests (isolated to specific rules)
		{
			name: "trusted-registries-allowed",
			dir:  "trusted-registries-allowed",
			args: append([]string{"--format", "json"}, selectRules("hadolint/DL3026")...),
		},
		{
			name:     "trusted-registries-untrusted",
			dir:      "trusted-registries-untrusted",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3026")...),
			wantExit: 1,
		},
		{
			name:     "avoid-latest-tag",
			dir:      "avoid-latest-tag",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3007")...),
			wantExit: 1,
		},
		{
			name:     "non-posix-shell",
			dir:      "non-posix-shell",
			args:     append([]string{"--format", "json"}, selectRules("hadolint/DL3027")...),
			wantExit: 0, // Should pass - shell rules disabled for PowerShell
		},

		// Prefer heredoc syntax tests (isolated to prefer-run-heredoc rule)
		{
			name:     "prefer-run-heredoc",
			dir:      "prefer-run-heredoc",
			args:     append([]string{"--format", "json"}, selectRules("tally/prefer-run-heredoc")...),
			wantExit: 1,
		},

		{
			name:     "prefer-add-unpack",
			dir:      "prefer-add-unpack",
			args:     append([]string{"--format", "json"}, selectRules("tally/prefer-add-unpack")...),
			wantExit: 1,
		},

		{
			name:     "prefer-vex-attestation",
			dir:      "prefer-vex-attestation",
			args:     append([]string{"--format", "json"}, selectRules("tally/prefer-vex-attestation")...),
			wantExit: 1,
		},

		// Combined: prefer-add-unpack with prefer-run-heredoc (both should fire)
		{
			name: "prefer-add-unpack-heredoc",
			dir:  "prefer-add-unpack-heredoc",
			args: append([]string{"--format", "json"},
				selectRules("tally/prefer-add-unpack", "tally/prefer-run-heredoc")...),
			wantExit: 1,
		},

		// Combined heredoc tests: both prefer-copy-heredoc and prefer-run-heredoc enabled
		{
			name: "heredoc-combined",
			dir:  "heredoc-combined",
			args: append([]string{"--format", "json"},
				selectRules("tally/prefer-copy-heredoc", "tally/prefer-run-heredoc")...),
			wantExit: 1,
		},

		// FROM --platform constant disallowed test (isolated to FromPlatformFlagConstDisallowed rule)
		{
			name:     "from-platform-flag-const-disallowed",
			dir:      "from-platform-flag-const-disallowed",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/FromPlatformFlagConstDisallowed")...),
			wantExit: 1,
		},
		{
			name:     "invalid-default-arg-in-from",
			dir:      "invalid-default-arg-in-from",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/InvalidDefaultArgInFrom")...),
			wantExit: 1,
		},
		{
			name:     "undefined-arg-in-from",
			dir:      "undefined-arg-in-from",
			args:     append([]string{"--format", "json"}, selectRules("buildkit/UndefinedArgInFrom")...),
			wantExit: 1,
		},
		{
			name:     "undefined-var",
			dir:      "undefined-var",
			args:     append([]string{"--format", "json", "--slow-checks=off"}, selectRules("buildkit/UndefinedVar")...),
			wantExit: 1,
		},

		// Slow checks (async) tests — mock registry is set via CONTAINERS_REGISTRIES_CONF
		// at the process level in TestMain.
		{
			name: "slow-checks-platform-mismatch",
			dir:  "slow-checks-platform-mismatch",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				selectRules("buildkit/InvalidBaseImagePlatform")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-platform-index-mismatch",
			dir:  "slow-checks-platform-index-mismatch",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				selectRules("buildkit/InvalidBaseImagePlatform")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-platform-meta-arg",
			dir:  "slow-checks-platform-meta-arg",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				selectRules("buildkit/InvalidBaseImagePlatform")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-platform-target-arg",
			dir:  "slow-checks-platform-target-arg",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				selectRules("buildkit/InvalidBaseImagePlatform")...),
		},
		{
			name: "slow-checks-undefined-var-enhanced",
			dir:  "slow-checks-undefined-var-enhanced",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				selectRules("buildkit/UndefinedVar")...),
		},
		{
			name: "slow-checks-undefined-var-still-caught",
			dir:  "slow-checks-undefined-var-still-caught",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				selectRules("buildkit/UndefinedVar")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-undefined-var-multi-stage",
			dir:  "slow-checks-undefined-var-multi-stage",
			args: append(
				[]string{"--format", "json", "--slow-checks=on"},
				selectRules("buildkit/UndefinedVar")...),
			wantExit: 1,
		},
		{
			name: "slow-checks-off",
			dir:  "slow-checks-off",
			args: append(
				[]string{"--format", "json", "--slow-checks=off"},
				selectRules("buildkit/InvalidBaseImagePlatform")...),
		},
		{
			name: "slow-checks-fail-fast",
			dir:  "slow-checks-fail-fast",
			args: append(
				[]string{"--format", "json", "--slow-checks=on", "--slow-checks-timeout=2s"},
				selectRules("buildkit/DuplicateStageName", "buildkit/InvalidBaseImagePlatform")...),
			wantExit: 1,
			afterCheck: func(t *testing.T, _ string) {
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
				selectRules("buildkit/InvalidBaseImagePlatform")...),
			afterCheck: func(t *testing.T, stderr string) {
				t.Helper()
				if !strings.Contains(stderr, "timed out") {
					t.Errorf("expected timeout note in stderr, got: %q", stderr)
				}
			},
		},

		// Consistent indentation tests (isolated to consistent-indentation rule)
		{
			name:     "consistent-indentation",
			dir:      "consistent-indentation",
			args:     append([]string{"--format", "json"}, selectRules("tally/consistent-indentation")...),
			wantExit: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testdataDir := filepath.Join("testdata", tc.dir)

			args := make([]string, 0, 1+len(tc.args)+4)
			args = append(args, "check")
			args = append(args, tc.args...)

			// Add context flag for context-aware tests
			if tc.useContext {
				args = append(args, "--context", testdataDir)
			}

			// Add target (directory or file)
			if tc.isDir {
				args = append(args, testdataDir)
			} else {
				args = append(args, filepath.Join(testdataDir, "Dockerfile"))
			}

			cmd := exec.Command(binaryPath, args...)
			cmd.Env = append(os.Environ(),
				"GOCOVERDIR="+coverageDir,
			)
			// Add test-specific environment variables
			cmd.Env = append(cmd.Env, tc.env...)
			var stdoutBuf, stderrBuf bytes.Buffer
			cmd.Stdout = &stdoutBuf
			cmd.Stderr = &stderrBuf
			err := cmd.Run()
			output := stdoutBuf.Bytes()

			// Check exit code
			exitCode := 0
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("command failed to start: %v", err)
				}
			}
			if exitCode != tc.wantExit {
				t.Errorf("expected exit code %d, got %d\nstdout: %s\nstderr: %s", tc.wantExit, exitCode, output, stderrBuf.String())
			}

			// Normalize output for cross-platform snapshot comparison
			outputStr := string(output)
			// Normalize line endings (Windows CRLF -> LF) for consistent snapshots
			outputStr = strings.ReplaceAll(outputStr, "\r\n", "\n")

			if tc.isDir {
				// Replace absolute paths with relative ones for reproducible snapshots
				wd, err := os.Getwd()
				if err == nil {
					wdSlash := filepath.ToSlash(wd) + "/"
					outputStr = strings.ReplaceAll(outputStr, wdSlash, "")
				}
			}

			if tc.snapRaw {
				// Non-JSON formats (e.g. github-actions .txt, markdown .md)
				snaps.WithConfig(snaps.Ext(tc.snapExt)).MatchStandaloneSnapshot(t, outputStr)
			} else {
				// JSON output — MatchStandaloneJSON validates JSON and
				// defaults to .snap.json; Ext overrides for variants like .sarif.
				opts := []func(*snaps.Config){
					snaps.JSON(snaps.JSONConfig{
						SortKeys: true,
						Indent:   "  ",
					}),
				}
				if tc.snapExt != "" {
					opts = append(opts, snaps.Ext(tc.snapExt))
				}
				snaps.WithConfig(opts...).MatchStandaloneJSON(t, outputStr)
			}

			if tc.afterCheck != nil {
				tc.afterCheck(t, stderrBuf.String())
			}
		})
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(binaryPath, "version")
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\noutput: %s", err, output)
	}

	// Version output contains "dev" in tests
	if len(output) == 0 {
		t.Error("expected version output, got empty")
	}
}

func TestFix(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		input       string // Input Dockerfile content
		args        []string
		wantApplied int    // Expected number of fixes applied
		config      string // Optional config file content (empty string uses default empty config)
	}{
		{
			name:        "stage-name-casing",
			input:       "FROM alpine:3.18 AS Builder\nRUN echo hello\nFROM alpine:3.18\nCOPY --from=Builder /app /app\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "from-as-casing",
			input:       "FROM alpine:3.18 as builder\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "combined-stage-and-as-casing",
			input:       "FROM alpine:3.18 as Builder\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 2, // Both FromAsCasing and StageNameCasing
		},
		// DL3027: apt -> apt-get (regression test for line number consistency)
		{
			name:        "dl3027-apt-to-apt-get",
			input:       "FROM ubuntu:22.04\nRUN apt update && apt install -y curl\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Single violation with multiple edits
		},
		// DL3047: wget -> wget --progress=dot:giga
		{
			name:        "dl3047-wget-progress",
			input:       "FROM ubuntu:22.04\nRUN wget http://example.com/file.tar.gz\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		// DL3047 + DL4001 + prefer-add-unpack: all three cooperating rules fire.
		// prefer-add-unpack (priority 95) applies first and replaces the wget|tar
		// with ADD --unpack, making DL3047 (priority 96) moot on that line.
		// The standalone wget (no tar) only triggers DL3047 → --progress inserted.
		// DL4001 has no fix. --fail-level=none prevents unfixed DL4001 from failing.
		{
			name: "dl3047-cross-rules",
			input: "FROM ubuntu:22.04\n" +
				"RUN wget http://example.com/archive.tar.gz | tar -xz -C /opt\n" +
				"RUN wget http://example.com/config.json -O /etc/app/config.json\n" +
				"RUN curl -fsSL http://example.com/script.sh | sh\n",
			args:        []string{"--fix", "--fix-unsafe", "--fail-level", "none"},
			wantApplied: 2, // prefer-add-unpack on wget|tar + DL3047 on standalone wget
		},
		// DL3003: cd -> WORKDIR (regression test for line number consistency)
		{
			// DL3003 fix is FixSuggestion (not FixSafe) because WORKDIR creates
			// the directory if it doesn't exist, while RUN cd fails.
			// Requires both --fix and --fix-unsafe since FixSuggestion > FixSafe.
			name:        "dl3003-cd-to-workdir",
			input:       "FROM ubuntu:22.04\nRUN cd /app\n",
			args:        []string{"--fix", "--fix-unsafe"},
			wantApplied: 1,
		},
		// DL4005: ln /bin/sh -> SHELL instruction
		{
			// DL4005 fix is FixSuggestion: SHELL affects Docker RUN execution
			// while ln affects the container filesystem — different semantics.
			name:  "dl4005-ln-to-shell",
			input: "FROM ubuntu:22.04\nRUN ln -sf /bin/bash /bin/sh\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				selectRules("hadolint/DL4005")...),
			wantApplied: 1,
		},
		{
			name:  "dl4005-ln-in-chain",
			input: "FROM ubuntu:22.04\nRUN apt-get update && ln -sf /bin/bash /bin/sh && echo done\n",
			args: append(
				[]string{"--fix", "--fix-unsafe"},
				selectRules("hadolint/DL4005")...),
			wantApplied: 1,
		},
		// NoEmptyContinuation: Remove empty lines in continuations
		{
			name:        "no-empty-continuation-single",
			input:       "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add curl\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "no-empty-continuation-multiple",
			input:       "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add \\\n\n    curl\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Single violation covers all empty lines
		},
		// ConsistentInstructionCasing: Normalize instruction casing
		{
			name:        "consistent-instruction-casing-to-upper",
			input:       "FROM alpine:3.18\nrun echo hello\nCOPY . /app\nworkdir /app\n",
			args:        []string{"--fix"},
			wantApplied: 2, // Two instructions need fixing
		},
		{
			name:        "consistent-instruction-casing-to-lower",
			input:       "from alpine:3.18\nrun echo hello\nCOPY . /app\nworkdir /app\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Only COPY needs fixing
		},
		// Multiple fixes with line shift: DL3003 splits one line into two,
		// then DL3027 fix on a later line must still apply correctly.
		// The fixer applies edits from end to start to handle position drift.
		{
			name: "multi-fix-line-shift",
			input: `FROM ubuntu:22.04
RUN cd /app && make build
RUN apt install curl
`,
			args:        []string{"--fix", "--fix-unsafe", "--ignore", "tally/prefer-run-heredoc"},
			wantApplied: 2, // DL3003 + DL3027
		},
		// LegacyKeyValueFormat: Replace legacy "ENV key value" with "ENV key=value"
		{
			name:        "legacy-key-value-format-simple",
			input:       "FROM alpine:3.18\nENV foo bar\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "legacy-key-value-format-multi-word",
			input:       "FROM alpine:3.18\nENV MY_VAR hello world\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "legacy-key-value-format-label",
			input:       "FROM alpine:3.18\nLABEL maintainer John Doe\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "legacy-key-value-format-multiple",
			input:       "FROM alpine:3.18\nENV foo bar\nLABEL version 1.0\n",
			args:        []string{"--fix"},
			wantApplied: 2,
		},
		// ExposeProtoCasing: Lowercase protocol in EXPOSE
		{
			name:        "expose-proto-casing-single",
			input:       "FROM alpine:3.18\nEXPOSE 8080/TCP\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "expose-proto-casing-multiple-ports",
			input:       "FROM alpine:3.18\nEXPOSE 80/TCP 443/UDP\n",
			args:        []string{"--fix"},
			wantApplied: 2, // One violation per non-lowercase port, matching BuildKit behavior
		},
		// ExposeProtoCasing + ConsistentInstructionCasing overlap: both rules edit the same EXPOSE line
		{
			name:  "expose-proto-casing-with-instruction-casing",
			input: "FROM alpine:3.18\nexpose 8080/TCP\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/ExposeProtoCasing",
				"--select", "buildkit/ConsistentInstructionCasing",
			},
			wantApplied: 2, // instruction casing + protocol casing
		},
		// MaintainerDeprecated: Replace MAINTAINER with LABEL
		{
			name:        "maintainer-deprecated",
			input:       "FROM alpine:3.18\nMAINTAINER John Doe <john@example.com>\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "json-args-recommended",
			input:       "FROM alpine:3.18\nCMD echo hello\n",
			args:        []string{"--fix", "--fix-unsafe"},
			wantApplied: 1,
		},
		// InvalidDefinitionDescription: Add empty line between non-description comment and instruction
		// Multiple violations to verify fixes apply correctly with line shifts
		{
			name: "invalid-definition-description-multiple",
			input: `# check=experimental=InvalidDefinitionDescription
# bar this is the bar
ARG foo=bar
# BasE this is the BasE image
FROM scratch AS base
# definitely a bad comment
ARG version=latest
# definitely a bad comment
ARG baz=quux
`,
			args:        []string{"--fix", "--select", "buildkit/InvalidDefinitionDescription"},
			wantApplied: 4, // Four violations: lines 3, 5, 7, 9
		},
		// Consistent indentation: add indentation to multi-stage commands
		{
			name:        "consistent-indentation-multi-stage",
			input:       "FROM alpine:3.20 AS builder\nRUN echo build\nFROM scratch\nCOPY --from=builder /app /app\n",
			args:        []string{"--fix", "--select", "tally/consistent-indentation"},
			wantApplied: 2,
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// Consistent indentation: remove indentation from single-stage
		{
			name:        "consistent-indentation-single-stage",
			input:       "FROM alpine:3.20\n\tRUN echo hello\n\tCOPY . /app\n",
			args:        []string{"--fix", "--select", "tally/consistent-indentation"},
			wantApplied: 2,
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// Consistent indentation: remove space indentation from single-stage
		{
			name:        "consistent-indentation-single-stage-spaces",
			input:       "FROM alpine:3.20\n    RUN echo hello\n    COPY . /app\n",
			args:        []string{"--fix", "--select", "tally/consistent-indentation"},
			wantApplied: 2,
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// Consistent indentation: multi-line continuation lines get aligned to 1 tab
		{
			name: "consistent-indentation-multi-line-continuation",
			input: "FROM ubuntu:22.04 AS builder\n" +
				"ARG LAMBDA_TASK_ROOT=/var/task\n" +
				"RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf \\\n" +
				"         --mount=type=cache,target=/root/.cache/pip \\\n" +
				"--mount=type=secret,id=uvtoml,target=/root/.config/uv/uv.toml \\\n" +
				"--mount=type=bind,source=requirements.txt,target=${LAMBDA_TASK_ROOT}/requirements.txt \\\n" +
				"     --mount=type=cache,target=/root/.cache/uv \\\n" +
				"  pip install uv==0.9.24 && \\\n" +
				"      uv pip install --system -r requirements.txt\n" +
				"FROM scratch\n" +
				"COPY --from=builder /app /app\n",
			args:        []string{"--fix", "--select", "tally/consistent-indentation"},
			wantApplied: 2, // RUN (multi-line) + COPY
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// Consistent indentation + ConsistentInstructionCasing: both fix the same line
		// Indentation adds a tab, casing fixes "run" -> "RUN" on the same line
		{
			name:  "consistent-indentation-with-casing-fix",
			input: "FROM alpine:3.20 AS builder\nrun echo build\nFROM scratch\ncopy --from=builder /app /app\n",
			args: []string{
				"--fix",
				"--select", "tally/consistent-indentation",
				"--select", "buildkit/ConsistentInstructionCasing",
			},
			wantApplied: 3, // 2 indentation + 1 casing (2 commands fixed)
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},
		// InvalidDefinitionDescription enabled via config file instead of Dockerfile directive
		// Verifies that experimental rules can be enabled by setting severity in tally.toml
		{
			name: "invalid-definition-description-via-config",
			input: `# This comment doesn't match the ARG name
ARG foo=bar
# Another mismatched comment
FROM scratch AS base
`,
			args:        []string{"--fix"},
			wantApplied: 2,
			config: `[rules.buildkit.InvalidDefinitionDescription]
severity = "error"
`,
		},

		// MultipleInstructionsDisallowed: Comment out duplicate CMD/ENTRYPOINT
		{
			name:  "multiple-cmd-fix",
			input: "FROM alpine:3.21\nCMD echo \"first\"\nRUN echo hello\nCMD echo \"second\"\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/MultipleInstructionsDisallowed",
			},
			wantApplied: 1,
		},
		{
			name:  "multiple-entrypoint-fix",
			input: "FROM alpine:3.21\nENTRYPOINT [\"/bin/bash\"]\nENTRYPOINT [\"/bin/sh\"]\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/MultipleInstructionsDisallowed",
			},
			wantApplied: 1,
		},
		{
			name:  "multiple-cmd-three",
			input: "FROM alpine:3.21\nCMD echo first\nCMD echo second\nCMD echo third\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/MultipleInstructionsDisallowed",
			},
			wantApplied: 2,
		},
		{
			name:  "multiple-healthcheck-fix",
			input: "FROM alpine:3.21\nHEALTHCHECK CMD curl -f http://localhost/\nHEALTHCHECK --interval=60s CMD wget -qO- http://localhost/\n",
			args: []string{
				"--fix",
				"--ignore", "*",
				"--select", "buildkit/MultipleInstructionsDisallowed",
			},
			wantApplied: 1,
		},
		// Cross-rule interaction: MultipleInstructionsDisallowed + ConsistentInstructionCasing + JSONArgsRecommended
		// all fire on the same duplicate CMD line. MultipleInstructionsDisallowed has priority -1 (applied
		// before cosmetic fixes at priority 0), so it comments out the earlier cmd on line 2 first.
		// Casing and JSON fixes on line 2 are then skipped (conflict with the whole-line edit).
		// Remaining non-conflicting fixes still apply on other lines.
		//   Line 2: commented out by MultipleInstructionsDisallowed (priority -1)
		//   Line 3: JSON fix (echo second→["echo","second"])
		//   Line 4: casing fix (entrypoint→ENTRYPOINT)
		//   Skipped: ConsistentInstructionCasing + JSONArgsRecommended on line 2 (conflict)
		{
			name: "multiple-instructions-cross-rules",
			input: "FROM alpine:3.21\n" +
				"cmd echo first\n" +
				"CMD echo second\n" +
				"entrypoint [\"/bin/sh\"]\n",
			args: append([]string{"--fix", "--fix-unsafe", "--fail-level", "none"},
				selectRules(
					"buildkit/MultipleInstructionsDisallowed",
					"buildkit/ConsistentInstructionCasing",
					"buildkit/JSONArgsRecommended",
				)...),
			wantApplied: 3, // comment-out line 2 + JSON line 3 + casing line 4
		},

		// === Heredoc fix tests ===

		// prefer-copy-heredoc: single RUN echo redirect → COPY heredoc
		{
			name:  "prefer-copy-heredoc-single-echo",
			input: "FROM ubuntu:22.04\nRUN echo 'hello world' > /app/greeting.txt\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-copy-heredoc: consecutive RUNs writing to same file → single COPY heredoc
		{
			name: "prefer-copy-heredoc-consecutive-writes",
			input: "FROM ubuntu:22.04\n" +
				"RUN echo 'line1' > /app/data.txt\n" +
				"RUN echo 'line2' >> /app/data.txt\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-run-heredoc: 3 consecutive RUNs → heredoc RUN
		{
			name: "prefer-run-heredoc-consecutive",
			input: "FROM ubuntu:22.04\n" +
				"RUN apt-get update\n" +
				"RUN apt-get install -y curl\n" +
				"RUN apt-get install -y git\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 1,
		},

		// prefer-run-heredoc: chained commands → heredoc RUN
		{
			name:  "prefer-run-heredoc-chained",
			input: "FROM ubuntu:22.04\nRUN echo step1 && echo step2 && echo step3\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 1,
		},

		// Both heredoc rules enabled together: prefer-copy-heredoc takes priority (99) over prefer-run-heredoc (100).
		// The file-creation RUN is handled by prefer-copy-heredoc; the consecutive RUNs by prefer-run-heredoc.
		{
			name: "heredoc-both-rules-combined",
			input: "FROM ubuntu:22.04\n" +
				"RUN echo 'server {}' > /etc/nginx.conf\n" +
				"RUN apt-get update\n" +
				"RUN apt-get install -y curl\n" +
				"RUN apt-get install -y git\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 2,
		},

		// Heredoc + indentation: multi-stage with both heredoc rules + indentation.
		// The indentation fix (priority 50) applies first, then run-heredoc (100).
		// After indentation adds tabs, the heredoc resolver should preserve them.
		{
			name: "heredoc-with-indentation-multi-stage",
			input: "FROM ubuntu:22.04 AS builder\n" +
				"RUN apt-get update\n" +
				"RUN apt-get install -y curl\n" +
				"RUN apt-get install -y git\n" +
				"FROM alpine:3.20\n" +
				"COPY --from=builder /usr/bin/curl /usr/bin/curl\n" +
				"RUN echo 'done'\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--ignore", "*",
				"--select", "tally/consistent-indentation",
				"--select", "tally/prefer-run-heredoc",
			},
			wantApplied: 6, // 5 indentation fixes + 1 heredoc
			config: `[rules.tally.consistent-indentation]
severity = "style"
`,
		},

		// prefer-copy-heredoc: echo with chmod → COPY --chmod heredoc
		{
			name:  "prefer-copy-heredoc-with-chmod",
			input: "FROM ubuntu:22.04\nRUN echo '#!/bin/sh' > /entrypoint.sh && chmod +x /entrypoint.sh\n",
			args: []string{
				"--fix-unsafe",
				"--fix",
				"--select", "tally/prefer-copy-heredoc",
			},
			wantApplied: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create a temporary directory with a Dockerfile
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath, []byte(tc.input), 0o644); err != nil {
				t.Fatalf("failed to write Dockerfile: %v", err)
			}

			// Create config file (custom content or empty to prevent discovery of repo configs)
			configPath := filepath.Join(tmpDir, ".tally.toml")
			if err := os.WriteFile(configPath, []byte(tc.config), 0o644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			// Run tally check --fix (disable slow checks — fix tests don't need async)
			args := append([]string{"check", "--config", configPath, "--slow-checks=off"}, tc.args...)
			args = append(args, dockerfilePath)
			cmd := exec.Command(binaryPath, args...)
			cmd.Env = append(os.Environ(),
				"GOCOVERDIR="+coverageDir,
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("check --fix failed: %v\noutput:\n%s", err, output)
			}

			// Read the fixed Dockerfile and snapshot it
			fixed, err := os.ReadFile(dockerfilePath)
			if err != nil {
				t.Fatalf("failed to read fixed Dockerfile: %v", err)
			}

			snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixed))

			// Check that the output mentions the expected number of fixes
			outputStr := string(output)
			if tc.wantApplied > 0 && !strings.Contains(outputStr, "Fixed") {
				t.Errorf("expected 'Fixed' in output, got: %s", outputStr)
			}
		})
	}
}

// TestFixRealWorld tests the auto-fix functionality on a real-world Dockerfile
// from a public repository, verifying that multiple fixes apply correctly.
// Source: https://github.com/tle211212/deepspeed_distributed_sagemaker_sample
func TestFixRealWorld(t *testing.T) {
	t.Parallel()
	testdataDir := filepath.Join("testdata", "benchmark-real-world-fix")

	// Read the original Dockerfile
	originalContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read original Dockerfile: %v", err)
	}

	// Create a temp directory and copy the Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, originalContent, 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Copy the config file to disable max-lines
	configContent, err := os.ReadFile(filepath.Join(testdataDir, ".tally.toml"))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Run tally check --fix --fix-unsafe (all rules enabled, slow checks off)
	args := []string{"check", "--config", configPath, "--slow-checks=off", "--fix", "--fix-unsafe", dockerfilePath}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	// Exit code 1 is expected due to remaining unfixable violations
	// Just verify the command ran (output captured regardless of exit code)
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("command failed to run: %v", err)
		}
		// Exit code 1 is expected, other exit codes indicate real failures
		if exitErr.ExitCode() != 1 {
			t.Fatalf("unexpected exit code %d: %v\noutput:\n%s", exitErr.ExitCode(), err, output)
		}
	}

	// Read the fixed Dockerfile
	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	// Use snapshot testing for easier maintenance
	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

	// Verify that fixes were applied (check output contains "Fixed")
	outputStr := string(output)
	if !strings.Contains(outputStr, "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", outputStr)
	}
}

// TestFixHeredocCombined tests auto-fix with both prefer-copy-heredoc and prefer-run-heredoc
// enabled together on a multi-stage Dockerfile that also has consistent-indentation enabled.
// The snapshot makes it easy to review the final fixed Dockerfile.
func TestFixHeredocCombined(t *testing.T) {
	t.Parallel()
	testdataDir := filepath.Join("testdata", "heredoc-combined")

	// Read the original Dockerfile
	originalContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read original Dockerfile: %v", err)
	}

	// Create a temp directory and copy the Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, originalContent, 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Config: enable consistent-indentation (off by default)
	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `[rules.tally.consistent-indentation]
severity = "style"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Run with all three rules: consistent-indentation (50), prefer-copy-heredoc (99), prefer-run-heredoc (100)
	args := []string{
		"check", "--config", configPath, "--slow-checks=off",
		"--fix", "--fix-unsafe",
		"--ignore", "*",
		"--select", "tally/consistent-indentation",
		"--select", "tally/prefer-copy-heredoc",
		"--select", "tally/prefer-run-heredoc",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("command failed to run: %v", err)
		}
		if exitErr.ExitCode() != 1 {
			t.Fatalf("unexpected exit code %d: %v\noutput:\n%s", exitErr.ExitCode(), err, output)
		}
	}

	// Read the fixed Dockerfile and snapshot it
	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

	// Verify fixes were applied
	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}

// TestFixConsistentIndentation tests auto-fix for the consistent-indentation rule
// on a multi-stage Dockerfile with multi-line continuation instructions.
// The snapshot makes it easy to verify all continuation lines get aligned.
func TestFixConsistentIndentation(t *testing.T) {
	t.Parallel()
	testdataDir := filepath.Join("testdata", "consistent-indentation")

	originalContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read original Dockerfile: %v", err)
	}

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, originalContent, 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Copy the config that enables consistent-indentation
	configContent, err := os.ReadFile(filepath.Join(testdataDir, ".tally.toml"))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"check", "--config", configPath, "--slow-checks=off",
		"--fix",
		"--select", "tally/consistent-indentation",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("command failed to run: %v", err)
		}
		if exitErr.ExitCode() != 1 {
			t.Fatalf("unexpected exit code %d: %v\noutput:\n%s", exitErr.ExitCode(), err, output)
		}
	}

	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}

// TestFixPreferAddUnpackBeatsHeredoc verifies that prefer-add-unpack (sync fix, priority 95)
// takes priority over prefer-run-heredoc (async fix, priority 100) when both rules target the
// same consecutive RUN instructions. After fixing, all RUNs become ADD --unpack.
// The prefer-run-heredoc violation still reports (exit code 1) since its fix was superseded.
func TestFixPreferAddUnpackBeatsHeredoc(t *testing.T) {
	t.Parallel()

	input := "FROM ubuntu:22.04\n" +
		"RUN curl -fsSL https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | tar -xz -C /usr/local\n" +
		"RUN curl -fsSL https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz | tar -xJ -C /usr/local\n" +
		"RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt\n"

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(input), 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}
	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"check", "--config", configPath, "--slow-checks=off",
		"--fix-unsafe", "--fix",
		"--select", "tally/prefer-add-unpack",
		"--select", "tally/prefer-run-heredoc",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	// Exit code 1 expected: prefer-run-heredoc violation remains (fix superseded)
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("command failed to run: %v", err)
		}
		if exitErr.ExitCode() != 1 {
			t.Fatalf("unexpected exit code %d: %v\noutput:\n%s", exitErr.ExitCode(), err, output)
		}
	}

	fixed, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixed))

	outputStr := string(output)
	if !strings.Contains(outputStr, "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", outputStr)
	}
}
