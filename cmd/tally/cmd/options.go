package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/reporter"
)

// lintOptions carries lint flag values that are NOT config-shaped.
//
// Simple config-shaped flags (--format, --max-lines, --ai-timeout, ...) flow
// into the Config via koanf/posflag instead of living here — that keeps
// defaults → config file → env → CLI precedence in one place. This struct
// holds three categories that don't fit that mould:
//
//   - Operational flags that never enter config (e.g. --config, --context,
//     --target, --fix).
//   - Flags with append/transform semantics (e.g. --select / --ignore append
//     to rule selections; --hide-source / --no-inline-directives invert the
//     matching config key).
//   - Ad-hoc env aliases that urfave/cli/v3 used to expose as env sources but
//     which shouldn't enter the TALLY_* koanf schema (e.g. NO_COLOR,
//     TALLY_EXCLUDE, TALLY_FIX_RULE).
type lintOptions struct {
	// flags is the pflag.FlagSet used to resolve config-shaped flag values
	// through koanf/posflag. It's attached here so internal helpers don't
	// need to pass a FlagSet separately alongside opts.
	flags *pflag.FlagSet

	// Standalone config discovery.
	configPath string

	// Append-semantics rule selection.
	selectR []string // --select (append to cfg.Rules.Include)
	ignore  []string // --ignore (append to cfg.Rules.Exclude)

	// Inversions of config values.
	hideSource         bool
	noInlineDirectives *bool
	noColor            *bool

	// Build context / orchestrator entrypoints.
	contextDir string
	contextSet bool
	targets    []string
	services   []string

	// Operational flags.
	exclude   []string
	fix       bool
	fixRule   []string
	fixUnsafe bool

	// Complex (shell-quoted) AI flag: parsed then folded into the config.
	acpCommand    string
	acpCommandSet bool

	// Optional metadata captured when tally is invoked as a Docker CLI plugin.
	dockerPlugin *dockerPluginContext
}

type dockerPluginContext struct {
	CurrentContext string
	ConfigPath     string
	EndpointHost   string
}

// addLintFlags registers all lint flags on the given FlagSet. The flags are
// bound either directly to fields of opts (for operational/transform flags)
// or live inside the pflag.FlagSet itself so the config loader can pick them
// up via posflag.
func addLintFlags(fs *pflag.FlagSet, opts *lintOptions) {
	fs.StringVarP(&opts.configPath, "config", "c", "", "Path to config file (default: auto-discover)")

	// Config-shaped flags. Values are read back by the koanf posflag layer;
	// we don't need Go-side variables for these.
	fs.IntP("max-lines", "l", 0, "Maximum number of lines allowed (0 = unlimited)")
	fs.Bool("skip-blank-lines", false, "Exclude blank lines from the line count")
	fs.Bool("skip-comments", false, "Exclude comment lines from the line count")

	fs.StringP("format", "f", "", "Output format: "+reporter.ValidFormatsUsage())
	fs.StringP("output", "o", "", "Output path: stdout, stderr, or file path")
	fs.Bool("show-source", true, "Show source code snippets (default: true)")
	fs.String("fail-level", "", "Minimum severity to cause non-zero exit: error, warning, info, style, none")

	fs.Bool("warn-unused-directives", false, "Warn about unused ignore directives")
	fs.Bool("require-reason", false, "Warn about ignore directives without reason= explanation")

	fs.String("slow-checks", "", "Slow checks mode: auto, on, off")
	fs.String("slow-checks-timeout", "", "Timeout for slow checks (e.g., 20s)")

	fs.Bool("ai", false, "Enable AI AutoFix (requires an ACP agent command)")
	fs.String("ai-timeout", "", "Per-fix AI timeout (e.g., 90s)")
	fs.Int("ai-max-input-bytes", 0, "Maximum prompt size in bytes")
	fs.Bool("ai-redact-secrets", true, "Redact obvious secrets before sending content to the agent")

	// Transform flags bound to opts.
	fs.BoolVar(&opts.hideSource, "hide-source", false, "Hide source code snippets")
	fs.Bool("no-inline-directives", false, "Disable processing of inline ignore directives")
	fs.Bool("no-color", false, "Disable colored output")

	// Operational flags bound to opts.
	fs.StringSliceVar(&opts.exclude, "exclude", nil, "Glob pattern to exclude files (can be repeated)")
	fs.StringSliceVar(&opts.selectR, "select", nil, "Enable specific rules (pattern: rule-code, namespace/*, *)")
	fs.StringSliceVar(&opts.ignore, "ignore", nil, "Disable specific rules (pattern: rule-code, namespace/*, *)")

	fs.StringVar(&opts.contextDir, "context", "", "Build context directory for context-aware rules")
	fs.StringSliceVar(&opts.targets, "target", nil, "Bake target to lint (can be repeated)")
	fs.StringSliceVar(&opts.services, "service", nil, "Compose service to lint (can be repeated)")

	fs.BoolVar(&opts.fix, "fix", false, "Apply all safe fixes automatically")
	fs.StringSliceVar(&opts.fixRule, "fix-rule", nil, "Only fix specific rules (can be repeated)")
	fs.BoolVar(&opts.fixUnsafe, "fix-unsafe", false, "Also apply suggestion/unsafe fixes (requires --fix)")

	fs.StringVar(&opts.acpCommand, "acp-command", "",
		`ACP agent command line (e.g. "gemini --experimental-acp --allowed-mcp-server-names=none --model=gemini-3-flash-preview")`)
}

