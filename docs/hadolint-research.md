# Hadolint Research - Complete Analysis

Research date: 2026-01-25
Hadolint version: Latest (main branch: dfd4cd97)
Repository: <https://github.com/hadolint/hadolint>

## Executive Summary

Hadolint is the de facto standard Dockerfile linter written in Haskell. It implements 70+ custom rules (DL codes) plus integrates ShellCheck (SC
codes) for shell script validation inside RUN instructions. The project demonstrates sophisticated architecture with strong typing, functional
programming patterns, and comprehensive rule coverage.

---

## 1. Complete Rule List

### 1.1 Rule Categories and Prefixes

Hadolint uses a **prefix-based categorization system**:

- **DL1xxx**: Meta rules (about linting itself)
- **DL3xxx**: Best practices and instruction-specific rules
- **DL4xxx**: Maintainability and anti-patterns
- **SCxxxx**: ShellCheck rules (delegated to ShellCheck library)

### 1.2 All Implemented Rules (70 DL Rules)

#### DL1xxx - Meta Rules (1 rule)

| Code | Severity | Description |
|------|----------|-------------|
| DL1001 | Ignore | Warns about using inline ignore pragmas |

#### DL3xxx - Best Practices (61 rules)

| Code | Severity | Description |
|------|----------|-------------|
| DL3000 | Error | Use absolute WORKDIR |
| DL3001 | Info | Commands like ssh, vim, shutdown don't belong in containers |
| DL3002 | Warning | Last user should not be root |
| DL3003 | Warning | Use WORKDIR to switch directories (not `cd`) |
| DL3004 | Error | Do not use sudo (unpredictable behavior) |
| DL3006 | Warning | Always tag image versions explicitly |
| DL3007 | Warning | Don't use `latest` tag |
| DL3008 | Warning | Pin versions in `apt-get install` |
| DL3009 | Info | Delete apt-get lists after installing |
| DL3010 | Info | Use ADD for extracting archives |
| DL3011 | Error | Valid UNIX ports range from 0 to 65535 |
| DL3012 | Error | Multiple `HEALTHCHECK` instructions |
| DL3013 | Warning | Pin versions in pip |
| DL3014 | Warning | Use `-y` switch with apt-get |
| DL3015 | Info | Avoid additional packages with `--no-install-recommends` |
| DL3016 | Warning | Pin versions in npm |
| DL3018 | Warning | Pin versions in `apk add` |
| DL3019 | Info | Use `--no-cache` with apk |
| DL3020 | Error | Use `COPY` instead of `ADD` for files |
| DL3021 | Error | `COPY` with >2 args requires last arg to end with `/` |
| DL3022 | Warning | `COPY --from` should reference a previous `FROM` alias |
| DL3023 | Error | `COPY --from` cannot reference its own `FROM` alias |
| DL3024 | Error | `FROM` aliases (stage names) must be unique |
| DL3025 | Warning | Use JSON notation for CMD and ENTRYPOINT |
| DL3026 | Error | Use only allowed registries in `FROM` |
| DL3027 | Warning | Don't use `apt` (use `apt-get` or `apt-cache`) |
| DL3028 | Warning | Pin versions in gem install |
| DL3029 | Warning | Do not use `--platform` flag with FROM |
| DL3030 | Warning | Use `-y` switch with yum install |
| DL3032 | Warning | `yum clean all` missing after yum command |
| DL3033 | Warning | Specify version with yum install |
| DL3034 | Warning | Non-interactive switch missing from zypper |
| DL3035 | Warning | Do not use `zypper dist-upgrade` |
| DL3036 | Warning | `zypper clean` missing after zypper use |
| DL3037 | Warning | Specify version with zypper install |
| DL3038 | Warning | Use `-y` switch with dnf install |
| DL3040 | Warning | `dnf clean all` missing after dnf command |
| DL3041 | Warning | Specify version with dnf install |
| DL3042 | Warning | Avoid cache with `pip install --no-cache-dir` |
| DL3043 | Error | Invalid instruction in ONBUILD |
| DL3044 | Error | Don't reference ENV var in same ENV statement |
| DL3045 | Warning | `COPY` to relative destination without `WORKDIR` |
| DL3046 | Warning | `useradd` without `-l` and high UID causes large images |
| DL3047 | Info | `wget` without `--progress` bloats build logs |
| DL3048 | Style | Invalid Label Key |
| DL3049 | Info | Label is missing (from schema) |
| DL3050 | Info | Superfluous labels present |
| DL3051 | Warning | Label is empty |
| DL3052 | Warning | Label is not a valid URL |
| DL3053 | Warning | Label not valid RFC3339 time format |
| DL3054 | Warning | Label is not a valid SPDX license |
| DL3055 | Warning | Label is not a valid git hash |
| DL3056 | Warning | Label doesn't conform to semver |
| DL3057 | Ignore | HEALTHCHECK instruction missing |
| DL3058 | Warning | Label is not valid email (RFC5322) |
| DL3059 | Info | Multiple consecutive RUN instructions (consolidate) |
| DL3060 | Info | `yarn cache clean` missing after `yarn install` |
| DL3061 | Error | Invalid instruction order (must start with FROM/ARG/comment) |
| DL3062 | Warning | Pin versions in go install |

