# Lessons from TypeScript-Go VS Code Extension (TypeScript Native Preview)

Research date: 2026-02-24  
Status: Draft (actionable backlog)

This doc captures practical patterns from the TypeScript-Go VS Code extension implementation at:

- `https://github.com/microsoft/typescript-go` (extension source lives under `_extension/`)

Our goal is to apply the useful parts to `tally`’s VS Code extension (`extensions/vscode-tally/`), which is also a Go-binary-backed LSP server.

---

## Executive summary (what we should steal)

The TypeScript Native Preview extension is intentionally small, but it uses a few VS Code + `vscode-languageclient` features that we should adopt:

- **Split Output vs. LSP trace channels** (`outputChannel` + `traceOutputChannel`) for debuggability.
- **Leverage `vscode-languageclient`’s built-in watchdog** (auto-restart on crashes) and wire it into our UX.
- **Expose standard `trace.server` configuration** (`<clientId>.trace.server`) so users can flip protocol tracing without custom code.
- **Use diagnostic pull options thoughtfully** (onSave / onTabs + a `match` implementation when only a URI is known).
- **Use status UI primitives** (`StatusBarItem` + `LanguageStatusItem`) to show server state + version/source.

The key “watchdog pattern” is not VS Code core; it’s `vscode-languageclient`’s default `ErrorHandler` behavior.

---

## What TypeScript-Go’s extension actually does (relevant bits)

### LSP client bootstrapping

Implementation:

- `_extension/src/extension.ts` wires activation + config changes.
- `_extension/src/client.ts` constructs `LanguageClient` with `serverOptions` pointing at the `tsgo` binary (`--lsp` + stdio).

Notable details:

- Uses **two log channels**:
  - `vscode.window.createOutputChannel("typescript-native-preview", { log: true })`
  - `vscode.window.createOutputChannel("typescript-native-preview (LSP)", { log: true })`
- Passes both into `LanguageClientOptions`:
  - `outputChannel`
  - `traceOutputChannel`
- Passes env overrides into the server process (e.g. validated `GOMEMLIMIT`).

### Diagnostic pull configuration

TypeScript-Go sets `diagnosticPullOptions` with:

- `onChange: true`
- `onSave: true`
- `onTabs: true`
- custom `match(documentSelector, uri)` logic to decide whether a URI likely belongs to JS/TS when no `TextDocument` exists.

This is important because `onTabs` will otherwise be ineffective when the document selector includes `language` (no language known for URI-only).

### Status UI

They use both:

- `StatusBarItem` (simple always-visible “tsgo” badge with a command menu)
- `LanguageStatusItem` (language-scoped “version” display)

---

## Watchdog / restart behavior (the “VS Code relaunches the LSP” observation)

### What’s actually doing the restart

The auto-restart behavior is built into `vscode-languageclient`, not VS Code itself.

TypeScript-Go does not implement a custom watchdog in its extension; it relies on the default `LanguageClient` error handler.
`vscode-languageclient`’s default handler restarts the server process when the connection closes unexpectedly.

### Default restart policy (important specifics)

`vscode-languageclient` (observed on `v9.0.1`, which is what `extensions/vscode-tally/package.json` currently tracks via `^9.0.1`) implements a
`DefaultErrorHandler` with:

- A **restart counter window** (timestamps of recent restarts).
- A default `maxRestartCount` of **4** (configurable via `connectionOptions.maxRestartCount`).
- If the server crashes **5 times within 3 minutes**, it stops restarting and reports a message similar to:
  - “The \<name\> server crashed 5 times in the last 3 minutes. The server will not be restarted…”

This is the “watchdog pattern” we should explicitly lean on and integrate with `tally` UX.
When upgrading `vscode-languageclient`, verify these defaults (`DefaultErrorHandler` and `connectionOptions.maxRestartCount`) still match.

### What we should do in `tally` (beyond relying on defaults)

We already use `vscode-languageclient`, so we get the watchdog “for free”. The missing pieces are UX + observability:

1. **Wire `LanguageClient.onDidChangeState` into context keys and logs**
   - Keep `tally.serverRunning` accurate (Running vs Stopped), including crash-loop terminal state.
2. **Surface “crash loop” shutdown to the user**
   - When `vscode-languageclient` stops restarting (DoNotRestart), show a toast with next steps:
     - open output
     - switch to bundled binary
     - set trace to verbose
3. **Optionally tune restart policy**
   - Keep default unless we have evidence it’s too aggressive or too timid.
   - If we do tune, prefer exposing a setting (e.g. `tally.lsp.maxRestartCount`) rather than hardcoding.

---

## Concrete recommendations for `extensions/vscode-tally/`

### 1) Add a dedicated LSP trace output channel + `tally.trace.server` setting

Why:

- It dramatically reduces “it crashed” guesswork.
- It matches the standard `vscode-languageclient` convention.

What to do:

- In `extensions/vscode-tally/src/extension.ts`, create two channels (both `{ log: true }`).
- In `extensions/vscode-tally/src/lsp/client.ts`, pass `traceOutputChannel`.
- Launch the language server via the explicit stdio contract: `tally lsp --stdio`.
  - `stdin`: JSON-RPC/LSP request stream
  - `stdout`: JSON-RPC/LSP response + notification stream
  - `stderr`: human-readable logs only (not protocol payloads)
- In `extensions/vscode-tally/package.json`, add:
  - `tally.trace.server: off | messages | verbose` (same schema as TSGo), bound to this `tally lsp --stdio` invocation.

Notes:

- `vscode-languageclient` already knows how to interpret `<clientId>.trace.server`; we shouldn’t invent a separate setting.

### 2) Make watchdog behavior visible in the UI

Why:

- Auto-restart is great, but invisible restarts feel like flaky diagnostics or “random” missing code actions.

What to do:

- Listen to `client.onDidChangeState` and:
  - update a `tally.serverRunning` context key
  - update status bar text (e.g. “tally” vs “tally (restarting…)”)
  - log state transitions to the output channel

### 3) Add `LanguageStatusItem` for resolved server version + source

Why:

- We already resolve binaries from many places; users need to know which one was picked (bundled vs npm vs venv vs PATH).

What to do:

- Add a language status item for `dockerfile` documents that shows:
  - `tally` version (from `tally version --json`)
  - and optionally the source (e.g. `bundled`, `workspaceNpm`, `workspaceVenv`, …)

### 4) Improve diagnostic pull behavior (optional, but likely worth it)

Why:

- Pull diagnostics can be more reliable than push, especially under rapid edits and multi-tab workflows.

What to do:

- Set `diagnosticPullOptions.onSave = true`.
- Consider `diagnosticPullOptions.onTabs = true`, but only if we can implement a solid `match()` for URI-only:
  - For `dockerfile`, this may require matching known filenames (`Dockerfile`, `Containerfile`, `Dockerfile.*`, `Containerfile.*`) and user
    `files.associations`.
  - If we can’t match accurately, skip `onTabs` to avoid wasting cycles.

### 5) Add “debug knobs” for Go server runtime (optional)

TSGo exposes:

- `goMemLimit` → sets `GOMEMLIMIT`
- `pprofDir` → passes `--pprofDir <dir>`

For `tally`, we can consider:

- `tally.goMemLimit` (sets `GOMEMLIMIT`)
- `tally.pprofDir` (if/when the server supports it)
- `tally.env` (key/value env overrides, with an allowlist)

---

## Proposed implementation plan (follow-up work)

1. Introduce output + trace channels, plus `tally.trace.server` setting.
2. Expose `tally.serverRunning` context key driven by `onDidChangeState`.
3. Create status bar item + quick-pick command menu (restart, open logs, report issue).
4. Show language status item for server version/source.
5. Assess `diagnosticPullOptions` changes (onSave + optional onTabs with `match`).
6. Add a “crash loop” toast flow when restarts stop (DoNotRestart).