// koanfFlagMap routes pflag.Flag -> canonical koanf key.
//
// Returning "" tells posflag to skip the flag. We skip in two cases:
//
//  1. Operational / transform / appending flags that don't belong in koanf.
//  2. Config-shaped flags that the user did NOT explicitly pass on the
//     command line. posflag otherwise merges unchanged pflag defaults into
//     keys that don't yet exist in koanf, which would override rule-level
//     defaults owned by the decoders (e.g. DefaultMaxLinesConfig sets
//     skip-blank-lines=true; the --skip-blank-lines flag default is false).
//
// By skipping unchanged config-shaped flags entirely, we get precedence:
// defaults (struct + rule decoders) -> config file -> TALLY_* env -> CLI flag.
func koanfFlagMap(f *pflag.Flag) (string, any) {
	if !f.Changed {
		return "", nil
	}
	switch f.Name {
	// Output keys.
	case "format":
		return "output.format", posflagStringVal(f)
	case "output":
		return "output.path", posflagStringVal(f)
	case "show-source":
		return "output.show-source", posflagBoolVal(f)
	case "fail-level":
		return "output.fail-level", posflagStringVal(f)

	// tally/max-lines rule option shortcuts.
	case "max-lines":
		return "rules.tally.max-lines.max", posflagIntVal(f)
	case "skip-blank-lines":
		return "rules.tally.max-lines.skip-blank-lines", posflagBoolVal(f)
	case "skip-comments":
		return "rules.tally.max-lines.skip-comments", posflagBoolVal(f)

	// Inline directives.
	case "warn-unused-directives":
		return "inline-directives.warn-unused", posflagBoolVal(f)
	case "require-reason":
		return "inline-directives.require-reason", posflagBoolVal(f)

	// Slow checks.
	case "slow-checks":
		return "slow-checks.mode", posflagStringVal(f)
	case "slow-checks-timeout":
		return "slow-checks.timeout", posflagStringVal(f)

	// AI.
	case "ai":
		return "ai.enabled", posflagBoolVal(f)
	case "ai-timeout":
		return "ai.timeout", posflagStringVal(f)
	case "ai-max-input-bytes":
		return "ai.max-input-bytes", posflagIntVal(f)
	case "ai-redact-secrets":
		return "ai.redact-secrets", posflagBoolVal(f)

	default:
		// Operational / transform / appending flags are not koanf-shaped.
		return "", nil
	}
}

func posflagStringVal(f *pflag.Flag) string {
	return f.Value.String()
}

func posflagBoolVal(f *pflag.Flag) bool {
	b, err := strconv.ParseBool(f.Value.String())
	if err != nil {
		return false
	}
	return b
}

func posflagIntVal(f *pflag.Flag) int {
	n, err := strconv.Atoi(f.Value.String())
	if err != nil {
		return 0
	}
	return n
}