#### DL4xxx - Maintainability (8 rules)

| Code | Severity | Description |
|------|----------|-------------|
| DL4000 | Error | MAINTAINER is deprecated |
| DL4001 | Warning | Either use Wget or Curl but not both |
| DL4003 | Warning | Multiple CMD instructions |
| DL4004 | Error | Multiple ENTRYPOINT instructions |
| DL4005 | Warning | Use SHELL to change default shell |
| DL4006 | Warning | Set SHELL option `-o pipefail` before RUN with pipes |

#### ShellCheck Integration (Dozens of SC codes)

Common ShellCheck rules listed in README:

- SC1000-SC1099: Syntax errors
- SC2002, SC2015, SC2026, SC2028, SC2035, SC2039, SC2046, SC2086, SC2140, SC2154, SC2155, SC2164: Various shell scripting issues

---

## 2. Severity Levels

Hadolint uses **5 severity levels** with clear semantics:

```haskell
data DLSeverity
  = DLErrorC     -- Critical issues that must be fixed
  | DLWarningC   -- Important issues, should be fixed
  | DLInfoC      -- Informational, nice to fix
  | DLStyleC     -- Style issues, optional
  | DLIgnoreC    -- Special: effectively disables the rule
```

### Severity Distribution

- **Error (18)**: Critical issues that break best practices
- **Warning (35)**: Important but not critical
- **Info (14)**: Helpful suggestions
- **Style (1)**: Cosmetic issues
- **Ignore (2)**: Meta-rules, disabled by default

### Configurable Severities

Users can override default severities via:

1. Config file (`override` section)
2. CLI flags (`--error`, `--warning`, `--info`, `--style`)
3. Environment variables

---

## 3. Inline Disable Mechanism

### 3.1 Inline Ignore Syntax

```dockerfile
# hadolint ignore=DL3006,DL3008
FROM ubuntu

# hadolint ignore=DL3003,SC1035
RUN cd /tmp && echo "hello"
```

**Key characteristics:**

- Comment must be **directly above** the instruction
- Format: `# hadolint ignore=RULE1,RULE2,...`
- Multiple rules separated by commas
- Applies **only to the next instruction**

### 3.2 Global Ignore Syntax

```dockerfile
# hadolint global ignore=DL3006,DL3003
FROM ubuntu
RUN cd /tmp && echo "foo"
```

**Key characteristics:**

- Applies to **entire file**
- Format: `# hadolint global ignore=RULE1,RULE2,...`
- Can appear anywhere in file
- Multiple global ignores are accumulated

