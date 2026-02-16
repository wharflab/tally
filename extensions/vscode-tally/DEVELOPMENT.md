# VS Code Extension Development

This folder contains the VS Code extension that talks to `tally lsp --stdio`.

## Commands

- `bun install --frozen-lockfile`
- `bun run typecheck`
- `bun run compile`
- `bun run test:e2e` (launches VS Code via `@vscode/test-electron`)

## Packaging

Build the extension bundle, then package a `.vsix`:

```bash
bun run compile
bunx @vscode/vsce package --no-dependencies
```

## Bundled `tally` binary

Release builds copy a platform-specific `tally` binary into:

`extensions/vscode-tally/bundled/bin/<platform>/<arch>/tally[.exe]`

The extension can be configured to use that binary via `tally.importStrategy = "useBundled"`.
