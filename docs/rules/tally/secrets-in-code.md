# tally/secrets-in-code

Detects hardcoded secrets, API keys, and credentials using [gitleaks](https://github.com/gitleaks/gitleaks) patterns.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Security |
| Default | Enabled |

## Description

Scans Dockerfile content for actual secret values (not just variable names):

- RUN commands and heredocs
- COPY/ADD heredocs
- ENV values
- ARG default values
- LABEL values

Uses gitleaks' curated database of 222+ secret patterns including AWS keys, GitHub tokens, private keys, and more.

## Complements BuildKit

**Complements `buildkit/SecretsUsedInArgOrEnv`**: BuildKit's rule checks variable *names* (e.g., `GITHUB_TOKEN`), while this rule detects actual
secret *values*.

## Examples

### Bad

```dockerfile
# Hardcoded AWS credentials
ENV AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
ENV AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

# Hardcoded API token in RUN
RUN curl -H "Authorization: Bearer ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" https://api.github.com/user
```

### Good

```dockerfile
# Use build secrets
RUN --mount=type=secret,id=aws_key \
    AWS_ACCESS_KEY_ID=$(cat /run/secrets/aws_key) \
    aws s3 cp ...

# Or use ARG without default value (passed at build time)
ARG GITHUB_TOKEN
RUN curl -H "Authorization: Bearer $GITHUB_TOKEN" https://api.github.com/user
```

## Configuration

```toml
[rules.tally.secrets-in-code]
severity = "error"  # Options: "off", "error", "warning", "info", "style"
```
