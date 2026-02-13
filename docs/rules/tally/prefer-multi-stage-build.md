# `tally/prefer-multi-stage-build`

Suggests converting Dockerfiles that **build artifacts in a single stage** into a **multi-stage build** to reduce the final image size and avoid
shipping build tooling in the runtime image.

This rule is informational by default and is intended to be used together with the **AI AutoFix** flow.

## What it looks for (heuristics)

The rule triggers only when the Dockerfile has **exactly one `FROM` stage** and the stage contains at least one of:

- A package-manager install likely related to building (e.g. `apt-get install build-essential`, `apk add gcc`)
- A build step (e.g. `go build`, `cargo build`, `npm run build`, `dotnet publish`)
- A download+install pattern (e.g. `curl ... | tar ...`, `wget ... | sh`)

## Auto-fix

When triggered, the rule emits an **unsafe**, async `SuggestedFix` that requires:

- `--fix --fix-unsafe`
- A configured ACP-capable agent in the config file (see the top-level `[ai]` section)

The resolver returns a single edit that replaces the entire Dockerfile content.

## Configuration

```toml
[rules.tally.prefer-multi-stage-build]
min-score = 4
fix = "explicit"
```

- `min-score` (default: `4`): minimum heuristic score required to trigger.
- `fix = "explicit"` is recommended to avoid accidentally running AI fixes when using `--fix-unsafe` broadly.