### 3.3 Shell Pragma

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022
# hadolint shell=powershell
RUN Get-Process notepad | Stop-Process
```

- Tells Hadolint which shell the base image uses
- Disables all shell-specific rules for non-POSIX shells
- Supported: `powershell`, `pwsh`, `cmd`, etc.

### 3.4 Implementation Details

**Parser** (`src/Hadolint/Pragma.hs`):

```haskell
-- Parses inline ignores (applies to next line)
parseIgnorePragma :: Text -> Maybe [Text]

-- Parses global ignores (applies to entire file)
parseGlobalIgnorePragma :: Text -> Maybe [Text]

-- Parses shell pragma
parseShell :: Text -> Maybe Text
```

**Processing** (`src/Hadolint/Process.hs`):

- Builds map: `IntMap (Set RuleCode)` for line-specific ignores
- Builds set: `Set RuleCode` for global ignores
- Filters failures based on ignore lists
- Can be disabled with `--disable-ignore-pragma` flag

---

## 4. Overall Architecture

### 4.1 High-Level Flow

```text
Dockerfile Text
    ↓
Parser (language-docker library)
    ↓
AST (InstructionPos)
    ↓
Shell Parser (for RUN instructions)
    ↓
Rule Engine (Fold-based)
    ↓
CheckFailures (with pragmas applied)
    ↓
Formatter
    ↓
Output (JSON/TTY/Checkstyle/etc.)
```

### 4.2 Core Modules

| Module | Purpose |
|--------|---------|
| `Hadolint.Rule` | Rule types, severity, CheckFailure |
| `Hadolint.Rule.DLxxxx` | Individual rule implementations |
| `Hadolint.Rule.Shellcheck` | ShellCheck integration |
| `Hadolint.Process` | Orchestrates all rules |
| `Hadolint.Lint` | High-level linting API |
| `Hadolint.Pragma` | Parses inline/global ignores |
| `Hadolint.Shell` | Parses shell commands, wraps ShellCheck |
| `Hadolint.Config.*` | Configuration system |
| `Hadolint.Formatter.*` | Output formatters |

### 4.3 Rule Implementation Pattern

Hadolint provides **three rule helpers** with increasing complexity:

#### Pattern 1: Simple Rule (Stateless)

```haskell
-- For rules that check each instruction independently
rule :: Rule args
rule = simpleRule code severity message check
  where
    code = "DL3000"
    severity = DLErrorC
    message = "Use absolute WORKDIR"
    check (Workdir loc)
      | "/" `Text.isPrefixOf` loc = True
      | otherwise = False
    check _ = True
```

#### Pattern 2: Custom Rule (Stateful)

```haskell
-- For rules that need to accumulate state
rule :: Rule Shell.ParsedShell
rule = customRule check (emptyState Set.empty)
  where
    code = "DL4001"
    severity = DLWarningC
    message = "Either use Wget or Curl but not both"

    check line st (Run (RunArgs args _)) =
      let newArgs = extractCommands args
          newState = st |> modify (Set.union newArgs)
       in if Set.size (state newState) >= 2
            then newState |> addFail (CheckFailure {..})
            else newState
    check _ st From {} = st |> replaceWith Set.empty  -- Reset per stage
    check _ st _ = st
```

#### Pattern 3: Very Custom Rule (with done callback)

```haskell
-- For rules that need to look ahead or do post-processing
veryCustomRule ::
  (Linenumber -> State a -> Instruction args -> State a) ->  -- step
  State a ->                                                  -- initial
  (State a -> Failures) ->                                    -- done
  Rule args
```

### 4.4 Fold-Based Architecture

Hadolint uses **Control.Foldl** for elegant rule composition:

```haskell
type Rule args = Foldl.Fold (InstructionPos args) Failures

-- Rules compose with Monoid (<>)
allRules = rule1 <> rule2 <> rule3

-- Analyzed with a single pass
analyze :: Configuration -> Foldl.Fold (InstructionPos Text) AnalysisResult
analyze config =
  AnalysisResult
    <$> Hadolint.Pragma.ignored          -- Extract ignores
    <*> Hadolint.Pragma.globalIgnored    -- Extract global ignores
    <*> Foldl.premap parseShell (failures config)  -- Run all rules
