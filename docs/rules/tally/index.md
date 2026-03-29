# tally Rules

Custom rules implemented by tally that go beyond BuildKit's checks.

| Rule | Description | Severity | Category | Default |
|------|-------------|----------|----------|---------|
| [require-stages](./require-stages.md) | Dockerfile has no stages to build | Error | Correctness | Enabled |
| [unknown-instruction](./unknown-instruction.md) | Detects misspelled or invalid Dockerfile instruction keywords | Error | Correctness | Enabled |
| [syntax-directive-typo](./syntax-directive-typo.md) | Detects typos in `# syntax=` parser directives | Error | Correctness | Enabled |
| [secrets-in-code](./secrets-in-code.md) | Detects hardcoded secrets, API keys, and credentials | Error | Security | Enabled |
| [prefer-vex-attestation](./prefer-vex-attestation.md) | Prefer attaching OpenVEX as an OCI attestation instead of copying VEX JSON into the image | Info | Security | Enabled |
| [require-secret-mounts](./require-secret-mounts.md) | Enforces --mount=type=secret for commands accessing private registries | Warning | Security | Off (requires config) |
| [stateful-root-runtime](./stateful-root-runtime.md) | Final stage runs as root and signals mutable/persistent state | Warning | Security | Enabled |
| [user-created-but-never-used](./user-created-but-never-used.md) | Final stage creates a user but never switches to it | Warning | Security | Enabled |
| [copy-after-user-without-chown](./copy-after-user-without-chown.md) | COPY/ADD without --chown after non-root USER creates root-owned files | Warning | Correctness | Enabled |
| [max-lines](./max-lines.md) | Enforces maximum number of lines in a Dockerfile | Error | Maintainability | Enabled (50 lines) |
| [no-unreachable-stages](./no-unreachable-stages.md) | Warns about build stages that don't contribute to the final image | Warning | Best Practice | Enabled |
| [shell-run-in-scratch](./shell-run-in-scratch.md) | Detects shell-form RUN in scratch stages where no shell exists | Warning | Correctness | Enabled |
| [no-ungraceful-stopsignal](./no-ungraceful-stopsignal.md) | STOPSIGNAL should not use signals that prevent graceful shutdown | Warning | Correctness | Enabled |
| [prefer-canonical-stopsignal](./prefer-canonical-stopsignal.md) | STOPSIGNAL should use canonical signal names | Info | Style | Enabled |
| [prefer-systemd-sigrtmin-plus-3](./prefer-systemd-sigrtmin-plus-3.md) | systemd/init containers should use STOPSIGNAL SIGRTMIN+3 | Warning | Correctness | Enabled |
| [invalid-onbuild-trigger](./invalid-onbuild-trigger.md) | ONBUILD trigger instruction is not a valid Dockerfile instruction | Error | Correctness | Enabled |
| [circular-stage-deps](./circular-stage-deps.md) | Detects circular dependencies between build stages | Error | Correctness | Enabled |
| [copy-from-empty-scratch-stage](./copy-from-empty-scratch-stage.md) | Detects COPY --from referencing a scratch stage with no file-producing instructions | Error | Correctness | Enabled |
| [invalid-json-form](./invalid-json-form.md) | Arguments appear to use JSON exec-form but contain invalid JSON | Error | Correctness | Enabled |
| [platform-mismatch](./platform-mismatch.md) | Explicit `--platform` on FROM does not match what the registry provides | Error | Correctness | Enabled |
| [prefer-curl-config](./prefer-curl-config.md) | Stages using curl should include a retry config | Info | Correctness | Enabled |
| [prefer-telemetry-opt-out](./prefer-telemetry-opt-out.md) | Stages using telemetry-enabled tools should set the vendor-documented opt-out environment variables | Info | Privacy | Enabled |
| [prefer-add-unpack](./prefer-add-unpack.md) | Prefer `ADD --unpack` for downloading and extracting remote archives | Info | Performance | Enabled |
| [prefer-multi-stage-build](./prefer-multi-stage-build.md) | Suggests converting single-stage builds into multi-stage builds to reduce final image size | Info | Performance | Off (experimental) |
| [prefer-copy-chmod](./prefer-copy-chmod.md) | Prefer `COPY --chmod` over separate `COPY` + `RUN chmod` | Info | Style | Enabled |
| [prefer-copy-heredoc](./prefer-copy-heredoc.md) | Suggests using COPY heredoc for file creation | Info | Performance | Enabled |
| [prefer-package-cache-mounts](./prefer-package-cache-mounts.md) | Suggests cache mounts for package install/build commands and removes cache cleanup | Info | Performance | Off (experimental) |
| [php/composer-no-dev-in-production](./php/composer-no-dev-in-production.md) | Production Composer install commands should include --no-dev | Warning | Security | Enabled |
| [powershell/prefer-shell-instruction](./powershell/prefer-shell-instruction.md) | Prefer a `SHELL` instruction instead of repeating `pwsh` or `powershell -Command` in `RUN` | Style | Style | Enabled (experimental) |
| [gpu/no-container-runtime-in-image](./gpu/no-container-runtime-in-image.md) | NVIDIA container runtime packages belong on the host, not inside the image | Warning | Correctness | Enabled |
| [gpu/no-hardcoded-visible-devices](./gpu/no-hardcoded-visible-devices.md) | GPU visibility is deployment policy; hardcoding it in the image reduces portability | Warning | Correctness | Enabled |
| [gpu/no-redundant-cuda-install](./gpu/no-redundant-cuda-install.md) | CUDA packages are already provided by the nvidia/cuda base image | Warning | Correctness | Enabled |
| [gpu/prefer-minimal-driver-capabilities](./gpu/prefer-minimal-driver-capabilities.md) | NVIDIA_DRIVER_CAPABILITIES=all exposes more driver surface than most workloads need | Info | Correctness | Enabled |
| [gpu/prefer-runtime-final-stage](./gpu/prefer-runtime-final-stage.md) | Final stage uses an NVIDIA devel image without clear build-time needs | Warning | Best Practices | Enabled |
| [windows/no-run-mounts](./windows/no-run-mounts.md) | `RUN --mount` flags are not supported on Windows containers | Error | Correctness | Enabled |
| [windows/no-stopsignal](./windows/no-stopsignal.md) | `STOPSIGNAL` has no effect on Windows containers | Warning | Correctness | Enabled |
| [prefer-run-heredoc](./prefer-run-heredoc.md) | Suggests using heredoc syntax for multi-command RUN | Style | Style | Off (experimental) |
| [consistent-indentation](./consistent-indentation.md) | Enforces consistent indentation for build stages | Style | Style | Off (experimental) |
| [newline-between-instructions](./newline-between-instructions.md) | Controls blank lines between Dockerfile instructions | Style | Style | Enabled (grouped) |
| [no-multi-spaces](./no-multi-spaces.md) | Disallows multiple consecutive spaces within instructions | Style | Style | Enabled |
| [no-multiple-empty-lines](./no-multiple-empty-lines.md) | Disallows multiple consecutive empty lines | Style | Style | Enabled |
| [no-trailing-spaces](./no-trailing-spaces.md) | Disallows trailing whitespace at the end of lines | Style | Style | Enabled |
| [eol-last](./eol-last.md) | Enforces a newline at the end of non-empty files | Style | Style | Enabled |
| [epilogue-order](./epilogue-order.md) | Enforces canonical order for epilogue instructions (STOPSIGNAL, HEALTHCHECK, ENTRYPOINT, CMD) | Style | Style | Enabled |
| [sort-packages](./sort-packages.md) | Package lists in install commands should be sorted alphabetically | Style | Style | Enabled |
| [newline-per-chained-call](./newline-per-chained-call.md) | Each chained element within an instruction should be on its own line | Style | Style | Enabled |
