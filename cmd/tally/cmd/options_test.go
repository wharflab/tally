package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// buildLintCommandForTest builds a Cobra command wired to a fresh
// lintOptions value that the caller can inspect. The command's RunE only
// exercises finalizeLintOptions so tests can assert the parsed state without
// actually running the linter.
//
// Per spf13/cobra#1790, factories are the only reliable way to avoid dirty
// command instances leaking state between subtests.
func buildLintCommandForTest() (*cobra.Command, *lintOptions) {
	opts := &lintOptions{}
	cmd := &cobra.Command{
		Use: "lint",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.flags = cmd.Flags()
			return finalizeLintOptions(cmd.Flags(), opts)
		},
	}
	addLintFlags(cmd.Flags(), opts)
	return cmd, opts
}

// TestKoanfFlagMap_SkipsUnchangedFlags pins the behavior that fixed the
// regression where pflag defaults for --skip-blank-lines (false) were
// overwriting the rule's own default (true) loaded by the rule decoder.
//
// If this test fails, review koanfFlagMap — returning a key for an unchanged
// flag causes posflag to seed the koanf map with the flag's default, which
// suppresses rule-owned defaults that apply later in the decoder.
func TestKoanfFlagMap_SkipsUnchangedFlags(t *testing.T) {
	t.Parallel()

	_, _ = buildLintCommandForTest()
	// Walk the flags and ensure every config-shaped flag is dropped when
	// unchanged.
	fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
	addLintFlags(fs, &lintOptions{})

	// Representative sample: all types (string, bool, int, string w/ shorthand).
	for _, name := range []string{
		"format", "output", "show-source", "fail-level",
		"max-lines", "skip-blank-lines", "skip-comments",
		"warn-unused-directives", "require-reason",
		"slow-checks", "slow-checks-timeout",
		"ai", "ai-timeout", "ai-max-input-bytes", "ai-redact-secrets",
	} {
		f := fs.Lookup(name)
		if f == nil {
			t.Fatalf("flag %q not registered", name)
		}
		if f.Changed {
			t.Fatalf("flag %q should start Changed=false", name)
		}
		key, val := koanfFlagMap(f)
		if key != "" {
			t.Errorf("unchanged flag %q should not route to koanf; got %q=%v", name, key, val)
		}
	}
}

// TestKoanfFlagMap_ChangedFlagsRouteToCanonicalKeys checks the happy path:
// after Cobra parses a flag from argv, koanfFlagMap must return the exact
// koanf key that the decoder expects. Getting a key wrong (e.g.
// "output.format" vs "format") silently drops CLI overrides because koanf
// layers the value under a key the decoder never reads.
func TestKoanfFlagMap_ChangedFlagsRouteToCanonicalKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		flag string
		argv []string
		key  string
		want any
	}{
		{"format", []string{"--format", "json"}, "output.format", "json"},
		{"output", []string{"--output", "stderr"}, "output.path", "stderr"},
		{"show-source", []string{"--show-source=false"}, "output.show-source", false},
		{"fail-level", []string{"--fail-level", "warning"}, "output.fail-level", "warning"},
		{"max-lines", []string{"--max-lines", "25"}, "rules.tally.max-lines.max", 25},
		{"skip-blank-lines", []string{"--skip-blank-lines"}, "rules.tally.max-lines.skip-blank-lines", true},
		{"skip-comments", []string{"--skip-comments"}, "rules.tally.max-lines.skip-comments", true},
		{"warn-unused-directives", []string{"--warn-unused-directives"}, "inline-directives.warn-unused", true},
		{"require-reason", []string{"--require-reason"}, "inline-directives.require-reason", true},
		{"slow-checks", []string{"--slow-checks", "off"}, "slow-checks.mode", "off"},
		{"slow-checks-timeout", []string{"--slow-checks-timeout", "30s"}, "slow-checks.timeout", "30s"},
		{"ai", []string{"--ai"}, "ai.enabled", true},
		{"ai-timeout", []string{"--ai-timeout", "60s"}, "ai.timeout", "60s"},
		{"ai-max-input-bytes", []string{"--ai-max-input-bytes", "1024"}, "ai.max-input-bytes", 1024},
		{"ai-redact-secrets", []string{"--ai-redact-secrets=false"}, "ai.redact-secrets", false},
	}

	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			t.Parallel()

			fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
			addLintFlags(fs, &lintOptions{})
			if err := fs.Parse(tc.argv); err != nil {
				t.Fatalf("parse %v: %v", tc.argv, err)
			}
			f := fs.Lookup(tc.flag)
			if !f.Changed {
				t.Fatalf("flag %q should be Changed after parsing %v", tc.flag, tc.argv)
			}
			gotKey, gotVal := koanfFlagMap(f)
			if gotKey != tc.key {
				t.Errorf("koanfFlagMap(%q) key = %q, want %q", tc.flag, gotKey, tc.key)
			}
			if gotVal != tc.want {
				t.Errorf("koanfFlagMap(%q) val = %#v, want %#v", tc.flag, gotVal, tc.want)
			}
		})
	}
}

