# Tally for VS Code

Lint, format, and auto-fix **Dockerfiles** and **Containerfiles** in VS Code using
[tally](https://github.com/wharflab/tally): a BuildKit-native linter + formatter with safe auto-fixes (and optional AI-powered fixes).

## What you get

- **Inline diagnostics** powered by BuildKit checks + Hadolint-compatible rules + tally's own modernization rules.
- **Production-grade**: 92% code coverage and 2,900+ Go tests executed in CI.
- **Quick Fixes** and a one-shot **Fix All** command to apply auto-fixable improvements.
- **Formatter support** (format on save) using the same engine as `tally lint --fix`.
- **Config-aware**: respects `.tally.toml` / `tally.toml` discovery in your repo.
- **No daemon**: runs locally without Docker Desktop or a Docker daemon.
- **Zero setup**: Marketplace builds bundle the `tally` binary for your platform (you can also bring your own).

## Quick start

1. Install the extension (`wharflab.tally`).
2. Open a `Dockerfile` or `Containerfile`.
3. Run `Tally: Fix all auto-fixable issues` to apply safe fixes.
4. Optional: run `Tally: Configure as default formatter for Dockerfile` to enable format on save.

## Commands

- `Tally: Fix all auto-fixable issues` (`tally.applyAllFixes`): applies safe fixes. Set `tally.fixUnsafe=true` to also apply unsafe fixes.
- `Tally: Configure as default formatter for Dockerfile` (`tally.configureDefaultFormatterForDockerfile`): writes workspace/user settings for
  Dockerfile formatting on save.
- `Tally: Restart server` (`tally.restartServer`)

## Settings

- `tally.enable`: enable/disable the language server.
- `tally.path`: explicit paths to a `tally` executable (first existing path wins).
- `tally.importStrategy`: where to resolve `tally` from (`fromEnvironment` or `useBundled`).
- `tally.configuration`: inline configuration override (merges with `.tally.toml` / `tally.toml`).
- `tally.configurationPreference`: how to merge editor settings with filesystem config (`editorFirst`, `filesystemFirst`, `editorOnly`).
- `tally.fixUnsafe`: allow "Fix all" to apply unsafe fixes (includes AI AutoFix, if configured).

## Recommended formatter setup (manual)

```jsonc
{
  "[dockerfile]": {
    "editor.defaultFormatter": "wharflab.tally",
    "editor.formatOnSave": true,
    "editor.formatOnSaveMode": "file",
    "editor.codeActionsOnSave": {
      "source.fixAll.tally": "explicit"
    }
  }
}
```

## Learn more

- Rules reference: <https://github.com/wharflab/tally/blob/main/RULES.md>
- Configuration guide: <https://github.com/wharflab/tally/blob/main/docs/guide/configuration.md>
- AI AutoFix (ACP): <https://github.com/wharflab/tally/blob/main/docs/guide/ai-autofix-acp.md>
