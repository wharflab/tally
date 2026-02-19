# tally/newline-between-instructions

Controls blank lines between Dockerfile instructions.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Enabled (grouped) |
| Auto-fix | Yes (safe) |

## Description

Enforces consistent blank-line spacing between Dockerfile instructions. Three modes are available:

- **`grouped`** (default): Same instruction types are grouped together with no blank lines between them; different types are separated by exactly one
  blank line.
- **`always`**: Every instruction is followed by at least one blank line.
- **`never`**: All blank lines between instructions are removed.

### Grouped mode (default)

```dockerfile
FROM alpine:3.20

RUN apk add --no-cache curl

ENV FOO=bar
ENV BAZ=qux

COPY . /app
```

Same-type instructions (both `ENV`) have no blank line between them. Different types (`FROM` to `RUN`, `RUN` to `ENV`, `ENV` to `COPY`) are separated
by a blank line.

### Always mode

```dockerfile
FROM alpine:3.20

RUN apk add --no-cache curl

ENV FOO=bar

ENV BAZ=qux

COPY . /app
```

### Never mode

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache curl
ENV FOO=bar
ENV BAZ=qux
COPY . /app
```

## Examples

### Bad (grouped mode, missing blank line)

```dockerfile
FROM alpine:3.20
RUN echo hello
```

### Good (grouped mode)

```dockerfile
FROM alpine:3.20

RUN echo hello
```

### Bad (grouped mode, unwanted blank between same types)

```dockerfile
ENV FOO=bar

ENV BAZ=qux
```

### Good (grouped mode, same types adjacent)

```dockerfile
ENV FOO=bar
ENV BAZ=qux
```

## Configuration

Default (grouped mode, no config needed):

```toml
# Grouped mode is the default when the rule is enabled
```

Always mode:

```toml
[rules.tally.newline-between-instructions]
mode = "always"
```

Never mode:

```toml
[rules.tally.newline-between-instructions]
mode = "never"
```

String shorthand:

```toml
[rules.tally]
newline-between-instructions = "always"
```

## Auto-fix

This rule provides safe auto-fixes:

- **Insert blank line**: Adds a single blank line between instructions that need separation.
- **Remove blank lines**: Removes excess blank lines between instructions that should be adjacent.

The auto-fix uses an async resolver that re-parses the file after other fixes have been applied,
ensuring correct line positions even when earlier fixes (such as heredoc conversions) change the
file structure.

```bash
tally lint --fix Dockerfile
```

## Interaction with buildkit/InvalidDefinitionDescription

When both rules are enabled, `buildkit/InvalidDefinitionDescription` may insert blank lines
between comments and instructions (to indicate a comment is not a description). These blank
lines are **not** affected by `newline-between-instructions` because the newline rule only
measures gaps between instruction nodes, not between comments and their associated instructions.

For example, with `never` mode and `InvalidDefinitionDescription` enabled:

```dockerfile
# This comment doesn't describe the ARG
ARG foo=bar
FROM scratch AS base
RUN echo hello
```

After auto-fix, `InvalidDefinitionDescription` inserts a blank line between the comment and
`ARG`, while `newline-between-instructions` keeps instructions adjacent:

```dockerfile
# This comment doesn't describe the ARG

ARG foo=bar
FROM scratch AS base
RUN echo hello
```