// TestKoanfFlagMap_OperationalFlagsAreDropped guards against config-shape
// drift: operational/transform/appending flags must never enter koanf, even
// when the user explicitly sets them. Forgetting one of these would make
// koanf schema validation reject perfectly valid CLI invocations (e.g.
// `--fix-rule tally/max-lines`).
func TestKoanfFlagMap_OperationalFlagsAreDropped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		flag string
		argv []string
	}{
		{"config", []string{"--config", "tally.toml"}},
		{"context", []string{"--context", "/tmp"}},
		{"exclude", []string{"--exclude", "*.bak"}},
		{"select", []string{"--select", "tally/*"}},
		{"ignore", []string{"--ignore", "tally/max-lines"}},
		{"target", []string{"--target", "web"}},
		{"service", []string{"--service", "api"}},
		{"fix", []string{"--fix"}},
		{"fix-rule", []string{"--fix-rule", "tally/max-lines"}},
		{"fix-unsafe", []string{"--fix-unsafe"}},
		{"no-color", []string{"--no-color"}},
		{"hide-source", []string{"--hide-source"}},
		{"no-inline-directives", []string{"--no-inline-directives"}},
		{"acp-command", []string{"--acp-command", "gemini"}},
	}

	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			t.Parallel()

			fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
			addLintFlags(fs, &lintOptions{})
			if err := fs.Parse(tc.argv); err != nil {
				t.Fatalf("parse %v: %v", tc.argv, err)
			}
			f := fs.Lookup(tc.flag)
			if !f.Changed {
				t.Fatalf("precondition: %q should be Changed", tc.flag)
			}
			if key, val := koanfFlagMap(f); key != "" {
				t.Errorf("operational flag %q leaked into koanf as %q=%v", tc.flag, key, val)
			}
		})
	}
}

// TestFinalizeLintOptions_EnvAliasesFillWhenFlagUnset ensures CLI-only env
// aliases (which are intentionally NOT part of the TALLY_* koanf schema)
// still populate lintOptions when the corresponding flag wasn't passed.
// urfave/cli used to handle these via Sources: cli.EnvVars(...); this test
// pins that behavior into the new posflag-based flow.
func TestFinalizeLintOptions_EnvAliasesFillWhenFlagUnset(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TALLY_EXCLUDE", "a.bak,b.bak")
	t.Setenv("TALLY_RULES_SELECT", "tally/*")
	t.Setenv("TALLY_RULES_IGNORE", "shellcheck/*")
	t.Setenv("TALLY_FIX_RULE", "tally/max-lines,tally/newline-per-chained-call")
	t.Setenv("TALLY_CONTEXT", "/workspace")
	t.Setenv("TALLY_FIX", "1")
	t.Setenv("TALLY_FIX_UNSAFE", "yes")
	t.Setenv("TALLY_NO_INLINE_DIRECTIVES", "true")
	t.Setenv("TALLY_ACP_COMMAND", "gemini --model foo")

	cmd, opts := buildLintCommandForTest()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if opts.noColor == nil || !*opts.noColor {
		t.Errorf("NO_COLOR env did not set opts.noColor=true")
	}
	if !slices.Equal(opts.exclude, []string{"a.bak", "b.bak"}) {
		t.Errorf("TALLY_EXCLUDE not applied: got %v", opts.exclude)
	}
	if !slices.Equal(opts.selectR, []string{"tally/*"}) {
		t.Errorf("TALLY_RULES_SELECT not applied: got %v", opts.selectR)
	}
	if !slices.Equal(opts.ignore, []string{"shellcheck/*"}) {
		t.Errorf("TALLY_RULES_IGNORE not applied: got %v", opts.ignore)
	}
	if !slices.Equal(opts.fixRule, []string{"tally/max-lines", "tally/newline-per-chained-call"}) {
		t.Errorf("TALLY_FIX_RULE not applied: got %v", opts.fixRule)
	}
	if opts.contextDir != "/workspace" || !opts.contextSet {
		t.Errorf("TALLY_CONTEXT not applied: contextDir=%q contextSet=%v", opts.contextDir, opts.contextSet)
	}
	if !opts.fix {
		t.Errorf("TALLY_FIX=1 did not set opts.fix=true")
	}
	if !opts.fixUnsafe {
		t.Errorf("TALLY_FIX_UNSAFE=yes did not set opts.fixUnsafe=true")
	}
	if opts.noInlineDirectives == nil || !*opts.noInlineDirectives {
		t.Errorf("TALLY_NO_INLINE_DIRECTIVES=true did not set opts.noInlineDirectives=true")
	}
	if !opts.acpCommandSet || opts.acpCommand != "gemini --model foo" {
		t.Errorf("TALLY_ACP_COMMAND not applied: set=%v value=%q", opts.acpCommandSet, opts.acpCommand)
	}
}

