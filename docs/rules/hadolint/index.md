# Hadolint Rules

Hadolint's Dockerfile linting rules reimplemented by tally, with auto-fix support and enhanced detection where available.

| Rule | Description | Severity | Auto-fix |
|------|-------------|----------|----------|
| [DL3001](DL3001.md) | Command does not make sense in a container | Info | No |
| [DL3002](DL3002.md) | Last user should not be root | Warning | No |
| [DL3003](DL3003.md) | Use WORKDIR to switch to a directory | Warning | Yes |
| [DL3004](DL3004.md) | Do not use sudo | Error | No |
| [DL3006](DL3006.md) | Always tag the version of an image explicitly | Warning | No |
| [DL3007](DL3007.md) | Using latest is prone to errors | Warning | No |
| [DL3010](DL3010.md) | Use ADD for extracting archives into an image | Info | No |
| [DL3011](DL3011.md) | Valid UNIX ports range from 0 to 65535 | Error | No |
| [DL3014](DL3014.md) | Use the -y switch (apt-get) | Warning | Yes |
| [DL3020](DL3020.md) | Use COPY instead of ADD for files and folders | Error | No |
| [DL3021](DL3021.md) | COPY with more than 2 arguments requires last to end with / | Error | No |
| [DL3022](DL3022.md) | COPY --from should reference a previously defined FROM alias | Warning | No |
| [DL3023](DL3023.md) | COPY --from cannot reference its own FROM alias | Error | No |
| [DL3026](DL3026.md) | Use only an allowed registry in the FROM image | Off | No |
| [DL3027](DL3027.md) | Do not use apt, use apt-get or apt-cache | Warning | Yes |
| [DL3030](DL3030.md) | Use the -y switch (yum) | Warning | Yes |
| [DL3034](DL3034.md) | Non-interactive switch missing from zypper command | Warning | Yes |
| [DL3038](DL3038.md) | Use the -y switch (dnf) | Warning | Yes |
| [DL3043](DL3043.md) | ONBUILD, FROM or MAINTAINER in ONBUILD | Error | No |
| [DL3045](DL3045.md) | COPY to relative destination without WORKDIR | Warning | No |
| [DL3046](DL3046.md) | useradd without -l and high UID | Warning | Yes |
| [DL3047](DL3047.md) | wget without --progress | Info | Yes |
| [DL3057](DL3057.md) | HEALTHCHECK instruction missing | Ignore | No |
| [DL3061](DL3061.md) | Invalid instruction order | Error | No |
| [DL4001](DL4001.md) | Either use Wget or Curl but not both | Warning | No |
| [DL4005](DL4005.md) | Use SHELL to change the default shell | Warning | Yes |
| [DL4006](DL4006.md) | Set SHELL -o pipefail before RUN with pipe | Warning | Yes |

## Superseded rules

The following Hadolint rules are covered by equivalent BuildKit or tally rules with improved diagnostics or auto-fix support:

| Hadolint Rule | Superseded by |
|---------------|---------------|
| [DL3000](DL3000.md) | [buildkit/WorkdirRelativePath](../buildkit/WorkdirRelativePath.md) |
| [DL3012](DL3012.md) | [buildkit/MultipleInstructionsDisallowed](../buildkit/MultipleInstructionsDisallowed.md) |
| [DL3024](DL3024.md) | [buildkit/DuplicateStageName](../buildkit/DuplicateStageName.md) |
| [DL3025](DL3025.md) | [buildkit/JSONArgsRecommended](../buildkit/JSONArgsRecommended.md) |
| [DL3029](DL3029.md) | [buildkit/FromPlatformFlagConstDisallowed](../buildkit/FromPlatformFlagConstDisallowed.md) |
| [DL3044](DL3044.md) | [buildkit/UndefinedVar](../buildkit/UndefinedVar.md) |
| [DL3059](DL3059.md) | [tally/prefer-run-heredoc](../tally/prefer-run-heredoc.md) |
| [DL4000](DL4000.md) | [buildkit/MaintainerDeprecated](../buildkit/MaintainerDeprecated.md) |
| [DL4003](DL4003.md) | [buildkit/MultipleInstructionsDisallowed](../buildkit/MultipleInstructionsDisallowed.md) |
| [DL4004](DL4004.md) | [buildkit/MultipleInstructionsDisallowed](../buildkit/MultipleInstructionsDisallowed.md) |

## Not implemented

Hadolint cache-cleanup rules (DL3009, DL3019, DL3032, DL3036, DL3040, DL3042, DL3060) are intentionally not implemented. Use
[tally/prefer-package-cache-mounts](../tally/prefer-package-cache-mounts.md) instead, which suggests BuildKit cache mounts as a modern alternative.

---

Based on the [Hadolint Wiki](https://github.com/hadolint/hadolint/wiki).
