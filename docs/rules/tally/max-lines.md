# tally/max-lines

Enforces maximum number of lines in a Dockerfile.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Maintainability |
| Default | Enabled (50 lines) |

## Description

Limits Dockerfile size to encourage modular builds. Enabled by default with a 50-line limit (P90 of analyzed public Dockerfiles).

Large Dockerfiles are harder to maintain, review, and debug. This rule encourages:

- Breaking complex builds into multi-stage patterns
- Using base images for common dependencies
- Keeping build logic modular

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max` | integer | 50 | Maximum lines allowed |
| `skip-blank-lines` | boolean | true | Exclude blank lines from count |
| `skip-comments` | boolean | true | Exclude comment lines from count |

## Examples

### Bad

```dockerfile
# A 100+ line Dockerfile with everything in one file
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y \
    build-essential \
    curl \
    # ... 50 more packages ...
    vim
# ... 80 more lines of setup ...
```

### Good

```dockerfile
# Base image with common dependencies
FROM myorg/base:1.0

# Application-specific setup only
COPY requirements.txt .
RUN pip install -r requirements.txt

COPY . .
CMD ["python", "app.py"]
```

## Configuration

```toml
[rules.tally.max-lines]
severity = "warning"
max = 100
skip-blank-lines = true
skip-comments = true
```

## CLI Flags

```bash
tally lint --max-lines 100 --skip-blank-lines --skip-comments Dockerfile
```
