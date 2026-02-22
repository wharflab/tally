# BuildKit Rules

Docker BuildKit's official Dockerfile checks, reimplemented by tally with
auto-fix support where available.

## Style

| Rule | Description | Severity | Auto-fix |
|------|-------------|----------|----------|
| [StageNameCasing](StageNameCasing.md) | Stage names should be lowercase | Warning | Yes (`--fix`) |
| [FromAsCasing](FromAsCasing.md) | `AS` keyword in FROM should use consistent casing | Warning | Yes (`--fix`) |
| [ConsistentInstructionCasing](ConsistentInstructionCasing.md) | Instructions should be in consistent casing | Warning | Yes (`--fix`) |
| [LegacyKeyValueFormat](LegacyKeyValueFormat.md) | Legacy key/value format with whitespace separator should not be used | Warning | Yes (`--fix`) |
| [ExposeProtoCasing](ExposeProtoCasing.md) | Protocol in EXPOSE should be lowercase | Warning | Yes (`--fix`) |
| [InvalidDefinitionDescription](InvalidDefinitionDescription.md) | Comment-based description of an ARG/FROM should follow proper format | Info | Yes (`--fix`) |

## Correctness

| Rule | Description | Severity | Auto-fix |
|------|-------------|----------|----------|
| [NoEmptyContinuation](NoEmptyContinuation.md) | Empty continuation lines are deprecated | Error | Yes (`--fix`) |
| [DuplicateStageName](DuplicateStageName.md) | Duplicate stage names are not allowed | Error | -- |
| [ReservedStageName](ReservedStageName.md) | Reserved words should not be used as stage names | Error | -- |
| [UndefinedArgInFrom](UndefinedArgInFrom.md) | Undefined ARG used in FROM | Warning | -- |
| [UndefinedVar](UndefinedVar.md) | Usage of undefined variable | Warning | -- |
| [InvalidDefaultArgInFrom](InvalidDefaultArgInFrom.md) | Default value of ARG used in FROM is not valid | Error | -- |
| [InvalidBaseImagePlatform](InvalidBaseImagePlatform.md) | Base image platform does not match expected target platform | Error | -- |
| [ExposeInvalidFormat](ExposeInvalidFormat.md) | EXPOSE should not define IP address or host-port mapping | Warning | -- |
| [CopyIgnoredFile](CopyIgnoredFile.md) | Attempting to COPY file that is excluded by .dockerignore | Warning | -- |

## Best Practice

| Rule | Description | Severity | Auto-fix |
|------|-------------|----------|----------|
| [JSONArgsRecommended](JSONArgsRecommended.md) | JSON arguments recommended for ENTRYPOINT/CMD | Info | Yes (`--fix`) |
| [MaintainerDeprecated](MaintainerDeprecated.md) | MAINTAINER instruction is deprecated in favor of using label | Warning | Yes (`--fix`) |
| [WorkdirRelativePath](WorkdirRelativePath.md) | Relative workdir can have unexpected results if the base image changes | Warning | -- |
| [MultipleInstructionsDisallowed](MultipleInstructionsDisallowed.md) | Multiple CMD/ENTRYPOINT/HEALTHCHECK in same stage; only last is used | Warning | Yes (`--fix`) |
| [RedundantTargetPlatform](RedundantTargetPlatform.md) | Setting platform to `$TARGETPLATFORM` in FROM is redundant | Info | -- |
| [FromPlatformFlagConstDisallowed](FromPlatformFlagConstDisallowed.md) | FROM `--platform` flag should not use a constant value | Warning | -- |

## Security

| Rule | Description | Severity | Auto-fix |
|------|-------------|----------|----------|
| [SecretsUsedInArgOrEnv](SecretsUsedInArgOrEnv.md) | Sensitive data should not be used in ARG or ENV | Warning | -- |

---

These rules are based on Docker's official
[build checks](https://docs.docker.com/reference/build-checks/). tally
reimplements them for offline use and adds auto-fix capabilities.
