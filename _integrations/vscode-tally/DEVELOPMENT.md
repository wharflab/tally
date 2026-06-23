# VS Code Extension Development

This folder contains the VS Code extension that talks to `tally lsp --stdio`.

## Commands

- `bun install --frozen-lockfile`
- `bun run typecheck`
- `bun run compile`
- `bun run test:e2e` (launches VS Code via `@vscode/test-electron`)
- `bun run test:e2e:ui` (drives rendered VS Code via code-server + Playwright)

## End-to-end tests

There are two complementary e2e layers:

1. **Extension-host smoke test** (`test/`, `bun run test:e2e`) — launches desktop
   VS Code with `@vscode/test-electron` and asserts the programmatic LSP contract
   (exact diagnostic counts, snapshot-equal formatting, `tally.applyAllFixes`
   output) from *inside* the extension host.
2. **UI tests** (`tests-ui/`, `bun run test:e2e:ui`) — install the packaged
   `.vsix` into a headless [code-server](https://github.com/coder/code-server)
   and drive the *rendered* workbench in headless Chromium via
   [`@playwright/test`](https://playwright.dev). These prove the behaviour
   actually surfaces in the UI (Problems panel, Format Document, the quick-fix
   menu, the status bar).

### Running the UI tests locally

```bash
# code-server requires Node 22 (its postinstall hard-checks the node major).
# If your default node differs, select 22 (e.g. via mise/nvm) for the install.
npm_config_user_agent="npm/10.0.0 node/v22.0.0" bun install
bunx playwright install chromium
bun run test:e2e:ui            # packages the vsix, then runs Playwright
bun run test:e2e:ui:headed     # watch it run in a real browser window
bun run test:e2e:ui:debug      # Playwright UI mode
```

The `setup` Playwright project builds a `tally` LSP binary into `.test_setup/`
and installs the freshly packaged `.vsix`; the `cleanup` project removes
`.test_setup/` afterwards. Pass `VSIX_PATH=<rel>` or `TALLY_BIN=<abs>` to
override either artifact.

## Packaging

Build the extension bundle, then package a `.vsix`:

```bash
bun run compile
bunx @vscode/vsce package --no-dependencies
```

## Bundled `tally` binary

Release builds copy a platform-specific `tally` binary into:

`_integrations/vscode-tally/bundled/bin/<platform>/<arch>/tally[.exe]`

The extension can be configured to use that binary via `tally.importStrategy = "useBundled"`.
