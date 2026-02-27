# ShellCheck Rules

ShellCheck-derived diagnostics in tally.

This namespace currently includes rules that are implemented natively in Go and use tally's fix and reporting infrastructure directly.

| Rule | Description | Severity | Auto-fix |
|------|-------------|----------|----------|
| [SC1040](SC1040.md) | `<<-` heredoc terminators may only be indented with tabs | Error | Yes (`--fix`) |

## Configuration

Enable only SC1040:

```toml
[rules]
include = ["shellcheck/SC1040"]
```

Disable SC1040 while keeping other ShellCheck rules enabled:

```toml
[rules]
exclude = ["shellcheck/SC1040"]
```
