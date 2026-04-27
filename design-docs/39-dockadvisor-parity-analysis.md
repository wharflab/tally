# Dockadvisor Parity Analysis

> **Source**: [github.com/deckrun/dockadvisor](https://github.com/deckrun/dockadvisor)  
> **Analyzed at commit**: `main` branch, April 2026  
> **Author**: @copilot

---

## 1. Overview

[Dockadvisor](https://github.com/deckrun/dockadvisor) is a Dockerfile linter written in Go that
uses BuildKit's official parser (`github.com/moby/buildkit/frontend/dockerfile/parser`) as its
AST source. It exposes its logic as a Go library (`parse.ParseDockerfile`), a CLI, and a
WebAssembly module for browser use. It reports a **0–100 quality score** alongside a list of
rule violations.

This document compares dockadvisor's ~50 rules against tally's rule set to identify gaps,
overlaps, and opportunities.

---

## 2. Dockadvisor Rule Catalogue

All rules come from the `parse/` package. Rules are grouped by instruction and by scope (global
checks that run across all instructions).

### 2.1 Global / Cross-Instruction Rules

| Rule Code | Severity | Description | Source File |
|-----------|----------|-------------|-------------|
| `ParserWarning` | Warning | Passes through BuildKit parser warnings with a generic code | [`parse.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/parse.go) |
| `NoEmptyContinuation` | Warning | Empty line following a backslash continuation character | [`continuation.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/continuation.go) |
| `ConsistentInstructionCasing` | Warning | Instruction keywords should be uniformly upper- or lowercase | [`casing.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/casing.go) |
| `DuplicateStageName` | Error | Two `FROM … AS` declarations share the same stage name (case-insensitive) | [`duplicate_stages.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/duplicate_stages.go) |
| `FromPlatformFlagConstDisallowed` | Warning | `FROM --platform=<literal>` without the stage being referenced by a variable reference in another `FROM` | [`platform_const.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/platform_const.go) |
| `JSONArgsRecommended` | Warning | `CMD`/`ENTRYPOINT` in shell form without an explicit `SHELL` instruction (signals not forwarded) | [`json_args.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/json_args.go) |
| `UndefinedArgInFrom` | Error | `FROM` references a variable not declared as a global `ARG` | [`undefined_arg.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/undefined_arg.go) |
| `UndefinedVar` | Error | Variable reference inside an instruction that has not been declared via `ARG` or `ENV` in scope | [`undefined_var.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/undefined_var.go) |
| `MultipleInstructionsDisallowed` | Error | `CMD`, `HEALTHCHECK`, or `ENTRYPOINT` appears more than once per stage | [`multiple_instructions.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/multiple_instructions.go) |
| `SecretsUsedInArgOrEnv` | Warning | Variable name in `ARG`/`ENV` matches a secret token pattern (`password`, `token`, `key`, …) | [`secrets.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/secrets.go) |
| `InvalidDefaultArgInFrom` | Error | Global `ARG` has no default and its empty expansion would create an invalid image reference in `FROM` | [`invalid_default_arg.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/invalid_default_arg.go) |
| `UnrecognizedInstruction` | Fatal | Instruction keyword is not a known Dockerfile instruction | [`parse.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/parse.go) |

### 2.2 `FROM` Rules

| Rule Code | Severity | Description | Source File |
|-----------|----------|-------------|-------------|
| `FromMissingImage` | Error | `FROM` has no image reference | [`from.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go) |
| `FromInvalidImageReference` | Error | Image reference fails a basic format validation | [`from.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go) |
| `FromInvalidPlatform` | Error | `--platform` value fails format validation | [`from.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go) |
| `FromInvalidStageName` | Error | Stage name (`AS <name>`) fails character-set/start-character validation | [`from.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go) |
| `ReservedStageName` | Error | Stage name is a Docker-reserved word | [`from.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go) |
| `RedundantTargetPlatform` | Warning | `FROM --platform=$TARGETPLATFORM` is the default; the flag is redundant | [`from.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go) |
| `StageNameCasing` | Warning | Stage name is not lowercase | [`from.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go) |
| `FromAsCasing` | Warning | `FROM` and `AS` keywords use inconsistent casing (e.g., `from … AS`) | [`from.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go) |

### 2.3 `RUN` Rules

| Rule Code | Severity | Description | Source File |
|-----------|----------|-------------|-------------|
| `RunMissingCommand` | Error | `RUN` has no command body | [`run.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/run.go) |
| `RunInvalidExecForm` | Error | `RUN [...]` is not valid JSON or uses single quotes | [`run.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/run.go) |
| `RunInvalidMountFlag` | Error | `--mount=type=<x>` uses an unknown mount type | [`run.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/run.go) |
| `RunInvalidNetworkFlag` | Error | `--network=<x>` is not `default`, `none`, or `host` | [`run.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/run.go) |
| `RunInvalidSecurityFlag` | Error | `--security=<x>` is not `sandbox` or `insecure` | [`run.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/run.go) |

### 2.4 `CMD` / `ENTRYPOINT` Rules

| Rule Code | Severity | Description | Source File |
|-----------|----------|-------------|-------------|
| `CmdMissingCommand` | Error | `CMD` has no argument | [`cmd.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/cmd.go) |
| `CmdInvalidExecForm` | Error | `CMD [...]` is not valid JSON or uses single quotes | [`cmd.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/cmd.go) |
| `EntrypointMissingCommand` | Error | `ENTRYPOINT` has no argument | [`entrypoint.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/entrypoint.go) |
| `EntrypointInvalidExecForm` | Error | `ENTRYPOINT [...]` is not valid JSON or uses single quotes | [`entrypoint.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/entrypoint.go) |

### 2.5 `EXPOSE` Rules

| Rule Code | Severity | Description | Source File |
|-----------|----------|-------------|-------------|
| `ExposeInvalidFormat` | Error | Port spec contains a colon (IP or host-port mapping not allowed) | [`expose.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose.go) |
| `ExposePortOutOfRange` | Error | Port number is outside 0–65535 | [`expose.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose.go) |
| `ExposeInvalidProtocol` | Error | Protocol is not `tcp` or `udp` (e.g. `80/http`, `443/sctp`) | [`expose.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose.go) |
| `ExposeProtoCasing` | Warning | Protocol in `EXPOSE` is not lowercase (e.g. `80/TCP`) | [`expose.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose.go) |

### 2.6 `COPY` / `ADD` Rules

| Rule Code | Severity | Description | Source File |
|-----------|----------|-------------|-------------|
| `CopyMissingArguments` | Error | `COPY` has fewer than two non-flag arguments | [`copy.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/copy.go) |
| `CopyInvalidFlag` | Error | `COPY` flag is not one of the recognised flags | [`copy.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/copy.go) |
| `AddMissingArguments` | Error | `ADD` has fewer than two non-flag arguments | [`add.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/add.go) |
| `AddInvalidFlag` | Error | `ADD` flag is not one of the recognised flags | [`add.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/add.go) |

### 2.7 `ENV` / `ARG` Rules

| Rule Code | Severity | Description | Source File |
|-----------|----------|-------------|-------------|
| `EnvMissingKeyValue` | Error | `ENV` has no key-value pair | [`env.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/env.go) |
| `EnvInvalidFormat` | Error | `ENV` key-value pair is not in `KEY=value` format | [`env.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/env.go) |
| `LegacyKeyValueFormat` (ENV) | Warning | `ENV key value` (space-separated) is deprecated | [`env.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/env.go) |
| `ArgMissingName` | Error | `ARG` has no argument name | [`arg.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/arg.go) |
| `ArgInvalidFormat` | Error | `ARG` format is invalid | [`arg.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/arg.go) |
| `LegacyKeyValueFormat` (ARG) | Warning | `ARG key value` (space-separated) is ambiguous/deprecated | [`arg.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/arg.go) |

### 2.8 Other Instruction Rules

| Instruction | Rule Code | Severity | Description | Source File |
|------------|-----------|----------|-------------|-------------|
| `USER` | `UserMissingValue` | Error | `USER` has no argument | [`user.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/user.go) |
| `USER` | `UserInvalidFormat` | Error | `USER` format is not `user[:group]` | [`user.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/user.go) |
| `LABEL` | `LabelMissingKeyValue` | Error | `LABEL` has no key-value pair | [`label.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/label.go) |
| `LABEL` | `LabelInvalidFormat` | Error | `LABEL` is not in `key=value` format | [`label.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/label.go) |
| `MAINTAINER` | `MaintainerMissingName` | Error | `MAINTAINER` has no name | [`maintainer.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/maintainer.go) |
| `MAINTAINER` | `MaintainerDeprecated` | Warning | `MAINTAINER` is deprecated in favour of `LABEL` | [`maintainer.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/maintainer.go) |
| `STOPSIGNAL` | `StopsignalMissingValue` | Error | `STOPSIGNAL` has no signal argument | [`stopsignal.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/stopsignal.go) |
| `SHELL` | `ShellMissingConfig` | Error | `SHELL` has no argument | [`shell.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/shell.go) |
| `SHELL` | `ShellRequiresJsonForm` | Error | `SHELL` is not in JSON form | [`shell.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/shell.go) |
| `SHELL` | `ShellInvalidJsonForm` | Error | `SHELL` JSON array is malformed | [`shell.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/shell.go) |
| `HEALTHCHECK` | `HealthcheckMissingCmd` | Error | `HEALTHCHECK` has no `CMD` keyword and is not `HEALTHCHECK NONE` | [`healthcheck.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/healthcheck.go) |
| `ONBUILD` | `OnbuildMissingInstruction` | Error | `ONBUILD` has no instruction following it | [`onbuild.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/onbuild.go) |
| `WORKDIR` | `WorkdirRelativePath` | Warning | `WORKDIR` uses a relative path | [`workdir.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/workdir.go) |
| `VOLUME` | `VolumeMissingPath` | Error | `VOLUME` has no mount-point argument | [`volume.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/volume.go) |
| `VOLUME` | `VolumeInvalidJsonForm` | Error | `VOLUME` JSON array is malformed | [`volume.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/volume.go) |

---

## 3. Tally Rule Coverage

Tally organises rules into four namespaces:

| Namespace | Description |
|-----------|-------------|
| `buildkit/*` | Re-implementations / exposures of BuildKit's built-in linter rules |
| `tally/*` | Original tally rules (style, best practices, security, specialised ecosystems) |
| `hadolint/*` | Ports / compatible implementations of hadolint's `DL3xxx`/`DL4xxx` rules |
| Implicit | Errors surfaced directly by the BuildKit parser (parse-time failures) |

### 3.1 BuildKit Namespace Rules

| Tally Rule ID | Equivalent Dockadvisor Rule(s) |
|--------------|-------------------------------|
| `buildkit/StageNameCasing` | `StageNameCasing` |
| `buildkit/FromAsCasing` | `FromAsCasing` |
| `buildkit/ConsistentInstructionCasing` | `ConsistentInstructionCasing` |
| `buildkit/LegacyKeyValueFormat` | `LegacyKeyValueFormat` (ENV + ARG) |
| `buildkit/ExposeProtoCasing` | `ExposeProtoCasing` |
| `buildkit/NoEmptyContinuation` | `NoEmptyContinuation` |
| `buildkit/DuplicateStageName` | `DuplicateStageName` |
| `buildkit/ReservedStageName` | `ReservedStageName` |
| `buildkit/UndefinedArgInFrom` | `UndefinedArgInFrom` |
| `buildkit/UndefinedVar` | `UndefinedVar` |
| `buildkit/InvalidDefaultArgInFrom` | `InvalidDefaultArgInFrom` |
| `buildkit/InvalidBaseImagePlatform` | _(no direct equivalent; async registry check)_ |
| `buildkit/ExposeInvalidFormat` | `ExposeInvalidFormat` (partial — see §4) |
| `buildkit/CopyIgnoredFile` | _(no equivalent in dockadvisor)_ |
| `buildkit/JSONArgsRecommended` | `JSONArgsRecommended` |
| `buildkit/MaintainerDeprecated` | `MaintainerDeprecated` |
| `buildkit/WorkdirRelativePath` | `WorkdirRelativePath` |
| `buildkit/MultipleInstructionsDisallowed` | `MultipleInstructionsDisallowed` |
| `buildkit/RedundantTargetPlatform` | `RedundantTargetPlatform` |
| `buildkit/FromPlatformFlagConstDisallowed` | `FromPlatformFlagConstDisallowed` |
| `buildkit/SecretsUsedInArgOrEnv` | `SecretsUsedInArgOrEnv` |
| `buildkit/InvalidDefinitionDescription` | _(no equivalent)_ |

### 3.2 Syntax-Validation Errors Handled by BuildKit Parser

The following dockadvisor Error-level rules exist to guard against malformed instructions. In
tally these are propagated as BuildKit **parser errors** (build failures), not as linter
warnings. This is the correct behaviour because the BuildKit parser is the authoritative source
of truth for Dockerfile syntax.

| Dockadvisor Rule | How tally handles it |
|-----------------|----------------------|
| `FromMissingImage`, `FromInvalidImageReference`, `FromInvalidPlatform`, `FromInvalidStageName` | BuildKit parse error |
| `RunMissingCommand`, `RunInvalidExecForm`, `RunInvalidMountFlag`, `RunInvalidNetworkFlag`, `RunInvalidSecurityFlag` | BuildKit parse error or `tally/invalid-json-form` |
| `CmdMissingCommand`, `CmdInvalidExecForm` | BuildKit parse error or `tally/invalid-json-form` |
| `EntrypointMissingCommand`, `EntrypointInvalidExecForm` | BuildKit parse error or `tally/invalid-json-form` |
| `CopyMissingArguments`, `CopyInvalidFlag` | BuildKit parse error |
| `AddMissingArguments`, `AddInvalidFlag` | BuildKit parse error |
| `EnvMissingKeyValue`, `EnvInvalidFormat` | BuildKit parse error |
| `ArgMissingName`, `ArgInvalidFormat` | BuildKit parse error |
| `UserMissingValue`, `UserInvalidFormat` | BuildKit parse error |
| `LabelMissingKeyValue`, `LabelInvalidFormat` | BuildKit parse error |
| `MaintainerMissingName` | BuildKit parse error |
| `StopsignalMissingValue` | BuildKit parse error |
| `ShellMissingConfig`, `ShellRequiresJsonForm` | BuildKit parse error |
| `ShellInvalidJsonForm` | `tally/invalid-json-form` |
| `HealthcheckMissingCmd` | BuildKit parse error |
| `OnbuildMissingInstruction` | `tally/invalid-onbuild-trigger` (partial) |
| `VolumeMissingPath`, `VolumeInvalidJsonForm` | BuildKit parse error or `tally/invalid-json-form` |
| `UnrecognizedInstruction` | BuildKit parse error |

---

## 4. Gap Analysis — Rules in Dockadvisor Not Fully Covered by Tally

The following rules are **present in dockadvisor but missing or only partially implemented in
tally**. For each, we note an implementation pointer and whether a test corpus exists in
dockadvisor.

### 4.1 `ExposePortOutOfRange` — Port number outside 0–65535

**Status in tally:** ❌ Not implemented

**Description:**  
Dockadvisor validates that `EXPOSE` port numbers are within the valid TCP/UDP port range
(0–65535). Tally's `buildkit/ExposeInvalidFormat` only checks for colons (IP addresses and
host-port mappings); it does not validate the numeric range.

**Potential implementation:** A new `tally/expose-port-out-of-range` rule (or an extension of
`buildkit/ExposeInvalidFormat`) that parses the integer before any `/protocol` and verifies it
is in [0, 65535].

**Dockadvisor implementation:**  
[`parse/expose.go` — `checkExposePortRange`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose.go)

**Dockadvisor test corpus:**  
[`parse/expose_test.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose_test.go)

---

### 4.2 `ExposeInvalidProtocol` — Non-`tcp`/`udp` protocol in `EXPOSE`

**Status in tally:** ❌ Not implemented

**Description:**  
Docker's `EXPOSE` instruction only supports `tcp` and `udp` as valid protocol suffixes.
Dockadvisor flags values like `80/http` or `443/sctp` as errors. Tally does not check the
protocol value.

**Potential implementation:** An extension to `buildkit/ExposeInvalidFormat` or a new
`tally/expose-invalid-protocol` rule that validates the optional `/protocol` suffix against an
allowlist of `{"tcp", "udp"}`.

**Dockadvisor implementation:**  
[`parse/expose.go` — `checkExposeValidProtocol`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose.go)

**Dockadvisor test corpus:**  
[`parse/expose_test.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose_test.go)

---

### 4.3 `FromInvalidImageReference` — Image reference format validation

**Status in tally:** ❌ Not implemented as a linter rule (BuildKit parser errors on truly
invalid references; borderline cases such as unexpected characters may pass silently)

**Description:**  
Dockadvisor performs a lightweight format check on the image reference in `FROM`, rejecting
references that fail a regex. While the BuildKit parser rejects truly malformed references at
parse time, dockadvisor's check may catch edge cases that don't cause a parse failure but are
semantically invalid (e.g. trailing colons, malformed digests).

**Note:** This is a low-priority gap since BuildKit's parser already catches the vast majority
of invalid references. However, a dedicated static check could improve diagnostics.

**Dockadvisor implementation:**  
[`parse/from.go` — `checkImageReferenceFormat`](https://github.com/deckrun/dockadvisor/blob/main/parse/from.go)

**Dockadvisor test corpus:**  
[`parse/from_test.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/from_test.go)

---

## 5. Rules in Tally Not Present in Dockadvisor

Tally has extensive coverage beyond dockadvisor's scope. The following summarises major areas
where tally is significantly more comprehensive.

### 5.1 Hadolint-Compatible Rules

Tally ships 25+ hadolint `DL3xxx`/`DL4xxx` rules. Dockadvisor has no equivalent.

| Tally Rule ID | Description |
|--------------|-------------|
| `hadolint/DL3001` | Useless commands inside containers (ssh, vim, shutdown…) |
| `hadolint/DL3002` | Last USER should not be root |
| `hadolint/DL3003` | Use WORKDIR instead of `cd` in RUN |
| `hadolint/DL3004` | Do not use `sudo` |
| `hadolint/DL3006` | Always tag image version |
| `hadolint/DL3007` | Avoid `:latest` tag |
| `hadolint/DL3010` | Use `ADD` for archives, not plain files |
| `hadolint/DL3011` | Valid EXPOSE ports |
| `hadolint/DL3014` | Use `-y` with `apt-get install` |
| `hadolint/DL3020` | Use `COPY` instead of `ADD` for local files |
| `hadolint/DL3021` | Use `ADD` for only URLs or local archives |
| `hadolint/DL3022–DL3023` | `COPY --from` stage validation |
| `hadolint/DL3026` | Use only trusted base images |
| `hadolint/DL3027–DL3061` | Package manager best practices (apt, apk, yum, dnf, zypper, pip, …) |
| `hadolint/DL4001` | Either use wget or curl but not both |
| `hadolint/DL4005–DL4006` | `SHELL` flags best practices |

### 5.2 Style and Formatting Rules

| Tally Rule ID | Description |
|--------------|-------------|
| `tally/consistent-indentation` | Consistent tab/space indentation across instructions |
| `tally/eol-last` | File must end with a newline |
| `tally/newline-between-instructions` | Blank line between instruction groups |
| `tally/newline-per-chained-call` | Each `&&`-chained command on its own line |
| `tally/no-multi-spaces` | No multiple consecutive spaces in instructions |
| `tally/no-multiple-empty-lines` | No consecutive blank lines |
| `tally/no-trailing-spaces` | No trailing whitespace |
| `tally/max-lines` | Configurable maximum Dockerfile length |
| `tally/epilogue-order` | Canonical order of `CMD`/`ENTRYPOINT`/`HEALTHCHECK`/`EXPOSE` |

### 5.3 Multi-Stage Build Analysis

| Tally Rule ID | Description |
|--------------|-------------|
| `tally/circular-stage-deps` | Detects circular stage dependencies |
| `tally/no-unreachable-stages` | Detects stages never referenced by a final or named stage |
| `tally/copy-from-empty-scratch-stage` | COPY from a scratch stage that has no files |
| `tally/prefer-multi-stage-build` | Suggests splitting build and runtime into stages |
| `tally/platform-mismatch` | Build and runtime stage platform inconsistency |

### 5.4 Security Rules

| Tally Rule ID | Description |
|--------------|-------------|
| `tally/secrets-in-code` | Detects hardcoded secrets in RUN commands (via gitleaks integration) |
| `tally/require-secret-mounts` | Suggests `--mount=type=secret` for credential-like values |
| `tally/stateful-root-runtime` | Final image runs as root with mutable state |
| `tally/final-stage-root` | Final stage user is root (base check) |
| `tally/named-identity-in-passwdless-stage` | `USER` refers to a named user in a scratch-based stage with no passwd file |

### 5.5 Network / Download Best Practices

| Tally Rule ID | Description |
|--------------|-------------|
| `tally/curl-should-follow-redirects` | `curl` downloads without `-L` |
| `tally/prefer-curl-config` | Flags that should be in a config file |
| `tally/prefer-wget-config` | Flags that should be in a `.wgetrc` |

### 5.6 STOPSIGNAL Rules

| Tally Rule ID | Description |
|--------------|-------------|
| `tally/no-ungraceful-stopsignal` | Signal does not allow clean shutdown |
| `tally/prefer-canonical-stopsignal` | Use signal name, not number |
| `tally/prefer-nginx-sigquit` | nginx images should use `SIGQUIT` |
| `tally/prefer-systemd-sigrtmin-plus-3` | systemd images should use `SIGRTMIN+3` |

### 5.7 BuildKit-Specific and Modern Dockerfile Features

| Tally Rule ID | Description |
|--------------|-------------|
| `tally/prefer-add-unpack` | Prefer `ADD --unpack` for archive extraction |
| `tally/prefer-add-git` | Prefer `ADD <git-url>` over `RUN git clone` |
| `tally/prefer-copy-heredoc` | Prefer `COPY <<EOF` heredoc form |
| `tally/prefer-heredoc` | Prefer heredoc for multi-line RUN |
| `tally/prefer-copy-chmod` | Use `COPY --chmod` instead of `RUN chmod` |
| `tally/prefer-package-cache-mounts` | Use `--mount=type=cache` for package managers |
| `tally/prefer-telemetry-opt-out` | Opt-out of telemetry in known tools |
| `tally/prefer-vex-attestation` | Add VEX attestation for CVE suppression |
| `tally/shell-run-in-scratch` | Shell form `RUN` in a scratch-based stage |
| `tally/sort-packages` | Keep package lists sorted |

### 5.8 USER / Privilege Rules

| Tally Rule ID | Description |
|--------------|-------------|
| `tally/copy-after-user-without-chown` | `COPY` after `USER` without `--chown` |
| `tally/user-created-but-never-used` | User created via `adduser`/`useradd` but `USER` never switches to it |
| `tally/user-explicit-group-drops-supplementary-groups` | `USER name:group` drops supplementary groups |
| `tally/world-writable-state-path-workaround` | World-writable path used as workaround for running as non-root |

### 5.9 Ecosystem-Specific Rules

**GPU / CUDA** (`tally/gpu/*`):  
`no-redundant-cuda-install`, `no-container-runtime-in-image`, `no-buildtime-gpu-queries`,
`no-hardcoded-visible-devices`, `prefer-minimal-driver-capabilities`, `prefer-runtime-final-stage`,
`prefer-uv-over-conda`, `cuda-version-mismatch`

**PHP** (`tally/php/*`):  
`composer-no-dev-in-production`, `enable-opcache-in-production`, `no-xdebug-in-final-image`

**Windows** (`tally/windows/*`):  
`no-chown-flag`, `no-run-mounts`, `no-stopsignal`

**PowerShell** (`tally/powershell/*`):  
`error-action-preference`, `prefer-shell-instruction`, `progress-preference`

---

## 6. Summary Table

| Category | Dockadvisor | Tally | Gap Direction |
|----------|-------------|-------|---------------|
| BuildKit captured parse checks | ✅ 22 rules | ✅ Full coverage via `buildkit/*` namespace | Even |
| Syntax validation (build failures) | ✅ ~20 error rules | ✅ Handled by BuildKit parser | Even |
| EXPOSE — port range and protocol | ✅ 2 rules | ❌ Missing | **Dockadvisor ahead** |
| Hadolint-compatible rules | ❌ None | ✅ 25+ rules | **Tally ahead** |
| Multi-stage build analysis | ❌ None | ✅ 5 rules | **Tally ahead** |
| Security (secrets in code) | ⚠️ Partial (ARG/ENV only) | ✅ Full (ARG/ENV + RUN commands) | **Tally ahead** |
| Style / formatting | ❌ None | ✅ 9 rules | **Tally ahead** |
| STOPSIGNAL ecosystem rules | ❌ None | ✅ 4 rules | **Tally ahead** |
| Package manager cache mounts | ❌ None | ✅ Full | **Tally ahead** |
| Ecosystem specialization (GPU/PHP/Windows/PS) | ❌ None | ✅ 14 rules | **Tally ahead** |
| Auto-fix capability | ❌ None | ✅ Many rules fixable | **Tally ahead** |
| LSP server | ❌ None | ✅ `tally lsp --stdio` | **Tally ahead** |
| Quality scoring (0–100) | ✅ Built-in | ❌ None | **Dockadvisor unique** |
| WASM browser execution | ✅ Built-in | ❌ None | **Dockadvisor unique** |

---

## 7. Recommended Actions

Based on the gap analysis, the following rules from dockadvisor are the strongest candidates for
implementation in tally. Only two represent genuine gaps; the remainder are either already
covered or intentionally out of scope.

### Priority 1 — High Value, Low Effort

#### `tally/expose-port-out-of-range`

Validate that the numeric part of an `EXPOSE` port specification is within [0, 65535].

- **Severity**: Warning  
- **Category**: correctness  
- **Implementation pointer**: Extend `internal/rules/buildkit/expose_invalid_format.go` or add
  a new `internal/rules/tally/expose_port_out_of_range.go`. Parse the integer before any `/`
  separator and compare against the valid range.  
- **Test corpus**: [`parse/expose_test.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose_test.go)  
- **Related buildkit rule**: `buildkit/ExposeInvalidFormat` (covers IP/host-port; this covers
  numeric range)

#### `tally/expose-invalid-protocol`

Validate that the protocol suffix in `EXPOSE` is one of `tcp` or `udp` (case-insensitive).

- **Severity**: Warning  
- **Category**: correctness  
- **Implementation pointer**: Extend `internal/rules/buildkit/expose_proto_casing.go` or add a
  new `internal/rules/tally/expose_invalid_protocol.go`. After splitting on `/`, check the
  protocol string against `{"tcp", "udp"}`.  
- **Test corpus**: [`parse/expose_test.go`](https://github.com/deckrun/dockadvisor/blob/main/parse/expose_test.go)  
- **Related buildkit rule**: `buildkit/ExposeProtoCasing` (covers casing; this covers invalid values)

### Priority 2 — Lower Priority / Already Covered

- **`FromInvalidImageReference`**: Low priority. BuildKit's parser handles truly invalid
  references; the remaining dockadvisor check adds minimal value.
- **Syntax validation error rules**: Intentionally delegated to BuildKit's parser. No action
  needed.
- **Quality scoring**: Out of scope for a linter; would require a separate `tally score` command
  if desired.

---

## 8. Appendix — Interesting Findings

### A. Architecture Comparison

| Aspect | Dockadvisor | Tally |
|--------|-------------|-------|
| Dockerfile parser | BuildKit `parser.Parse` (official) | BuildKit `parser.Parse` + `instructions.Parse` (semantic model) |
| Rule model | Per-instruction functions returning `[]Rule` | Interface-based `rules.LintRule` with metadata, sync + async checks |
| Multi-stage awareness | Yes (global checks iterate all `FROM` nodes) | Yes (full semantic model with stage graph, ARG scope, ENV propagation) |
| Fix support | None | Auto-fix engine with safe/suggestion/unsafe levels |
| Config | None | Cascading `.tally.toml` discovery, per-rule severity overrides |
| Output formats | JSON only (library); CLI output on top | text, JSON, SARIF, Markdown, GitHub Actions, LSP diagnostics |
| WASM | Yes (browser-runnable linter) | Shellcheck compiled to WASM; the linter itself is not WASM-compiled |
| Test approach | Unit tests per rule file (`_test.go` in `parse/`) | Unit tests per rule + snapshot integration tests in `internal/integration/` |

### B. Dockadvisor's Quality Scoring System

Dockadvisor computes a 0–100 score using the formula:

```text
score = 100 − (errors × 15 + warnings × 5)
```

Any fatal violation immediately sets the score to 0. This is a differentiating feature with no
equivalent in tally. A potential tally enhancement would be a `tally score` sub-command that
computes a similar quality metric, though this would need careful design to avoid encouraging
"goodhart's law" optimisation.

### C. Dockadvisor's `InvalidDefaultArgInFrom` vs BuildKit's Implementation

Both dockadvisor and tally implement `InvalidDefaultArgInFrom`, but via different mechanisms:

- **Dockadvisor** reimplements the check from scratch using regex-based variable extraction.
- **Tally** delegates to BuildKit's own `linter.RuleInvalidDefaultArgInFrom` which is the
  authoritative implementation.

This highlights a general architectural difference: tally leans on BuildKit as the source of
truth for checks that BuildKit already implements, while dockadvisor re-implements them.
Dockadvisor's reimplementations sometimes produce slightly different results (e.g. its
`UndefinedVar` check does not fully account for the `${VAR:-default}` fallback syntax, and its
regex-based image reference validator may allow or reject cases differently from BuildKit).

### D. Dockadvisor's `SecretsUsedInArgOrEnv` Allowlist vs Tally's

Both tools check for secret-like variable names in `ARG`/`ENV`, but with different approaches:

- **Dockadvisor** uses a deny-list of tokens (`apikey`, `auth`, `credential`, `key`, `password`,
  `passwd`, `pword`, `secret`, `token`) at word boundaries, with an explicit allow-list
  (`public`). This is a lightweight regex approach.
- **Tally** (`buildkit/SecretsUsedInArgOrEnv`) delegates to BuildKit's implementation which uses
  the same underlying logic from the official linter.
- **Tally** additionally has `tally/secrets-in-code` which uses
  [gitleaks](https://github.com/gitleaks/gitleaks) pattern matching in `RUN` command bodies —
  a significantly deeper analysis that dockadvisor does not attempt.

### E. Dockadvisor Does Not Verify Instructions Against BuildKit Semantic Model

Dockadvisor walks the parser AST (`parser.Node`) directly and performs string-based checks.
It does **not** call `instructions.Parse` (BuildKit's semantic layer), so it cannot benefit from
structured command objects, heredoc unpacking, or BuildKit's own build-time validation.

Tally uses `instructions.Parse` to get structured `instructions.Command` objects, enabling
checks like:

- Detecting `COPY --from` references to non-existent stages (stage graph analysis)
- Checking heredoc body content
- Analysing `RUN --mount` flag combinations
- Tracking `ENV`/`ARG` scope across multi-stage builds accurately

### F. Missing Test Files in Dockadvisor

The repository contains a `parse/unrecognized_test.go` file for the `UnrecognizedInstruction`
rule but no corresponding `unrecognized.go` implementation file. The `UnrecognizedInstruction`
rule is emitted inline in `parse.go`'s default switch case, not in its own file. This
discrepancy suggests some planned refactoring.

### G. Dockadvisor WASM Module and Web Interface

Dockadvisor compiles to WebAssembly for browser use. The WASM module exposes a
`parseDockerfile(content: string)` JavaScript function that returns the full rule list and score
synchronously. This enables an interactive playground without a backend. Tally has no equivalent
browser target, though tally's shellcheck integration already uses a WASM-compiled binary.

### H. `RunInvalidMountFlag` — RUN Flag Validation

Dockadvisor explicitly validates that `--mount=type=<x>` uses a known type. Tally relies on
BuildKit's parser for this, which rejects unknown mount types at parse time. The dockadvisor
implementation is effectively redundant given BuildKit's coverage, but it catches the error
earlier in the tool pipeline (before attempting a build).

### I. Version at Analysis Time

Dockadvisor was at an early stage of development when this analysis was performed (first public
release, no git tags). The rule set may evolve. The project's README claims "60+ rules" but the
code analysis found approximately 50 distinct rule codes. This discrepancy may reflect planned
but not-yet-implemented rules.