```

**Benefits:**

- Single pass over Dockerfile
- Efficient memory usage
- Easy rule composition
- Parallel processing possible

---

## 5. Parser Integration

### 5.1 Dockerfile Parser

Hadolint uses **language-docker** library:

- Repository: <https://github.com/hadolint/language-docker>
- Official BuildKit/Moby parser maintained separately
- Produces typed AST with location info

```haskell
-- From language-docker
data InstructionPos args = InstructionPos
  { instruction :: Instruction args,
    lineNumber :: Linenumber,
    sourceName :: Text
  }
```

### 5.2 Shell Parser Integration

For `RUN` instructions, Hadolint:

1. **Extracts shell script** from RUN instruction
2. **Parses with ShellCheck** to get AST
3. **Extracts commands** and arguments
4. **Runs ShellCheck** for shell-specific issues
5. **Applies custom rules** to parsed commands

```haskell
data ParsedShell = ParsedShell
  { original :: Text.Text,           -- Original script
    parsed :: ParseResult,            -- ShellCheck AST
    presentCommands :: [Command]      -- Extracted commands
  }

data Command = Command
  { name :: Text.Text,
    arguments :: [CmdPart],
    flags :: [CmdPart]
  }
```

**ShellCheck Integration:**

- Uses ShellCheck as a library (not external process)
- Converts ShellCheck severities to Hadolint severities
- Tracks shell type per stage (sh, bash, etc.)
- Respects `SHELL` instruction and `# hadolint shell=` pragma
- Tracks ENV/ARG variables for ShellCheck context

---

## 6. Configuration System

### 6.1 Configuration Sources (Priority Order)

1. **CLI flags** (highest priority)
2. **Environment variables** (`TALLY_*` prefix)
3. **Config file** (discovered or specified)
4. **Built-in defaults** (lowest priority)

### 6.2 Config File Discovery

Hadolint searches in order:

1. `$PWD/.hadolint.yaml`
2. `$XDG_CONFIG_HOME/hadolint.yaml`
3. `$HOME/.config/hadolint.yaml`
4. `$HOME/.hadolint/hadolint.yaml` or `$HOME/hadolint/config.yaml`
5. `$HOME/.hadolint.yaml`

Supports both `.yaml` and `.yml` extensions.

### 6.3 Configuration Structure

```yaml
# Full schema
failure-threshold: string               # error | warning | info | style | ignore | none
format: string                          # tty | json | checkstyle | codeclimate | etc.
ignored: [string]                       # List of rule codes to ignore
no-color: boolean
no-fail: boolean
strict-labels: boolean
disable-ignore-pragma: boolean
trustedRegistries: [string]            # List of allowed registries

# Severity overrides
override:
  error: [string]
  warning: [string]
  info: [string]
  style: [string]

# Label schema validation
label-schema:
  author: text
  contact: email
  created: rfc3339
  version: semver
  documentation: url
  git-revision: hash
  license: spdx
```

### 6.4 Label Schema Validation

Hadolint can enforce label schemas with type checking:

**Supported types:**

- `text`: Any string
- `url`: Valid URI (RFC 3986)
- `email`: Valid email (RFC 5322)
- `rfc3339`: ISO 8601 datetime
- `semver`: Semantic version
- `hash`: Git hash (short or long)
- `spdx`: SPDX license identifier

**Strict mode:**
When `strict-labels: true`, warns about any labels not in schema.

---

## 7. Output Formats

Hadolint supports **9 output formats**:

1. **tty**: Colorized terminal output (default)
2. **json**: JSON array of violations
3. **checkstyle**: Checkstyle XML format
4. **codeclimate**: Code Climate JSON format
5. **gitlab_codeclimate**: GitLab-specific Code Climate
6. **gnu**: GNU error format (file:line:col: message)
7. **codacy**: Codacy JSON format
8. **sonarqube**: SonarQube JSON format
9. **sarif**: SARIF format for security tools

