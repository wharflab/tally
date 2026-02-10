# Tally (VS Code)

This folder contains the VS Code extension for `tally`.

Commands:

- `Tally: Fix all auto-fixable issues` (`tally.applyAllFixes`) — applies safe fixes. Set `tally.fixUnsafe=true` to also apply unsafe fixes.
- `Tally: Configure as default formatter for Dockerfile` (`tally.configureDefaultFormatterForDockerfile`) — writes workspace settings for
  Dockerfile formatting on save.
- `Tally: Restart server` (`tally.restartServer`)

Formatter setup (manual):

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

Development:

- `bun install --save-text-lockfile`
- `bun run typecheck`
- `bun run compile`
- `bun run test:e2e` (launches VS Code via `@vscode/test-electron`)