// finalizeLintOptions resolves CLI-only env aliases (NO_COLOR, TALLY_EXCLUDE,
// TALLY_FIX, TALLY_FIX_RULE, TALLY_FIX_UNSAFE, TALLY_CONTEXT, TALLY_RULES_SELECT,
// TALLY_RULES_IGNORE, TALLY_NO_INLINE_DIRECTIVES, TALLY_ACP_COMMAND) into
// lintOptions. These env vars exist for CLI compatibility but are NOT part of
// the koanf schema — config-shaped TALLY_* env vars flow through koanf's env
// provider instead. Flag-provided values always win over env values.
//
// After this call:
//   - opts.configPath was bound by pflag.
//   - opts.selectR/ignore/exclude/fixRule hold the union of flag + env values
//     (flag wins; env used only when the flag wasn't set).
//   - opts.fix / opts.fixUnsafe / opts.hideSource / opts.acpCommand... reflect
//     env fallback when the flag wasn't set.
func finalizeLintOptions(fs *pflag.FlagSet, opts *lintOptions) error {
	if err := validateLintFlagFormat(fs); err != nil {
		return err
	}
	if err := resolveLintInversions(fs, opts); err != nil {
		return err
	}

	// Append-style slice flags: flag takes precedence, otherwise env fills in.
	if !fs.Changed("exclude") {
		if v, ok := lookupSliceEnv("TALLY_EXCLUDE"); ok {
			opts.exclude = v
		}
	}
	if !fs.Changed("select") {
		if v, ok := lookupSliceEnv("TALLY_RULES_SELECT"); ok {
			opts.selectR = v
		}
	}
	if !fs.Changed("ignore") {
		if v, ok := lookupSliceEnv("TALLY_RULES_IGNORE"); ok {
			opts.ignore = v
		}
	}
	if !fs.Changed("fix-rule") {
		if v, ok := lookupSliceEnv("TALLY_FIX_RULE"); ok {
			opts.fixRule = v
		}
	}

	// --context: track whether it was set so orchestrator validation can
	// distinguish "user passed --context" from "user didn't".
	if fs.Changed("context") {
		opts.contextSet = true
	} else if v, ok := os.LookupEnv("TALLY_CONTEXT"); ok {
		opts.contextDir = v
		opts.contextSet = true
	}

	if !fs.Changed("fix") {
		if v, ok, err := parseEnvBool("TALLY_FIX"); err != nil {
			return err
		} else if ok {
			opts.fix = v
		}
	}
	if !fs.Changed("fix-unsafe") {
		if v, ok, err := parseEnvBool("TALLY_FIX_UNSAFE"); err != nil {
			return err
		} else if ok {
			opts.fixUnsafe = v
		}
	}

	// --acp-command: track whether it was set so loadConfigForFile knows
	// whether to parse it and force ai.enabled=true.
	if fs.Changed("acp-command") {
		opts.acpCommandSet = true
	} else if v, ok := os.LookupEnv("TALLY_ACP_COMMAND"); ok {
		opts.acpCommand = v
		opts.acpCommandSet = true
	}

	return nil
}

// validateLintFlagFormat surfaces an invalid --format value before the
// posflag layer turns it into an obscure decode error.
func validateLintFlagFormat(fs *pflag.FlagSet) error {
	if !fs.Changed("format") {
		return nil
	}
	v, err := fs.GetString("format")
	if err != nil {
		return err
	}
	if _, err := reporter.ParseFormat(v); err != nil {
		return fmt.Errorf("invalid --format %q: %w", v, err)
	}
	return nil
}

// resolveLintInversions materializes --no-color and --no-inline-directives
// (plus their env fallbacks) into lintOptions. They invert config values
// rather than feeding them, so they can't live in the posflag layer.
func resolveLintInversions(fs *pflag.FlagSet, opts *lintOptions) error {
	if fs.Changed("no-color") {
		v, err := fs.GetBool("no-color")
		if err != nil {
			return err
		}
		opts.noColor = &v
	} else if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		b := true
		opts.noColor = &b
	}

	if fs.Changed("no-inline-directives") {
		v, err := fs.GetBool("no-inline-directives")
		if err != nil {
			return err
		}
		opts.noInlineDirectives = &v
	} else if v, ok, err := parseEnvBool("TALLY_NO_INLINE_DIRECTIVES"); err != nil {
		return err
	} else if ok {
		opts.noInlineDirectives = &v
	}
	return nil
}

// lintFlagMapper returns the FlagKeyMapper used by config.LoadWithFlags for
// the lint command. It's a thin wrapper so the cmd package can hand a
// FlagKeyMapper to the config package without exporting koanfFlagMap.
func lintFlagMapper() config.FlagKeyMapper {
	return koanfFlagMap
}

func lookupSliceEnv(env string) ([]string, bool) {
	v, ok := os.LookupEnv(env)
	if !ok {
		return nil, false
	}
	if v == "" {
		return nil, true
	}
	parts := strings.Split(v, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out, true
}

func parseEnvBool(env string) (bool, bool, error) {
	v, ok := os.LookupEnv(env)
	if !ok {
		return false, false, nil
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "yes", "on":
		return true, true, nil
	case "0", "f", "false", "no", "off", "":
		return false, true, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, false, fmt.Errorf("invalid boolean for %s=%q: %w", env, v, err)
	}
	return b, true, nil
}