**Multi-file support:**

- Can lint multiple Dockerfiles in one run
- Each file gets separate result object
- Parallel processing enabled via `Parallel.Strategies`

---

## 8. Key Architectural Decisions

### 8.1 Functional Design

- **Pure functions**: Rules are pure transformations
- **Immutability**: State changes create new values
- **Type safety**: Strong typing prevents many bugs
- **Composability**: Rules combine with `<>` (monoid)

### 8.2 Performance Optimizations

- **Single-pass**: All rules run in one pass
- **Parallel linting**: Multiple files processed in parallel
- **Efficient data structures**: IntMap for line ignores, Set for codes
- **Lazy evaluation**: Haskell's lazy evaluation prevents unnecessary work

### 8.3 Extensibility Points

1. **New rules**: Add module in `src/Hadolint/Rule/`
2. **New formatters**: Add module in `src/Hadolint/Formatter/`
3. **Configuration**: Easy to add new config options
4. **Parser**: Abstracted behind `language-docker`

### 8.4 Testing Strategy

From `test/` directory:

- Unit tests for individual rules
- Integration tests with real Dockerfiles
- Property-based tests (QuickCheck)
- Golden tests for output formats

---

## 9. Rule Organization Best Practices

### 9.1 Numbering Scheme

- **DL1xxx**: Meta/lint-related rules (sparse, only DL1001)
- **DL3xxx**: Best practices (dense, 3000-3062)
- **DL4xxx**: Maintainability (sparse, 4000-4006)
- **Gaps exist**: DL3005, DL3017, DL3031, DL3039 not assigned

### 9.2 File Organization

```text
src/Hadolint/Rule/
├── DL1001.hs      # One file per rule
├── DL3000.hs
├── DL3001.hs
├── ...
├── DL4006.hs
└── Shellcheck.hs  # Special: wraps ShellCheck library
```

Each rule file exports a single `rule :: Rule args` function.

### 9.3 Documentation

Each rule has a wiki page:

- URL pattern: `https://github.com/hadolint/hadolint/wiki/DL3XXX`
- Contains: rationale, examples, exceptions
- Linked from README

---

## 10. Recommendations for Tally

### 10.1 Essential Features to Implement

1. **Severity system**: Adopt Error/Warning/Info/Style levels
2. **Inline disables**: `# tally ignore=RULE1,RULE2`
3. **Global disables**: `# tally global ignore=RULE1`
4. **Severity overrides**: Config-based rule severity changes
5. **Rule categories**: Use prefix system (TL1xxx, TL3xxx, etc.)

### 10.2 Architecture Patterns

1. **Rule composition**: Support combining rules
2. **Single-pass**: Analyze once, apply all rules
3. **State management**: Support stateful rules (like DL4001)
4. **Parser abstraction**: Keep parser separate from rules

### 10.3 Critical Rules to Prioritize

**High Priority (Security/Critical):**

- DL3000: Absolute WORKDIR
- DL3004: No sudo
- DL3006: Tag versions
- DL3008: Pin apt-get versions
- DL3013: Pin pip versions
- DL3020: COPY vs ADD
- DL3026: Trusted registries

**Medium Priority (Best Practices):**

- DL3002: Non-root user
- DL3009: Clean apt lists
- DL3015: No install recommends
- DL3025: JSON notation for CMD/ENTRYPOINT
- DL4001: Curl vs Wget
- DL4006: Pipefail

**Nice to Have:**

- Label validation (DL3048-DL3058)
- Style rules (DL3059)

### 10.4 Feature Parity Roadmap

**Phase 1: Core (MVP)**

- 20 most important rules
- Basic inline disables
- JSON output
- Config file support

**Phase 2: Parity**

- All DL3xxx rules
- Global ignores
- Multiple output formats
- Severity overrides

**Phase 3: Beyond**

- ShellCheck integration
- Label schema validation
- Custom rule plugins
- Performance optimizations

