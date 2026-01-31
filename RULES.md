# Rules Reference

tally supports rules from multiple sources, each with its own namespace prefix.

## Rule Namespaces

| Namespace | Source | Description |
|-----------|--------|-------------|
| `tally/` | tally | Custom rules implemented by tally |
| `buildkit/` | [BuildKit Linter](https://docs.docker.com/reference/build-checks/) | Docker's official Dockerfile checks |
| `hadolint/` | [Hadolint](https://github.com/hadolint/hadolint) | Shell best practices (DL/SC rules) |

## Summary

| Namespace | Implemented | Covered by BuildKit | Total |
|-----------|-------------|---------------------|-------|
| tally | 3 | - | 3 |
| buildkit | 4 + 15 captured | - | 19 |
| hadolint | 8 | ~10 | 70+ |

---

## tally Rules

Custom rules implemented by tally that go beyond BuildKit's checks.

| Rule | Description | Severity | Category | Default |
|------|-------------|----------|----------|---------|
| `tally/secrets-in-code` | Detects hardcoded secrets, API keys, and credentials using [gitleaks](https://github.com/gitleaks/gitleaks) patterns | Error | Security | Enabled |
| `tally/max-lines` | Enforces maximum number of lines in a Dockerfile | Error | Maintainability | Enabled (50 lines) |
| `tally/no-unreachable-stages` | Warns about build stages that don't contribute to the final image | Warning | Best Practice | Enabled |

### tally/secrets-in-code

Scans Dockerfile content for actual secret values (not just variable names):

- RUN commands and heredocs
- COPY/ADD heredocs
- ENV values
- ARG default values
- LABEL values

Uses gitleaks' curated database of 222+ secret patterns including AWS keys, GitHub tokens, private keys, and more.

**Complements `buildkit/SecretsUsedInArgOrEnv`**: BuildKit's rule checks variable *names* (e.g., `GITHUB_TOKEN`), while this rule detects actual
secret *values*.

### tally/max-lines

Limits Dockerfile size to encourage modular builds. Enabled by default with a 50-line limit (P90 of analyzed public Dockerfiles).

**Options:**

- `max`: Maximum lines allowed (default: 50)
- `skip-blank-lines`: Exclude blank lines from count (default: true)
- `skip-comments`: Exclude comment lines from count (default: true)

### tally/no-unreachable-stages

Detects stages that are defined but never used (not referenced by `--target` or `COPY --from`).

---

## BuildKit Rules

Rules from Docker's official BuildKit linter. tally captures these automatically during parsing.

### Implemented by tally

These rules are implemented by tally to provide enhanced functionality:

| Rule | Description | Severity | Category | Default |
|------|-------------|----------|----------|---------|
| `buildkit/SecretsUsedInArgOrEnv` | Warns when ARG/ENV variable names suggest secrets | Warning | Security | Enabled |
| `buildkit/CopyIgnoredFile` | Warns when COPY/ADD sources match .dockerignore | Warning | Correctness | Enabled |
| `buildkit/WorkdirRelativePath` | Warns about relative WORKDIR without absolute base | Warning | Correctness | Enabled |
| `buildkit/RedundantTargetPlatform` | Warns when FROM --platform=$TARGETPLATFORM is redundant | Warning | Best Practice | Enabled |

### Captured from BuildKit Linter

These rules are automatically captured from BuildKit during Dockerfile parsing:

| Rule | Description | Severity | Status |
|------|-------------|----------|--------|
| `buildkit/StageNameCasing` | Stage names should be lowercase | Warning | ✅ Captured |
| `buildkit/FromAsCasing` | The 'as' keyword should match 'from' casing | Warning | ✅ Captured |
| `buildkit/NoEmptyContinuation` | Empty continuation lines will become errors | Warning | ✅ Captured |
| `buildkit/ConsistentInstructionCasing` | Instructions should use consistent casing | Warning | ✅ Captured |
| `buildkit/DuplicateStageName` | Stage names should be unique | Warning | ✅ Captured |
| `buildkit/ReservedStageName` | Reserved words should not be stage names | Warning | ✅ Captured |
| `buildkit/JSONArgsRecommended` | JSON args recommended for ENTRYPOINT/CMD | Warning | ✅ Captured |
| `buildkit/MaintainerDeprecated` | MAINTAINER is deprecated; use LABEL | Warning | ✅ Captured |
| `buildkit/UndefinedArgInFrom` | FROM must use declared ARGs | Warning | ✅ Captured |
| `buildkit/UndefinedVar` | Variables should be defined before use | Warning | ✅ Captured |
| `buildkit/MultipleInstructionsDisallowed` | Avoid repeating instructions in a stage | Warning | ✅ Captured |
| `buildkit/LegacyKeyValueFormat` | Legacy key/value format should not be used | Warning | ✅ Captured |
| `buildkit/InvalidDefaultArgInFrom` | Default ARG values must produce valid images | Warning | ✅ Captured |
| `buildkit/FromPlatformFlagConstDisallowed` | FROM --platform should not use constants | Warning | ✅ Captured |
| `buildkit/InvalidDefinitionDescription` | Stage/arg comments must follow format | Warning | ✅ Captured |

See [Docker Build Checks](https://docs.docker.com/reference/build-checks/) for detailed documentation.

---

## Hadolint Rules

[Hadolint](https://github.com/hadolint/hadolint) rules for Dockerfile and shell best practices.
See the [Hadolint Wiki](https://github.com/hadolint/hadolint/wiki) for detailed rule documentation.

**Legend:**

- ✅ Implemented by tally
- 🔄 Covered by BuildKit rule (use that instead)
- ⏳ Not yet implemented

### DL Rules (Dockerfile Lint)

| Rule | Description | Severity | Status |
|------|-------------|----------|--------|
| DL1001 | Avoid inline ignore pragmas | Ignore | ⏳ |
| DL3000 | Use absolute WORKDIR paths | Error | 🔄 `buildkit/WorkdirRelativePath` |
| DL3001 | Avoid running certain commands in containers (ssh, vim, etc.) | Info | ⏳ |
| DL3002 | Last user should not be root | Warning | ✅ `hadolint/DL3002` |
| DL3003 | Use WORKDIR to switch directories | Warning | ⏳ |
| DL3004 | Do not use sudo; use gosu instead | Error | ✅ `hadolint/DL3004` |
| DL3006 | Always tag image versions explicitly | Warning | ✅ `hadolint/DL3006` |
| DL3007 | Avoid using "latest" tag | Warning | ✅ `hadolint/DL3007` |
| DL3008 | Pin versions in apt-get install | Warning | ⏳ |
| DL3009 | Delete apt-get lists after installing | Info | ⏳ |
| DL3010 | Use ADD for extracting archives | Info | ⏳ |
| DL3011 | Valid UNIX ports range 0-65535 | Error | ⏳ |
| DL3012 | Multiple HEALTHCHECK not allowed | Error | ✅ `hadolint/DL3012` |
| DL3013 | Pin versions in pip | Warning | ⏳ |
| DL3014 | Use -y switch with apt-get | Warning | ⏳ |
| DL3015 | Use --no-install-recommends with apt-get | Info | ⏳ |
| DL3016 | Pin versions in npm | Warning | ⏳ |
| DL3018 | Pin versions in apk add | Warning | ⏳ |
| DL3019 | Use --no-cache with apk | Info | ⏳ |
| DL3020 | Use COPY instead of ADD for files/folders | Error | ✅ `hadolint/DL3020` |
| DL3021 | COPY with multiple args requires trailing / | Error | ⏳ |
| DL3022 | COPY --from should reference defined FROM alias | Warning | ⏳ |
| DL3023 | COPY --from cannot reference own FROM alias | Error | ✅ `hadolint/DL3023` |
| DL3024 | FROM stage names must be unique | Error | ✅ `hadolint/DL3024` |
| DL3025 | Use JSON notation for CMD/ENTRYPOINT | Warning | 🔄 `buildkit/JSONArgsRecommended` |
| DL3026 | Use only allowed registries | Off (enable via config) | ✅ `hadolint/DL3026` |
| DL3027 | Avoid apt; use apt-get or apt-cache | Warning | ⏳ |
| DL3028 | Pin versions in gem install | Warning | ⏳ |
| DL3029 | Do not use --platform flag with FROM | Warning | 🔄 `buildkit/FromPlatformFlagConstDisallowed` |
| DL3030 | Use -y switch with yum | Warning | ⏳ |
| DL3032 | Missing yum clean all | Warning | ⏳ |
| DL3033 | Specify version with yum install | Warning | ⏳ |
| DL3034 | Use -y switch with zypper | Warning | ⏳ |
| DL3035 | Do not use zypper dist-upgrade | Warning | ⏳ |
| DL3036 | Missing zypper clean | Warning | ⏳ |
| DL3037 | Specify version with zypper install | Warning | ⏳ |
| DL3038 | Use -y switch with dnf | Warning | ⏳ |
| DL3040 | Missing dnf clean all | Warning | ⏳ |
| DL3041 | Specify version with dnf install | Warning | ⏳ |
| DL3042 | Use pip install --no-cache-dir | Warning | ⏳ |
| DL3043 | ONBUILD/FROM/MAINTAINER in ONBUILD not allowed | Error | ✅ `hadolint/DL3043` |
| DL3044 | Do not refer to env vars in same ENV statement | Error | 🔄 `buildkit/UndefinedVar` |
| DL3045 | COPY to relative dest without WORKDIR | Warning | 🔄 `buildkit/WorkdirRelativePath` |
| DL3046 | useradd without -l and high UID creates large images | Warning | ⏳ |
| DL3047 | wget without --progress bloats logs | Info | ⏳ |
| DL3048 | Invalid label key | Style | ⏳ |
| DL3049 | Label is missing | Info | ⏳ |
| DL3050 | Superfluous labels present | Info | ⏳ |
| DL3051 | Label is empty | Warning | ⏳ |
| DL3052 | Label is not a valid URL | Warning | ⏳ |
| DL3053 | Label does not conform to RFC3339 | Warning | ⏳ |
| DL3054 | Label is not valid SPDX license | Warning | ⏳ |
| DL3055 | Label is not valid git hash | Warning | ⏳ |
| DL3056 | Label does not conform to semver | Warning | ⏳ |
| DL3057 | HEALTHCHECK instruction missing | Ignore | ⏳ |
| DL3058 | Label is not valid email (RFC5322) | Warning | ⏳ |
| DL3059 | Multiple consecutive RUN; consider consolidation | Info | ⏳ |
| DL3060 | Missing yarn cache clean | Info | ⏳ |
| DL3061 | Invalid instruction order | Error | ✅ `hadolint/DL3061` |
| DL3062 | Pin versions in go install | Warning | ⏳ |
| DL4000 | MAINTAINER is deprecated | Error | 🔄 `buildkit/MaintainerDeprecated` |
| DL4001 | Use either wget or curl, not both | Warning | ✅ `hadolint/DL4001` |
| DL4003 | Multiple CMD instructions | Warning | 🔄 `buildkit/MultipleInstructionsDisallowed` |
| DL4004 | Multiple ENTRYPOINT instructions | Error | 🔄 `buildkit/MultipleInstructionsDisallowed` |
| DL4005 | Use SHELL to change default shell | Warning | ⏳ |
| DL4006 | Set SHELL -o pipefail before RUN with pipe | Warning | ⏳ |

### SC Rules (ShellCheck)

ShellCheck rules analyze shell scripts within RUN commands. These require shell parsing integration.

| Category | Description | Status |
|----------|-------------|--------|
| SC1xxx | Syntax/parsing (quotes, escaping) | ⏳ Planned |
| SC2xxx | Logic/correctness (word splitting, globbing) | ⏳ Planned |

Key rules include: SC2046 (quote to prevent word splitting), SC2086 (double quote variables), SC2154 (referenced but not assigned), etc.

---

## Inline Directives

Suppress rules using inline comments. tally supports multiple formats for migration compatibility:

```dockerfile
# tally ignore=buildkit/StageNameCasing
FROM alpine AS Build

# hadolint ignore=DL3024
FROM alpine AS builder

# check=skip=StageNameCasing
FROM alpine AS Builder
```

**Note:** Directives work with or without namespace prefixes. Both `ignore=DL3024` and `ignore=hadolint/DL3024` are valid.

See [README.md](README.md#ignoring-violations) for full directive documentation.

### Shell Directive for Non-POSIX Shells

When using base images with non-POSIX shells (e.g., Windows images with PowerShell), use the `shell` directive to automatically disable shell-specific linting rules:

```dockerfile
FROM mcr.microsoft.com/windows/servercore:ltsc2022
# hadolint shell=powershell
RUN Get-Process notepad | Stop-Process
```

Supported non-POSIX shells:
- `powershell` - Windows PowerShell
- `pwsh` - PowerShell Core (cross-platform)
- `cmd` / `cmd.exe` - Windows Command Prompt

When a non-POSIX shell is specified, the following rule categories are automatically disabled:
- Shell command analysis rules (e.g., DL3004 sudo detection, DL4001 wget/curl detection)
- Future ShellCheck-based rules (SC* rules)

Both `# hadolint shell=<shell>` and `# tally shell=<shell>` formats are supported.

---

## Configuration

Configure rules in `.tally.toml`:

```toml
[rules]
# Enable/disable rules by pattern
include = ["buildkit/*"]                     # Enable all buildkit rules
exclude = ["buildkit/MaintainerDeprecated"]  # Disable specific rules

# Example 1: Configure rule options and override severity
[rules.tally.max-lines]
severity = "warning"          # Options: "off", "error", "warning", "info", "style"
max = 500
skip-blank-lines = true
skip-comments = true

# Example 2: Enable "off by default" rules by providing config
[rules.hadolint.DL3026]
trusted-registries = ["docker.io", "ghcr.io"]
# Providing trusted-registries auto-enables with severity="warning"
# To use a different severity, set it explicitly: severity = "error"
```

**Severity-based enabling:** Rules with `DefaultSeverity: "off"` (like DL3026) are automatically enabled with `severity: "warning"` when you provide configuration options for them, without needing to explicitly set `enabled = true` or `severity = "warning"`. To use a different severity, set the `severity` field explicitly in the rule's configuration block.

---

## Adding New Rules

See [CLAUDE.md](CLAUDE.md#adding-new-linting-rules) for development guidelines.
