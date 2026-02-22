# Exit Codes

tally uses distinct exit codes so scripts and CI pipelines can react to different outcomes.

| Code | Name | Meaning |
|------|------|---------|
| `0` | Success | No violations found (or all violations are below `--fail-level`) |
| `1` | Violations | One or more violations at or above the configured `--fail-level` |
| `2` | Error | Configuration, parse, or I/O error (e.g. invalid config file, permission denied) |
| `3` | No files | No Dockerfiles to lint (missing file, empty glob, empty directory) |

## Examples

```bash
# Succeed only when there are zero issues
tally lint .
echo $?  # 0 = clean, 1 = violations

# Fail CI only on errors (ignore warnings)
tally lint --fail-level error .

# Detect "nothing to lint" separately from real errors
tally lint Dockerfile.prod
status=$?
if [ "$status" -eq 3 ]; then
  echo "No Dockerfiles found — skipping lint"
elif [ "$status" -ne 0 ]; then
  exit "$status"
fi
```

## CI/CD tips

- Use `--fail-level` to control which severities cause exit code 1.
- Exit code 3 lets you distinguish "the path was wrong" from "the config is broken" (code 2).
- See [CI/CD](./ci-cd.md) for pipeline examples.
