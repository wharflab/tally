# 25. CLI-Config Integration Refactor

**Status:** Proposed
**Scope:** `cmd/tally/cmd/lint.go`, `internal/config/`
**Goal:** Replace hand-written CLI-to-config glue code with idiomatic urfave/cli v3 patterns

---

## Problem

`cmd/tally/cmd/lint.go` contains ~100 lines of repetitive `cmd.IsSet` / `cmd.String` / assign-to-config
glue that manually merges CLI flags into the koanf-loaded `Config` struct:

```go
if cmd.IsSet("slow-checks") {
    cfg.SlowChecks.Mode = cmd.String("slow-checks")
}
if cmd.IsSet("slow-checks-timeout") {
    cfg.SlowChecks.Timeout = cmd.String("slow-checks-timeout")
}
if cmd.IsSet("ai") {
    cfg.AI.Enabled = cmd.Bool("ai")
}
// ... repeated ~30 times
```

This is error-prone, hard to keep in sync, and re-implements what the CLI framework and config
library are designed to handle. Every new flag requires touching three places:

1. Flag definition (with name, env var, usage)
2. Manual `IsSet` + read in `resolveConfig()`
3. Config struct field (with koanf tag)

The flag names, env var names, and koanf paths are maintained independently and can drift.

---

## Current Architecture

```text
CLI flags ──► cmd.IsSet / cmd.String ──► manual assignment ──► Config struct
Env vars  ──► cli.EnvVars()          ──► (separate path)   ──►     ↑
TOML file ──► koanf TOML provider    ──► koanf.Unmarshal   ──►     ↑
Defaults  ──► koanf structs provider ──► koanf.Unmarshal   ──►     ↑
```

The problem: CLI flags bypass koanf entirely and are merged via custom imperative code.

---

## Proposed Architecture

### Option A: urfave/cli-altsrc (Recommended)

