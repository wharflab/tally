# tally Rules

Custom rules implemented by tally that go beyond BuildKit's checks.

| Rule | Description | Severity | Category | Default |
|------|-------------|----------|----------|---------|
| [secrets-in-code](./secrets-in-code.md) | Detects hardcoded secrets, API keys, and credentials | Error | Security | Enabled |
| [prefer-vex-attestation](./prefer-vex-attestation.md) | Prefer attaching OpenVEX as an OCI attestation instead of copying VEX JSON into the image | Info | Security | Enabled |
| [max-lines](./max-lines.md) | Enforces maximum number of lines in a Dockerfile | Error | Maintainability | Enabled (50 lines) |
| [no-unreachable-stages](./no-unreachable-stages.md) | Warns about build stages that don't contribute to the final image | Warning | Best Practice | Enabled |
| [prefer-add-unpack](./prefer-add-unpack.md) | Prefer `ADD --unpack` for downloading and extracting remote archives | Info | Performance | Enabled |
| [prefer-copy-heredoc](./prefer-copy-heredoc.md) | Suggests using COPY heredoc for file creation | Style | Style | Off (experimental) |
| [prefer-run-heredoc](./prefer-run-heredoc.md) | Suggests using heredoc syntax for multi-command RUN | Style | Style | Off (experimental) |
| [consistent-indentation](./consistent-indentation.md) | Enforces consistent indentation for build stages | Style | Style | Off (experimental) |