---

## 11. Implementation Notes

### 11.1 Rule Implementation Complexity

**Simple (Stateless):**

- DL3000, DL3007, DL3011, DL3020, DL3025, DL4000
- Pattern: Check instruction in isolation

**Medium (Per-stage state):**

- DL3002, DL3022, DL3023, DL3024, DL4001, DL4006
- Pattern: Track state within build stage

**Complex (Cross-stage state):**

- DL3044, DL3045, DL3057
- Pattern: Track state across entire Dockerfile

**Very Complex (Shell parsing):**

- DL3008, DL3009, DL3013, DL3015, DL3016, etc.
- Pattern: Parse shell commands, analyze arguments

### 11.2 Package Manager Rules

Hadolint has comprehensive package manager support:

| Package Manager | Rules | Coverage |
|----------------|-------|----------|
| apt-get | DL3008, DL3009, DL3014, DL3015, DL3027 | Version pinning, cache, flags |
| pip | DL3013, DL3042 | Version pinning, cache |
| npm | DL3016 | Version pinning |
| apk | DL3018, DL3019 | Version pinning, cache |
| gem | DL3028 | Version pinning |
| yum | DL3030, DL3032, DL3033 | Flags, cache, versions |
| zypper | DL3034, DL3035, DL3036, DL3037 | Flags, cache, versions |
| dnf | DL3038, DL3040, DL3041 | Flags, cache, versions |
| yarn | DL3060 | Cache |
| go | DL3062 | Version pinning |

### 11.3 Multi-Stage Build Support

Hadolint properly handles multi-stage builds:

- Tracks aliases (stage names) - DL3024
- Validates COPY --from references - DL3022, DL3023
- Resets per-stage state on FROM - Multiple rules
- Supports --platform flag detection - DL3029

---

## 12. Comparison: Hadolint vs Tally Goals

### 12.1 Similarities

- Both lint Dockerfiles for best practices
- Both use structured AST parsing
- Both support configuration files
- Both aim for speed and accuracy

### 12.2 Key Differences

| Aspect | Hadolint | Tally |
|--------|----------|-------|
| Language | Haskell | Go |
| Parser | language-docker | moby/buildkit |
| Rule Count | 70+ DL rules | TBD (planned) |
| ShellCheck | Integrated | Planned |
| Config Format | YAML | TOML |
| Inline Disable | Yes | Planned |
| Distribution | Binary, Docker, Brew, Scoop | npm, PyPI, RubyGems |

### 12.3 Tally's Advantages

1. **Easier contribution**: Go vs Haskell
2. **Better parser**: Official BuildKit parser
3. **Multi-language packages**: npm, PyPI, RubyGems
4. **Modern config**: TOML with cascading discovery
5. **Native compilation**: Fast startup, no GHC runtime

### 12.4 Learning from Hadolint

1. **Rule organization**: File per rule is maintainable
2. **Inline disables**: Essential feature
3. **Severity system**: Well-designed levels
4. **Rule helpers**: Simple/Custom/VeryCustom pattern works
5. **Documentation**: Wiki per rule is comprehensive

---

## 13. Conclusion

Hadolint represents a **mature, well-architected linter** with comprehensive rule coverage. Its functional design in Haskell provides strong
guarantees about correctness and composability, though this comes at the cost of contributor accessibility.

**For Tally to achieve feature parity**, focus on:

1. Implementing the top 30 most impactful rules
2. Supporting inline disable comments (critical UX)
3. Building a flexible severity system
4. Creating clear rule documentation
5. Maintaining simple contribution guidelines

The Go implementation in Tally, combined with the official BuildKit parser, positions it well to be a **faster, more accessible alternative** while
maintaining the high quality bar set by Hadolint.

**Next Steps:**

1. Prioritize rules based on impact (security > best practices > style)
2. Design rule registration system inspired by Hadolint's pattern
3. Implement inline disable parser
4. Create rule documentation template
5. Build integration test suite using Hadolint's test cases as reference