[urfave/cli-altsrc](https://github.com/urfave/cli-altsrc) is the official companion library for
reading flag values from config files (JSON, YAML, TOML). It plugs into urfave/cli v3's `Sources`
field — the same mechanism already used for `cli.EnvVars()`.

```go
// Before: three separate systems
&cli.StringFlag{
    Name:    "slow-checks",
    Sources: cli.EnvVars("TALLY_SLOW_CHECKS"),  // env only
}
// + manual: if cmd.IsSet("slow-checks") { cfg.SlowChecks.Mode = cmd.String("slow-checks") }

// After: unified source chain
&cli.StringFlag{
    Name:    "slow-checks",
    Sources: cli.NewValueSourceChain(
        cli.EnvVars("TALLY_SLOW_CHECKS"),
        altsrc.TOML("slow-checks.mode", configFilePath),
    ),
    Destination: &cfg.SlowChecks.Mode,
}
```

With this approach:

- The framework handles precedence: CLI flag > env var > TOML file > default
- `Destination` binds directly to the config struct field
- `Validator` runs regardless of source
- No `cmd.IsSet` / `cmd.String` glue needed
- Adding a new flag = one declaration, zero glue

**Trade-off:** `cli-altsrc` needs the config file path before flag parsing, which requires a
two-pass approach or a `Before` hook. Our current auto-discovery (walk up from target file) would
need to run in the command's `Before` callback.

### Option B: urfave/sflags (Struct-Driven Flags)

[urfave/sflags](https://github.com/urfave/sflags) generates CLI flags from struct field tags.
The `Config` struct already has `koanf` tags — sflags could read those (or its own `flag` tags)
to auto-generate flag definitions.

```go
type SlowChecksConfig struct {
    Mode    string `koanf:"mode"    flag:"slow-checks"       desc:"Slow checks mode: auto, on, off"`
    Timeout string `koanf:"timeout" flag:"slow-checks-timeout" desc:"Timeout for slow checks"`
}
```

**Trade-off:** sflags is less actively maintained and doesn't support urfave/cli v3 natively yet.
Better as a future consideration.

### Option C: Koanf CLI Provider (Minimal Change)

Keep koanf as the single config backend and add CLI flags as another koanf provider, running
after TOML and env providers. This preserves the existing config loading and avoids adding
cli-altsrc as a dependency.

```go
// Load in priority order (last wins)
k.Load(structs.Provider(defaultConfig, "koanf"), nil)   // defaults
k.Load(file.Provider(configPath), toml.Parser())         // TOML
k.Load(env.Provider("TALLY_", "_", normalize), nil)      // env vars
k.Load(cliProvider(cmd), nil)                            // CLI flags (only IsSet)
k.Unmarshal("", &cfg)
```

A thin `cliProvider` adapter would iterate the command's flags and emit only explicitly-set values
into koanf's flat key space. This eliminates the manual glue while keeping koanf as the single
merge point.

**Trade-off:** Requires writing a small adapter, but it's a one-time ~50 line implementation that
replaces ~100 lines of per-flag glue that grows with every new flag.

---

## Validator Pattern

Regardless of which option is chosen, enum-style flags should use the `Validator` field on
`FlagBase` for framework-level validation:

```go
&cli.StringFlag{
    Name:      "format",
    Usage:     "Output format: " + reporter.ValidFormatsUsage(),
    Validator: func(s string) error {
        _, err := reporter.ParseFormat(s)
        return err
    },
}

&cli.StringFlag{
    Name:      "slow-checks",
    Usage:     "Slow checks mode: auto, on, off",
    Validator: func(s string) error {
        // validate against known modes
    },
}

&cli.StringFlag{
    Name:      "fail-level",
    Usage:     "Minimum severity: error, warning, info, style, none",
    Validator: func(s string) error {
        // validate against known severity levels
    },
}
```

This ensures:

- Invalid values are rejected before the command runs
- Error messages are formatted by the framework (consistent UX)
- Help text and validation derive from the same source of truth
- Shell completion can leverage the same valid values list

---

## Migration Plan

### Phase 1: Add Validators to Enum Flags (Small, Safe)

Add `Validator` to flags that accept a fixed set of values: `--format`, `--slow-checks`,
`--fail-level`. No structural change, immediate UX improvement.

**Already done for `--format`.**

### Phase 2: Implement Koanf CLI Provider (Option C)

Write a `koanf`-compatible provider that reads explicitly-set CLI flags and emits them into koanf's
key space. Replace the `resolveConfig()` glue with a single `k.Load(cliProvider(cmd), nil)` call.

This is the lowest-risk path: no new dependencies, preserves koanf as the single merge engine,
and the adapter is small and testable.

### Phase 3: Evaluate cli-altsrc (Option A)

Once Phase 2 stabilizes, evaluate whether `cli-altsrc` TOML support could replace the custom
koanf TOML loading + config discovery. This would unify the entire config stack under urfave/cli's
`Sources` chain, but requires solving config file auto-discovery in a `Before` hook.

---

## Current Flags Inventory

| Flag | Type | Env Var | Config Path | Validator Needed? |
|------|------|---------|-------------|-------------------|
| `--format` | string | `TALLY_FORMAT` | `output.format` | Yes (done) |
| `--fail-level` | string | `TALLY_OUTPUT_FAIL_LEVEL` | `output.fail-level` | Yes |
| `--slow-checks` | string | `TALLY_SLOW_CHECKS` | `slow-checks.mode` | Yes |
| `--output` | string | `TALLY_OUTPUT_PATH` | `output.path` | No (file path) |
| `--config` | string | — | — | No (file path) |
| `--max-lines` | int | `TALLY_RULES_MAX_LINES_MAX` | `rules.max-lines.max` | No |
| `--fix` | bool | `TALLY_FIX` | — | No |
| `--fix-unsafe` | bool | `TALLY_FIX_UNSAFE` | — | No |
| `--ai` | bool | `TALLY_AI_ENABLED` | `ai.enabled` | No |
| `--select` | []string | `TALLY_RULES_SELECT` | `rules.include` | No |
| `--ignore` | []string | `TALLY_RULES_IGNORE` | `rules.exclude` | No |
| ... | | | | |

---

## References

- [urfave/cli v3 FlagBase](https://pkg.go.dev/github.com/urfave/cli/v3#FlagBase) — `Validator`, `Destination`, `Sources` fields
- [urfave/cli-altsrc](https://github.com/urfave/cli-altsrc) — TOML/YAML/JSON flag sources
- [urfave/sflags](https://github.com/urfave/sflags) — struct-to-flags generation
- [urfave/cli-validation](https://pkg.go.dev/github.com/urfave/cli-validation) — `Enum`, `Min`, `Max` validators
- [knadh/koanf](https://github.com/knadh/koanf) — config library currently used by tally
- Current glue code: `cmd/tally/cmd/lint.go` lines ~710-810 (`resolveConfig` + `getOutputConfig`)
