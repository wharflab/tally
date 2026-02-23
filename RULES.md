# Rules Reference

tally supports rules from multiple sources, each with its own namespace prefix.

## Rule Namespaces

| Namespace | Source | Description |
|-----------|--------|-------------|
| `tally/` | tally | Custom rules implemented by tally |
| `buildkit/` | [BuildKit Linter](https://docs.docker.com/reference/build-checks/) | Docker's official Dockerfile checks |
| `hadolint/` | [Hadolint](https://github.com/hadolint/hadolint) | Shell best practices (DL/SC rules) |

## Summary

<!-- BEGIN RULES_SUMMARY -->
| Namespace | Implemented | Covered by BuildKit | Total |
|-----------|-------------|---------------------|-------|
| tally | 20 | - | 20 |
| buildkit | 17 + 5 captured | - | 22 |
| hadolint | 27 | 10 | 66 |
<!-- END RULES_SUMMARY -->

---

## tally Rules

Custom rules implemented by tally that go beyond BuildKit's checks.
See the [tally rules documentation](docs/rules/tally/) for detailed descriptions, examples, and configuration options.

| Rule | Description | Severity | Category | Default |
|------|-------------|----------|----------|---------|
| [`tally/require-stages`](docs/rules/tally/require-stages.md) | Dockerfile has no stages to build (fail-fast syntax check) | Error | Correctness | Enabled |
| [`tally/unknown-instruction`](docs/rules/tally/unknown-instruction.md) | Detects misspelled or invalid Dockerfile instruction keywords (fail-fast syntax check) | Error | Correctness | Enabled |
| [`tally/syntax-directive-typo`](docs/rules/tally/syntax-directive-typo.md) | Detects typos in `# syntax=` parser directives (fail-fast syntax check) | Error | Correctness | Enabled |
| [`tally/secrets-in-code`](docs/rules/tally/secrets-in-code.md) | Detects hardcoded secrets, API keys, and credentials using [gitleaks](https://github.com/gitleaks/gitleaks) patterns | Error | Security | Enabled |
| [`tally/prefer-vex-attestation`](docs/rules/tally/prefer-vex-attestation.md) | Recommends attaching OpenVEX as an OCI attestation instead of copying `*.vex.json` into the image | Info | Security | Enabled |
| [`tally/max-lines`](docs/rules/tally/max-lines.md) | Enforces maximum number of lines in a Dockerfile | Error | Maintainability | Enabled (50 lines) |
| [`tally/no-unreachable-stages`](docs/rules/tally/no-unreachable-stages.md) | Warns about build stages that don't contribute to the final image | Warning | Best Practice | Enabled |
| [`tally/invalid-onbuild-trigger`](docs/rules/tally/invalid-onbuild-trigger.md) 🔧 | ONBUILD trigger instruction is not a valid Dockerfile instruction | Error | Correctness | Enabled |
| [`tally/circular-stage-deps`](docs/rules/tally/circular-stage-deps.md) | Detects circular dependencies between build stages | Error | Correctness | Enabled |
| [`tally/invalid-json-form`](docs/rules/tally/invalid-json-form.md) 🔧 | Arguments appear to use JSON exec-form but contain invalid JSON | Error | Correctness | Enabled |
| [`tally/prefer-add-unpack`](docs/rules/tally/prefer-add-unpack.md) 🔧 | Suggests `ADD --unpack` instead of downloading and extracting remote archives in `RUN` | Info | Performance | Enabled |
| [`tally/prefer-multi-stage-build`](docs/rules/tally/prefer-multi-stage-build.md) 🔧 | Suggests converting single-stage builds into multi-stage builds to reduce final image size | Info | Performance | Off (experimental) |
| [`tally/prefer-copy-heredoc`](docs/rules/tally/prefer-copy-heredoc.md) 🔧 | Suggests using COPY heredoc for file creation instead of RUN echo/cat | Style | Style | Off (experimental) |
| [`tally/prefer-package-cache-mounts`](docs/rules/tally/prefer-package-cache-mounts.md) 🔧 | Suggests BuildKit cache mounts for package install/build commands and removes cache cleanup commands | Info | Performance | Off (experimental) |
| [`tally/prefer-run-heredoc`](docs/rules/tally/prefer-run-heredoc.md) 🔧 | Suggests using heredoc syntax for multi-command RUN instructions | Style | Style | Off (experimental) |
| [`tally/consistent-indentation`](docs/rules/tally/consistent-indentation.md) 🔧 | Enforces consistent indentation for Dockerfile build stages | Style | Style | Off (experimental) |
| [`tally/newline-between-instructions`](docs/rules/tally/newline-between-instructions.md) 🔧 | Controls blank lines between Dockerfile instructions | Style | Style | Enabled (grouped) |
| [`tally/no-trailing-spaces`](docs/rules/tally/no-trailing-spaces.md) 🔧 | Disallows trailing whitespace at the end of lines | Style | Style | Enabled |
| [`tally/epilogue-order`](docs/rules/tally/epilogue-order.md) 🔧 | Enforces canonical order for epilogue instructions (STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD) | Style | Style | Enabled |
| [`tally/newline-per-chained-call`](docs/rules/tally/newline-per-chained-call.md) 🔧 | Each chained element within an instruction should be on its own line | Style | Style | Enabled |

---

## BuildKit Rules

Rules from Docker's official BuildKit linter. tally captures parsing-time checks directly and reimplements some build-time (LLB conversion) checks as
static rules.

**Legend:**

- ✅ Captured from BuildKit linter
- 🔧 Auto-fixable with `tally lint --fix`

<!-- BEGIN BUILDKIT_RULES -->
<!-- Auto-generated by scripts/sync-buildkit-rules -->

tally supports **22/22** BuildKit checks:

- **5** captured during parsing
- **17** reimplemented by tally (BuildKit normally runs these during LLB conversion)
- **10** with auto-fixes (🔧)

### Implemented by tally

These BuildKit checks run during LLB conversion in Docker/BuildKit. tally reimplements them so they work as a pure static linter.

| Rule | Description | Severity | Category | Default |
|------|-------------|----------|----------|---------|
| [`buildkit/ConsistentInstructionCasing`](https://docs.docker.com/reference/build-checks/consistent-instruction-casing/) 🔧 | All commands within the Dockerfile should use the same casing (either upper or lower) | Warning | Style | Enabled |
| [`buildkit/CopyIgnoredFile`](https://docs.docker.com/reference/build-checks/copy-ignored-file/) | Attempting to Copy file that is excluded by .dockerignore | Warning | Correctness | Enabled |
| [`buildkit/DuplicateStageName`](https://docs.docker.com/reference/build-checks/duplicate-stage-name/) | Stage names should be unique | Error | Correctness | Enabled |
| [`buildkit/ExposeInvalidFormat`](https://docs.docker.com/reference/build-checks/expose-invalid-format/) | IP address and host-port mapping should not be used in EXPOSE instruction. This will become an error in a future release | Warning | Correctness | Enabled |
| [`buildkit/ExposeProtoCasing`](https://docs.docker.com/reference/build-checks/expose-proto-casing/) 🔧 | Protocol in EXPOSE instruction should be lowercase | Warning | Style | Enabled |
| [`buildkit/FromPlatformFlagConstDisallowed`](https://docs.docker.com/reference/build-checks/from-platform-flag-const-disallowed/) | FROM --platform flag should not use a constant value | Warning | Best Practice | Enabled |
| `buildkit/InvalidBaseImagePlatform` | Base image platform does not match expected target platform | Error | Correctness | Enabled |
| [`buildkit/InvalidDefaultArgInFrom`](https://docs.docker.com/reference/build-checks/invalid-default-arg-in-from/) | Default value for global ARG results in an empty or invalid base image name | Error | Correctness | Enabled |
| [`buildkit/JSONArgsRecommended`](https://docs.docker.com/reference/build-checks/json-args-recommended/) 🔧 | JSON arguments recommended for ENTRYPOINT/CMD to prevent unintended behavior related to OS signals | Info | Best Practice | Enabled |
| [`buildkit/LegacyKeyValueFormat`](https://docs.docker.com/reference/build-checks/legacy-key-value-format/) 🔧 | Legacy key/value format with whitespace separator should not be used | Warning | Style | Enabled |
| [`buildkit/MultipleInstructionsDisallowed`](https://docs.docker.com/reference/build-checks/multiple-instructions-disallowed/) 🔧 | Multiple instructions of the same type should not be used in the same stage | Warning | Best Practice | Enabled |
| [`buildkit/RedundantTargetPlatform`](https://docs.docker.com/reference/build-checks/redundant-target-platform/) | Setting platform to predefined $TARGETPLATFORM in FROM is redundant as this is the default behavior | Warning | Best Practice | Enabled |
| [`buildkit/ReservedStageName`](https://docs.docker.com/reference/build-checks/reserved-stage-name/) | Reserved words should not be used as stage names | Error | Correctness | Enabled |
| [`buildkit/SecretsUsedInArgOrEnv`](https://docs.docker.com/reference/build-checks/secrets-used-in-arg-or-env/) | Sensitive data should not be used in the ARG or ENV commands | Warning | Security | Enabled |
| [`buildkit/UndefinedArgInFrom`](https://docs.docker.com/reference/build-checks/undefined-arg-in-from/) | FROM command must use declared ARGs | Warning | Correctness | Enabled |
| [`buildkit/UndefinedVar`](https://docs.docker.com/reference/build-checks/undefined-var/) | Variables should be defined before their use | Warning | Correctness | Enabled |
| [`buildkit/WorkdirRelativePath`](https://docs.docker.com/reference/build-checks/workdir-relative-path/) | Relative workdir without an absolute workdir declared within the build can have unexpected results if the base image changes | Warning | Correctness | Enabled |

### Captured from BuildKit linter

These checks are emitted by BuildKit during Dockerfile parsing and are captured directly by tally.

| Rule | Description | Severity | Default | Status |
|------|-------------|----------|---------|--------|
| [`buildkit/FromAsCasing`](https://docs.docker.com/reference/build-checks/from-as-casing/) 🔧 | The 'as' keyword should match the case of the 'from' keyword | Warning | Enabled | ✅🔧 |
| [`buildkit/InvalidDefinitionDescription`](https://docs.docker.com/reference/build-checks/invalid-definition-description/) 🔧 | Comment for build stage or argument should follow the format: `# <arg/stage name> <description>`. If this is not intended to be a description comment, add an empty line or comment between the instruction and the comment. | Warning | Off (experimental) | ✅🔧 |
| [`buildkit/MaintainerDeprecated`](https://docs.docker.com/reference/build-checks/maintainer-deprecated/) 🔧 | The MAINTAINER instruction is deprecated, use a label instead to define an image author | Warning | Enabled | ✅🔧 |
| [`buildkit/NoEmptyContinuation`](https://docs.docker.com/reference/build-checks/no-empty-continuation/) 🔧 | Empty continuation lines will become errors in a future release | Warning | Enabled | ✅🔧 |
| [`buildkit/StageNameCasing`](https://docs.docker.com/reference/build-checks/stage-name-casing/) 🔧 | Stage names should be lowercase | Warning | Enabled | ✅🔧 |

See [Docker Build Checks](https://docs.docker.com/reference/build-checks/) for detailed documentation.
<!-- END BUILDKIT_RULES -->

---

## Hadolint Rules

[Hadolint](https://github.com/hadolint/hadolint) rules for Dockerfile and shell best practices.
See the [Hadolint Wiki](https://github.com/hadolint/hadolint/wiki) for detailed rule documentation.

**Legend:**

- ✅ Implemented by tally
- 🔧 Auto-fixable with `tally lint --fix`
- 🔄 Covered by BuildKit or tally rule (use that instead)
- ⛔ Not planned (intentionally not implemented)
- ⏳ Not yet implemented

### DL Rules (Dockerfile Lint)

<!-- BEGIN HADOLINT_DL_RULES -->
| Rule | Description | Severity | Status |
|------|-------------|----------|--------|
| [DL1001](https://github.com/hadolint/hadolint/wiki/DL1001) | Please refrain from using inline ignore pragmas `# hadolint ignore=DLxxxx`. | Ignore | ⏳ |
| [DL3000](https://github.com/hadolint/hadolint/wiki/DL3000) | Use absolute WORKDIR. | Error | 🔄 `buildkit/WorkdirRelativePath` |
| [DL3001](https://github.com/hadolint/hadolint/wiki/DL3001) | For some bash commands it makes no sense running them in a Docker container like ssh, vim, shutdown, service, ps, free, top, kill, mount, ifconfig. | Info | ✅ `hadolint/DL3001` |
| [DL3002](https://github.com/hadolint/hadolint/wiki/DL3002) | Last user should not be root. | Warning | ✅ `hadolint/DL3002` |
| [DL3003](https://github.com/hadolint/hadolint/wiki/DL3003) | Use WORKDIR to switch to a directory. | Warning | ✅🔧 `hadolint/DL3003` |
| [DL3004](https://github.com/hadolint/hadolint/wiki/DL3004) | Do not use sudo as it leads to unpredictable behavior. Use a tool like gosu to enforce root. | Error | ✅ `hadolint/DL3004` |
| [DL3006](https://github.com/hadolint/hadolint/wiki/DL3006) | Always tag the version of an image explicitly. | Warning | ✅ `hadolint/DL3006` |
| [DL3007](https://github.com/hadolint/hadolint/wiki/DL3007) | Using latest is prone to errors if the image will ever update. Pin the version explicitly to a release tag. | Warning | ✅ `hadolint/DL3007` |
| [DL3008](https://github.com/hadolint/hadolint/wiki/DL3008) | Pin versions in apt-get install. | Warning | ⏳ |
| [DL3009](https://github.com/hadolint/hadolint/wiki/DL3009) | Delete the apt-get lists after installing something. | Info | ⛔ Not planned (`tally/prefer-package-cache-mounts`) |
| [DL3010](https://github.com/hadolint/hadolint/wiki/DL3010) | Use ADD for extracting archives into an image. | Info | ✅ `hadolint/DL3010` |
| [DL3011](https://github.com/hadolint/hadolint/wiki/DL3011) | Valid UNIX ports range from 0 to 65535. | Error | ✅ `hadolint/DL3011` |
| [DL3012](https://github.com/hadolint/hadolint/wiki/DL3012) | Multiple `HEALTHCHECK` instructions. | Error | 🔄 `buildkit/MultipleInstructionsDisallowed` |
| [DL3013](https://github.com/hadolint/hadolint/wiki/DL3013) | Pin versions in pip. | Warning | ⏳ |
| [DL3014](https://github.com/hadolint/hadolint/wiki/DL3014) | Use the `-y` switch. | Warning | ✅🔧 `hadolint/DL3014` |
| [DL3015](https://github.com/hadolint/hadolint/wiki/DL3015) | Avoid additional packages by specifying --no-install-recommends. | Info | ⏳ |
| [DL3016](https://github.com/hadolint/hadolint/wiki/DL3016) | Pin versions in `npm`. | Warning | ⏳ |
| [DL3018](https://github.com/hadolint/hadolint/wiki/DL3018) | Pin versions in apk add. Instead of `apk add <package>` use `apk add <package>=<version>`. | Warning | ⏳ |
| [DL3019](https://github.com/hadolint/hadolint/wiki/DL3019) | Use the `--no-cache` switch to avoid the need to use `--update` and remove `/var/cache/apk/*` when done installing packages. | Info | ⛔ Not planned (`tally/prefer-package-cache-mounts`) |
| [DL3020](https://github.com/hadolint/hadolint/wiki/DL3020) | Use `COPY` instead of `ADD` for files and folders. | Error | ✅ `hadolint/DL3020` |
| [DL3021](https://github.com/hadolint/hadolint/wiki/DL3021) | `COPY` with more than 2 arguments requires the last argument to end with `/` | Error | ✅ `hadolint/DL3021` |
| [DL3022](https://github.com/hadolint/hadolint/wiki/DL3022) | `COPY --from` should reference a previously defined `FROM` alias | Warning | ✅ `hadolint/DL3022` |
| [DL3023](https://github.com/hadolint/hadolint/wiki/DL3023) | `COPY --from` cannot reference its own `FROM` alias | Error | ✅ `hadolint/DL3023` |
| [DL3024](https://github.com/hadolint/hadolint/wiki/DL3024) | `FROM` aliases (stage names) must be unique | Error | 🔄 `buildkit/DuplicateStageName` |
| [DL3025](https://github.com/hadolint/hadolint/wiki/DL3025) | Use arguments JSON notation for CMD and ENTRYPOINT arguments | Warning | 🔄 `buildkit/JSONArgsRecommended` |
| [DL3026](https://github.com/hadolint/hadolint/wiki/DL3026) | Use only an allowed registry in the FROM image | Error | ✅ `hadolint/DL3026` |
| [DL3027](https://github.com/hadolint/hadolint/wiki/DL3027) | Do not use `apt` as it is meant to be an end-user tool, use `apt-get` or `apt-cache` instead | Warning | ✅🔧 `hadolint/DL3027` |
| [DL3028](https://github.com/hadolint/hadolint/wiki/DL3028) | Pin versions in gem install. Instead of `gem install <gem>` use `gem install <gem>:<version>` | Warning | ⏳ |
| [DL3029](https://github.com/hadolint/hadolint/wiki/DL3029) | Do not use --platform flag with FROM. | Warning | 🔄 `buildkit/FromPlatformFlagConstDisallowed` |
| [DL3030](https://github.com/hadolint/hadolint/wiki/DL3030) | Use the `-y` switch to avoid manual input `yum install -y <package>` | Warning | ✅🔧 `hadolint/DL3030` |
| [DL3032](https://github.com/hadolint/hadolint/wiki/DL3032) | `yum clean all` missing after yum command. | Warning | ⛔ Not planned (`tally/prefer-package-cache-mounts`) |
| [DL3033](https://github.com/hadolint/hadolint/wiki/DL3033) | Specify version with `yum install -y <package>-<version>` | Warning | ⏳ |
| [DL3034](https://github.com/hadolint/hadolint/wiki/DL3034) | Non-interactive switch missing from `zypper` command: `zypper install -y` | Warning | ✅🔧 `hadolint/DL3034` |
| [DL3035](https://github.com/hadolint/hadolint/wiki/DL3035) | Do not use `zypper dist-upgrade`. | Warning | ⏳ |
| [DL3036](https://github.com/hadolint/hadolint/wiki/DL3036) | `zypper clean` missing after zypper use. | Warning | ⛔ Not planned (`tally/prefer-package-cache-mounts`) |
| [DL3037](https://github.com/hadolint/hadolint/wiki/DL3037) | Specify version with `zypper install -y <package>[=]<version>`. | Warning | ⏳ |
| [DL3038](https://github.com/hadolint/hadolint/wiki/DL3038) | Use the `-y` switch to avoid manual input `dnf install -y <package>` | Warning | ✅🔧 `hadolint/DL3038` |
| [DL3040](https://github.com/hadolint/hadolint/wiki/DL3040) | `dnf clean all` missing after dnf command. | Warning | ⛔ Not planned (`tally/prefer-package-cache-mounts`) |
| [DL3041](https://github.com/hadolint/hadolint/wiki/DL3041) | Specify version with `dnf install -y <package>-<version>` | Warning | ⏳ |
| [DL3042](https://github.com/hadolint/hadolint/wiki/DL3042) | Avoid cache directory with `pip install --no-cache-dir <package>`. | Warning | ⛔ Not planned (`tally/prefer-package-cache-mounts`) |
| [DL3043](https://github.com/hadolint/hadolint/wiki/DL3043) | `ONBUILD`, `FROM` or `MAINTAINER` triggered from within `ONBUILD` instruction. | Error | ✅ `hadolint/DL3043` |
| [DL3044](https://github.com/hadolint/hadolint/wiki/DL3044) | Do not refer to an environment variable within the same `ENV` statement where it is defined. | Error | 🔄 `buildkit/UndefinedVar` |
| [DL3045](https://github.com/hadolint/hadolint/wiki/DL3045) | `COPY` to a relative destination without `WORKDIR` set. | Warning | ✅ `hadolint/DL3045` |
| [DL3046](https://github.com/hadolint/hadolint/wiki/DL3046) |  `useradd` without flag `-l` and high UID will result in excessively large Image. | Warning | ✅🔧 `hadolint/DL3046` |
| [DL3047](https://github.com/hadolint/hadolint/wiki/DL3047) | `wget` without flag `--progress` will result in excessively bloated build logs when downloading larger files. | Info | ✅🔧 `hadolint/DL3047` |
| [DL3048](https://github.com/hadolint/hadolint/wiki/DL3048) | Invalid Label Key | Style | ⏳ |
| [DL3049](https://github.com/hadolint/hadolint/wiki/DL3049) | Label `<label>` is missing. | Info | ⏳ |
| [DL3050](https://github.com/hadolint/hadolint/wiki/DL3050) | Superfluous label(s) present. | Info | ⏳ |
| [DL3051](https://github.com/hadolint/hadolint/wiki/DL3051) | Label `<label>` is empty. | Warning | ⏳ |
| [DL3052](https://github.com/hadolint/hadolint/wiki/DL3052) | Label `<label>` is not a valid URL. | Warning | ⏳ |
| [DL3053](https://github.com/hadolint/hadolint/wiki/DL3053) | Label `<label>` is not a valid time format - must conform to RFC3339. | Warning | ⏳ |
| [DL3054](https://github.com/hadolint/hadolint/wiki/DL3054) | Label `<label>` is not a valid SPDX license identifier. | Warning | ⏳ |
| [DL3055](https://github.com/hadolint/hadolint/wiki/DL3055) | Label `<label>` is not a valid git hash. | Warning | ⏳ |
| [DL3056](https://github.com/hadolint/hadolint/wiki/DL3056) | Label `<label>` does not conform to semantic versioning. | Warning | ⏳ |
| [DL3057](https://github.com/hadolint/hadolint/wiki/DL3057) | `HEALTHCHECK` instruction missing. | Ignore | ✅ `hadolint/DL3057` |
| [DL3058](https://github.com/hadolint/hadolint/wiki/DL3058) | Label `<label>` is not a valid email format - must conform to RFC5322. | Warning | ⏳ |
| [DL3059](https://github.com/hadolint/hadolint/wiki/DL3059) | Multiple consecutive `RUN` instructions. Consider consolidation. | Info | 🔄 [`tally/prefer-run-heredoc`](docs/rules/tally/prefer-run-heredoc.md) |
| [DL3060](https://github.com/hadolint/hadolint/wiki/DL3060) | `yarn cache clean` missing after `yarn install` was run. | Info | ⛔ Not planned (`tally/prefer-package-cache-mounts`) |
| [DL3061](https://github.com/hadolint/hadolint/wiki/DL3061) | Invalid instruction order. Dockerfile must begin with `FROM`, `ARG` or comment. | Error | ✅ `hadolint/DL3061` |
| [DL3062](https://github.com/hadolint/hadolint/wiki/DL3062) | Pin versions in go install. Instead of `go install <package>` use `go install <package>@<version>` | Warning | ⏳ |
| [DL4000](https://github.com/hadolint/hadolint/wiki/DL4000) | MAINTAINER is deprecated. | Error | 🔄 `buildkit/MaintainerDeprecated` |
| [DL4001](https://github.com/hadolint/hadolint/wiki/DL4001) | Either use Wget or Curl but not both. | Warning | ✅ `hadolint/DL4001` |
| [DL4003](https://github.com/hadolint/hadolint/wiki/DL4003) | Multiple `CMD` instructions found. | Warning | 🔄 `buildkit/MultipleInstructionsDisallowed` |
| [DL4004](https://github.com/hadolint/hadolint/wiki/DL4004) | Multiple `ENTRYPOINT` instructions found. | Error | 🔄 `buildkit/MultipleInstructionsDisallowed` |
| [DL4005](https://github.com/hadolint/hadolint/wiki/DL4005) | Use `SHELL` to change the default shell. | Warning | ✅🔧 `hadolint/DL4005` |
| [DL4006](https://github.com/hadolint/hadolint/wiki/DL4006) | Set the `SHELL` option -o pipefail before `RUN` with a pipe in it | Warning | ✅🔧 `hadolint/DL4006` |
<!-- END HADOLINT_DL_RULES -->

#### Cache-cleanup Hadolint rules marked as not planned

tally intentionally does not plan to implement Hadolint rules that enforce package-manager cache cleanup patterns (for example `* clean` and
`--no-cache-dir`).
Instead, tally promotes BuildKit cache mounts via [`tally/prefer-package-cache-mounts`](docs/rules/tally/prefer-package-cache-mounts.md), which is
better aligned with modern container build recommendations and improves rebuild performance without cache-disabling patterns.

#### DL3057: HEALTHCHECK Instruction Missing (Enhanced)

tally goes beyond Hadolint's static check by resolving base image metadata from the registry when `--slow-checks` is enabled.

**Docker behavior:** `HEALTHCHECK` is inherited from base images at runtime. If a base image defines `HEALTHCHECK CMD ...`, child images inherit it
automatically. `HEALTHCHECK NONE` explicitly disables any inherited health check.

**Three violation scenarios:**

| Scenario | Fast path (static) | With `--slow-checks` |
|---|---|---|
| No `HEALTHCHECK CMD` in Dockerfile, base has HC | Violation (false positive) | Suppressed (inherited from base) |
| No `HEALTHCHECK CMD` in Dockerfile, base has no HC | Violation | Violation confirmed |
| `HEALTHCHECK NONE` in Dockerfile, base has no HC | Violation (generic "missing") | Specific: "HEALTHCHECK NONE has no effect" |

**Configuration:** The async behavior is controlled by `--slow-checks` (or `slow-checks` in config). When set to `off`, only the fast static check
runs.

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

When using base images with non-POSIX shells (e.g., Windows images with PowerShell), use the `shell` directive to automatically disable shell-specific
linting rules:

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

**Severity-based enabling:** Rules with `DefaultSeverity: "off"` (like DL3026) are automatically enabled with `severity: "warning"` when you provide
configuration options for them, without needing to explicitly set `enabled = true` or `severity = "warning"`. To use a different severity, set the
`severity` field explicitly in the rule's configuration block.

---

## Adding New Rules

See [CLAUDE.md](CLAUDE.md#adding-new-linting-rules) for development guidelines.