// TestFinalizeLintOptions_FlagBeatsEnv verifies the precedence rule: when a
// user passes a flag AND the corresponding env var is set, the flag wins.
// This was implicit in urfave/cli's Source ordering; the hand-rolled
// finalize step has to replicate it.
func TestFinalizeLintOptions_FlagBeatsEnv(t *testing.T) {
	t.Setenv("TALLY_EXCLUDE", "from-env.bak")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TALLY_FIX", "1")
	t.Setenv("TALLY_CONTEXT", "/from-env")
	t.Setenv("TALLY_NO_INLINE_DIRECTIVES", "true")

	cmd, opts := buildLintCommandForTest()
	cmd.SetArgs([]string{
		"--exclude", "from-flag.bak",
		"--no-color=false",
		"--fix=false",
		"--context", "/from-flag",
		"--no-inline-directives=false",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !slices.Equal(opts.exclude, []string{"from-flag.bak"}) {
		t.Errorf("--exclude flag should win: got %v", opts.exclude)
	}
	if opts.noColor == nil || *opts.noColor {
		t.Errorf("--no-color=false should win over NO_COLOR=1: got %v", opts.noColor)
	}
	if opts.fix {
		t.Errorf("--fix=false should win over TALLY_FIX=1")
	}
	if opts.contextDir != "/from-flag" {
		t.Errorf("--context flag should win: got %q", opts.contextDir)
	}
	if opts.noInlineDirectives == nil || *opts.noInlineDirectives {
		t.Errorf("--no-inline-directives=false should win over env: got %v", opts.noInlineDirectives)
	}
}

// TestFinalizeLintOptions_InvalidFormatIsEagerlyRejected guards the eager
// --format validation we do in finalize. Deferring this to the koanf decoder
// produces an opaque error message; catching it up front lets us return a
// precise "invalid --format %q" instead.
func TestFinalizeLintOptions_InvalidFormatIsEagerlyRejected(t *testing.T) {
	t.Parallel()

	cmd, _ := buildLintCommandForTest()
	cmd.SetArgs([]string{"--format", "not-a-real-format"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --format")
	}
	if msg := err.Error(); msg == "" || !containsAll(msg, "format", "not-a-real-format") {
		t.Errorf("error should mention the bad format value: %q", msg)
	}
}

// TestFinalizeLintOptions_NoColorEnvNonEmptyDisablesColor pins the NO_COLOR
// ecosystem convention: any non-empty value disables color. An empty value
// does NOT disable color. Getting this wrong breaks widely-documented
// expectations for every tally user who relies on NO_COLOR.
func TestFinalizeLintOptions_NoColorEnvConvention(t *testing.T) {
	cases := []struct {
		envValue  string
		envSet    bool
		wantSet   bool // whether opts.noColor should be non-nil
		wantValue bool // expected *opts.noColor when wantSet
	}{
		{envValue: "1", envSet: true, wantSet: true, wantValue: true},
		{envValue: "true", envSet: true, wantSet: true, wantValue: true},
		{envValue: "anything", envSet: true, wantSet: true, wantValue: true},
		{envValue: "", envSet: true, wantSet: false}, // empty NO_COLOR is NOT "disable color"
		{envValue: "", envSet: false, wantSet: false},
	}

	for _, tc := range cases {
		label := "unset"
		if tc.envSet {
			label = "set=" + tc.envValue
		}
		t.Run(label, func(t *testing.T) {
			if tc.envSet {
				t.Setenv("NO_COLOR", tc.envValue)
			} else {
				t.Setenv("NO_COLOR", "")
			}

			cmd, opts := buildLintCommandForTest()
			cmd.SetArgs([]string{})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			switch {
			case !tc.wantSet && opts.noColor != nil:
				t.Errorf("expected noColor unset, got %v", *opts.noColor)
			case tc.wantSet && opts.noColor == nil:
				t.Errorf("expected noColor=%v, got nil", tc.wantValue)
			case tc.wantSet && opts.noColor != nil && tc.wantValue != *opts.noColor:
				t.Errorf("noColor = %v, want %v", *opts.noColor, tc.wantValue)
			}
		})
	}
}

// TestFinalizeLintOptions_BoolEnvParsing pins the accepted truthy/falsy
// values for TALLY_* env bool aliases. Some callers use `yes`/`no` in CI
// configs; some use `1`/`0`. If we accidentally regress to strict
// strconv.ParseBool, those invocations will start erroring out loudly.
func TestFinalizeLintOptions_BoolEnvParsing(t *testing.T) {
	truthy := []string{"1", "t", "true", "yes", "on", "TRUE", "Yes"}
	falsy := []string{"0", "f", "false", "no", "off", "FALSE", "No", ""}

	for _, v := range truthy {
		t.Run("truthy="+v, func(t *testing.T) {
			t.Setenv("TALLY_FIX", v)
			cmd, opts := buildLintCommandForTest()
			cmd.SetArgs([]string{})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !opts.fix {
				t.Errorf("TALLY_FIX=%q should be truthy", v)
			}
		})
	}
	for _, v := range falsy {
		t.Run("falsy="+v, func(t *testing.T) {
			t.Setenv("TALLY_FIX", v)
			cmd, opts := buildLintCommandForTest()
			cmd.SetArgs([]string{})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if opts.fix {
				t.Errorf("TALLY_FIX=%q should be falsy", v)
			}
		})
	}
}

// TestFinalizeLintOptions_SliceEnvTrimsAndDropsEmpty pins the split-and-trim
// behavior for comma-separated env values. urfave/cli's StringSliceFlag
// preserved empty entries from `TALLY_EXCLUDE=",a,,b,"`; our custom splitter
// intentionally drops them, and this test prevents accidental regressions
// in either direction.
func TestFinalizeLintOptions_SliceEnvTrimsAndDropsEmpty(t *testing.T) {
	t.Setenv("TALLY_EXCLUDE", "  a.bak  , ,, b.bak ,")

	cmd, opts := buildLintCommandForTest()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"a.bak", "b.bak"}
	if !slices.Equal(opts.exclude, want) {
		t.Errorf("exclude = %v, want %v", opts.exclude, want)
	}
}

// TestAddLintFlags_CreatesFreshStateEachCall defends against the class of
// issue spf13/cobra#1790 warns about: shared global flag state leaks between
// tests. Our factory (lintCommand) builds a fresh FlagSet and fresh opts
// every call; regressing to a package-global would let one test's --fix
// bleed into the next.
func TestAddLintFlags_CreatesFreshStateEachCall(t *testing.T) {
	t.Parallel()

	cmdA := lintCommand()
	cmdB := lintCommand()
	if cmdA == cmdB {
		t.Fatal("lintCommand() returned the same pointer twice")
	}
	if cmdA.Flags() == cmdB.Flags() {
		t.Fatal("lintCommand() returned shared FlagSet across calls")
	}

	// Parsing --fix on cmdA must not leak into cmdB.
	if err := cmdA.Flags().Parse([]string{"--fix"}); err != nil {
		t.Fatalf("parse A: %v", err)
	}
	if cmdB.Flags().Lookup("fix").Changed {
		t.Fatal("cmdB --fix was marked Changed by cmdA parse")
	}
}

func containsAll(s string, needles ...string) bool {
	for _, n := range needles {
		if !strings.Contains(s, n) {
			return false
		}
	}
	return true
}
