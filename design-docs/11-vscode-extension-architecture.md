# VSCode Extension Architecture for `tally` (LSP-First)

Research date: 2026-02-06
Status: Draft for implementation

## Executive summary

Yes, both researched extensions (`ruff-vscode` and `oxc-vscode`) are LSP-based in VS Code.

For `tally`, we should ship an LSP-first extension in this monorepo with:

- TypeScript VS Code client (`vscode-languageclient`)
- Go LSP server integrated into `tally` binary (`tally lsp --stdio`)
- Bun-first extension toolchain (no npm/pnpm workflow management)
- Hybrid binary strategy:
  - Prefer local project binaries (npm + Python venv) for parity with project tooling
  - Fall back to bundled extension binary for zero-setup UX
- Code-actions-first fix workflow (`quickfix` + `source.fixAll.tally`) using `tally`'s existing `SuggestedFix` model
- Config schema validation for `.tally.toml` / `tally.toml` via generated JSON Schema
- Release from the same `.github/workflows/release.yml`, including platform-specific VSIX artifacts

---

## 1. Direct answers to the research questions

### 1.1 How is native binary delivered?

**Ruff (`astral-sh/ruff-vscode`)**

- Bundles a platform-specific native `ruff` binary inside the extension (`bundled/libs/bin/ruff` / `ruff.exe`).
- Release workflow builds target-specific VSIX files and installs platform deps before packaging.
- Also supports non-bundled execution by discovering local/environment binaries.

**Oxc (`oxc-project/oxc-vscode`)**

- Does not bundle binaries in VSIX.
- Resolves binaries from project `node_modules`, global node_modules roots, or user-provided path.
- `.vscodeignore` keeps VSIX very small (mostly compiled extension JS + metadata).

**Recommendation for `tally`**

- Use a **hybrid model**:
  - Local-first binary resolution (npm + Python env)
  - Bundled binary fallback in VSIX for reliability and first-run success
- This combines Oxc’s project-local behavior with Ruff’s “works out of the box” behavior.

### 1.2 How is local project binary discovered?

**Ruff**

- Resolution order (native path):
  1. `ruff.path`
  2. `importStrategy = useBundled` -> bundled binary
  3. Python helper script with selected interpreter (`find_ruff_binary_path.py`)
  4. `PATH`
  5. bundled fallback

**Oxc**

- Resolution order:
  1. user setting path
  2. project `node_modules`
  3. global node_modules roots (npm/pnpm/bun)
- Has path safety checks, especially around Windows `shell: true` execution.

**`tally` requirement (npm + PyPI)**

Implement both discovery schemas:

1. `tally.path` (explicit path list)
2. npm project binaries:
   - `<workspace>/node_modules/.bin/tally` (`.cmd` on Windows)
   - fallback: resolve `tally-cli` entry script if needed
3. Python virtual env binaries:
   - `<workspace>/.venv/bin/tally`
   - `<workspace>/venv/bin/tally`
   - Windows: `<workspace>/.venv/Scripts/tally.exe`, `<workspace>/venv/Scripts/tally.exe`
4. Interpreter-assisted lookup (optional), similar to Ruff:
   - run a tiny helper with configured interpreter to locate installed `tally`
5. `PATH`
6. Bundled extension binary

### 1.3 How is fix-on-save implemented? Tricky parts?

**Ruff**

- Uses standard code actions on save (`source.fixAll`, `source.organizeImports`) and scoped variants (`source.fixAll.ruff`).
- Documents notebook-specific behavior and warns about misconfigured notebook code-action keys.

**Oxc**

- Supports `source.fixAll.oxc` and tests it in integration tests.
- Uses LSP command execution for explicit “fix all” command.
- Includes targeted workaround logic for known diagnostic quirks.

**Recommendation for `tally`**

- Provide code actions with kinds:
  - `quickfix`
  - `source.fixAll.tally`
- Optionally also provide `source.fixAll` mapping (scoped action remains preferred in docs).
- Add explicit command `tally.applyAllFixes` that maps to LSP `workspace/executeCommand` for manual invocation.
- Add integration tests specifically for:
  - `editor.codeActionsOnSave = { "source.fixAll.tally": "always" }`
  - honoring inline ignore directives
  - conflict-safe multi-fix behavior

### 1.4 How are local config, workspace settings, user settings merged?

**Ruff strengths**

- Strong settings precedence handling and variable substitution.
- Explicit handling of deprecated/new setting pairs via `inspect()`.
- `configurationPreference` modes (`editorFirst`, `filesystemFirst`, `editorOnly`).

**Oxc strengths**

- Clear split between global VS Code config and per-workspace-folder config.
- Config service that tracks workspace changes and pushes updates to server.

**Recommendation for `tally`**

Copy the best of both:

- Global config object + per-workspace-folder config objects.
- Variable substitution (`${workspaceFolder}`, `${env:...}`, `${userHome}`) for path-like settings.
- Deprecated setting migration behavior via explicit inspect-based precedence.
- `tally.configurationPreference` with Ruff-like semantics.
- Server receives all workspace configs and computes effective config per document URI.

### 1.5 Other implementation nuances relevant to `tally`

- `tally` already has a strong fix model (`SuggestedFix`, safety levels, conflict handling, async resolvers) that maps naturally to LSP code actions.
- `tally` uses line=1-based and column=0-based internally; LSP requires line=0-based and character=0-based, so only line conversion is needed.
- `dockerfile.Parse` already supports parsing from `io.Reader`, so unsaved editor content can be linted without writing temp files.
- Workspace trust should gate non-bundled executable resolution paths.

### 1.6 How `ruff.toml` autocomplete actually works in VS Code

- `ruff-vscode` itself does not contribute TOML schema wiring (`jsonValidation`/`tomlValidation`) and does not reference Taplo/Even Better TOML APIs.
- Ruff autocomplete for `ruff.toml` comes from schema ecosystem integration:
  - Ruff schema is published in SchemaStore as `https://www.schemastore.org/ruff.json`.
  - SchemaStore catalog maps it to `ruff.toml` and `.ruff.toml`.
  - Even Better TOML (Taplo) loads SchemaStore catalog by default and applies matching schema automatically.
- Taplo also supports extension-provided TOML associations through `contributes.tomlValidation` (custom contribution point, analogous to JSON
  `jsonValidation`).

---

## 2. LSP baseline from researched extensions

Both extensions are LSP clients in VS Code:

- Ruff: `LanguageClient` starts either native `ruff server` or legacy `ruff-lsp`.
- Oxc: `LanguageClient` starts `oxlint/oxfmt` with `--lsp`.

For `tally`, we should follow the same model:

- VS Code extension is a thin client/orchestrator.
- All linting/fix logic lives in the Go server (shared with CLI internals).

---

## 3. `typescript-go` investigation and reuse decision

### 3.1 Findings

`microsoft/typescript-go` runs LSP via `tsgo --lsp --stdio` and contains a custom in-repo LSP stack:

- Custom server runtime: `internal/lsp/server.go`
- Custom JSON-RPC transport/types: `internal/lsp/lsproto/*`
- Large generated protocol file: `internal/lsp/lsproto/lsp_generated.go` (~909 KB in the checked revision)

It does **not** use a third-party Go LSP framework package in `go.mod`.

### 3.2 Reuse viability

Direct import/reuse is not viable:

- Implementation lives under `internal/...`, so it is not importable from another module.
- It is tightly coupled to TypeScript-Go internals.
- README marks language service as in-progress and repo is expected to be merged into TypeScript repo later.

Copying code is legally possible (Apache-2.0), but would create a large maintenance burden and high coupling.

### 3.3 Decision

Do **not** copy or depend on `typescript-go` LSP code.

Use a dedicated Go LSP library + small `tally` adapter layer.

---

## 4. Go LSP library recommendation for `tally`

### 4.1 Options checked

- `github.com/tliron/glsp` latest: `v0.2.2` (2024-03-09), includes protocol types + server runtime.
- `go.lsp.dev/protocol` latest: `v0.12.0` (2022-03-23), broad protocol support but older.
- `github.com/sourcegraph/jsonrpc2` latest: `v0.2.1` (2025-02-17), robust JSON-RPC transport but no LSP model layer.

### 4.2 Recommended base stack

- ~~**Primary recommendation**: `github.com/tliron/glsp`~~ **Dropped** — `glsp` v0.2.2 has a broken transitive dependency (
  `github.com/tliron/kutil v0.3.11` is not resolvable).
- **Actual choice**: `go.lsp.dev/protocol` v0.12.0 + `go.lsp.dev/jsonrpc2` v0.10.0
  - `go.lsp.dev/protocol` provides well-typed LSP 3.16 structs (generated from spec).
  - `go.lsp.dev/jsonrpc2` provides the JSON-RPC transport (stdio stream, connection, handler).
  - Thin handler dispatch in `internal/lspserver/server.go` — only methods we implement are routed; rest returns method-not-found.
- **Abstraction rule**: LSP library types are confined to `internal/lspserver/`; core lint packages have no LSP dependency.

### 4.3 Scope note

`go.lsp.dev/protocol` is 3.16-oriented, which is sufficient for our initial diagnostics + code action plan. If we later need specific 3.17-only
endpoints (for example diagnostic pull), we can add them incrementally or swap protocol layer without changing core lint engine.

---

## 5. Target architecture (LSP-first)

### 5.1 Monorepo layout

Add new top-level extension package:

```text
extensions/vscode-tally/
  package.json
  bunfig.toml
  bun.lock
  tsconfig.json
  .vscode-test.mjs
  .vscodeignore
  src/
    extension.ts
    config/
      configService.ts
      vscodeConfig.ts
      workspaceConfig.ts
    binary/
      findBinary.ts
      pathValidator.ts
    lsp/
      client.ts
      lspHelper.ts
    commands/
      restart.ts
      fixAll.ts
    test/
```

Go LSP server package (implemented):

```text
internal/lspserver/
  server.go          # lifecycle, handler dispatch, stdio transport
  diagnostics.go     # lint pipeline reuse, violation→diagnostic conversion
  codeactions.go     # SuggestedFix→CodeAction conversion
  documents.go       # in-memory document store (URI→content)
  server_test.go     # initialize handshake, diagnostics on open/close, conversions
  pipe_test.go       # in-memory pipe for test transport
internal/lsptest/
  setup_test.go      # TestMain (binary build with -cover, GOCOVERDIR), testServer subprocess helper
  lsp_test.go        # black-box protocol tests against real tally lsp --stdio process
cmd/tally/cmd/lsp.go # CLI entrypoint: `tally lsp --stdio`
```

### 5.2 Process model

1. VS Code extension activates.
2. Resolves `tally` executable (local-first, bundled fallback).
3. Starts `tally lsp --stdio` via `LanguageClient`.
4. Server receives document/config events and publishes diagnostics.
5. Fixes are exposed through LSP code actions and execute commands.

### 5.3 Extension toolchain pattern (adapted from `amazon-bedrock-copilot-chat`)

Use the same Bun-based pattern already proven in `https://github.com/tinovyatkin/amazon-bedrock-copilot-chat`:

- `extensions/vscode-tally/package.json`:
  - `packageManager: "bun"`
  - scripts:
    - `compile`: `bun build ...` (CJS bundle for VS Code host)
    - `test`: `vscode-test`
    - `vsce:package`: `bunx @vscode/vsce package ...`
- VS Code test runner config:
  - `extensions/vscode-tally/.vscode-test.mjs` using `@vscode/test-cli` `defineConfig(...)`
- `extensions/vscode-tally/bunfig.toml`:
  - Bun runtime enabled for scripts
  - deterministic install settings
- lockfile:
  - commit `bun.lock`
- CI/release:
  - `oven-sh/setup-bun`
  - `bun install --frozen-lockfile`
  - `bun run compile`
  - `bunx @vscode/vsce ...`

Rationale:

- keeps monorepo toolchain as Go + Bun (instead of Go + npm/pnpm + Bun)
- lower maintenance overhead for extension packaging and CI
- matches an existing repo maintained by this project owner

---

## 6. Binary delivery and discovery design

### 6.1 Extension packaging strategy

Ship platform-specific VSIX artifacts containing bundled `tally` binary in e.g.:

```text
extensions/vscode-tally/bundled/bin/<platform>/<arch>/tally[.exe]
```

Resolve bundled binary by `process.platform` + `process.arch` at runtime.

### 6.2 Runtime resolution order

`findTallyBinary(settings, workspace)` should use:

1. Explicit `tally.path` entries (first existing executable)
2. If `importStrategy = useBundled`: bundled binary
3. Project-local npm
4. Project-local Python venv
5. Helper-interpreter lookup (optional)
6. `PATH`
7. bundled binary

In untrusted workspace:

- skip workspace-local discovery
- allow only bundled binary or user-global explicit trusted path

### 6.3 npm detection details

Check these candidates:

- `<workspace>/node_modules/.bin/tally`
- `<workspace>/node_modules/.bin/tally.cmd` (Windows)
- `require.resolve("tally-cli")` fallback path handling when needed

Prefer direct executable in `.bin`; avoid shell unless Windows shim handling requires it.

### 6.4 Python detection details

Check these candidates:

- `<workspace>/.venv/bin/tally`
- `<workspace>/venv/bin/tally`
- `<workspace>/.venv/Scripts/tally.exe`
- `<workspace>/venv/Scripts/tally.exe`

Optional interpreter-probe mode:

- run configured interpreter with small script equivalent to Ruff’s `find_ruff_binary_path.py` approach

### 6.5 Security checks

Adopt Oxc-style path validation:

- reject traversal patterns in user-provided binary path
- reject shell-metacharacter patterns for paths when shell execution is used
- if Windows `shell: true` is required for shim execution, quote command path and validate strictly

### 6.6 Version probe contract (`tally version --json`)

Add machine-readable version output for extension compatibility checks:

- command: `tally version --json`
- expected fields (v1):
  - `version`: tally semantic version string (for example `0.4.0`)
  - `buildkitVersion`: linked BuildKit version (if available)
  - `platform`:
    - `os`
    - `arch`
  - `goVersion`: Go toolchain used for build (optional but useful for diagnostics)
  - `gitCommit`: embedded commit SHA when available

Example:

```json
{
  "version": "0.4.0",
  "buildkitVersion": "v0.27.1",
  "platform": { "os": "darwin", "arch": "arm64" },
  "goVersion": "go1.25.0",
  "gitCommit": "abc1234"
}
```

Extension behavior:

- after resolving binary path, probe `tally version --json`
- reject/disable server startup if JSON is invalid or version is below minimum supported
- surface actionable error in VS Code output channel and notification

---

## 7. LSP server implementation plan (Go)

### 7.1 New CLI entrypoint

Add `tally lsp --stdio` subcommand in `cmd/tally/cmd/lsp.go`.

Rules:

- only `--stdio` for v1
- non-zero on startup protocol failures
- structured server logs to stderr (or file if configured)

### 7.2 Server capabilities (phase 1)

Implement:

- `initialize`, `initialized`, `shutdown`, `exit`
- `textDocument/didOpen`
- `textDocument/didChange`
- `textDocument/didClose`
- `textDocument/didSave`
- `textDocument/codeAction`
- `textDocument/formatting`
- `workspace/didChangeConfiguration`
- `workspace/didChangeWorkspaceFolders`
- `workspace/executeCommand` (`tally.applyAllFixes`)

Publish diagnostics via `textDocument/publishDiagnostics`.

Formatting note:

- start with full-document formatting (`textDocument/formatting`)
- add `textDocument/rangeFormatting` only when range-safe behavior is implemented and tested

### 7.3 Document model

Maintain in-memory document store keyed by URI:

- current text
- version
- language id
- workspace folder root

Lint from memory text (`dockerfile.Parse(reader, cfg)`) to support unsaved edits.

### 7.4 Diagnostics pipeline reuse

Reuse current internals rather than shelling out to CLI:

- `dockerfile.Parse`
- semantic model + rules
- processor chain
- `rules.Violation` output

Convert locations:

- `lspLine = violation.Location.Start.Line - 1`
- `lspCharacter = violation.Location.Start.Column`

Severity mapping:

- error -> `DiagnosticSeverity.Error`
- warning -> `Warning`
- info/style -> `Information` or `Hint` (decide once and keep stable)

### 7.5 Code actions and fix-all

Use `Violation.SuggestedFix` as source of truth.

Provide:

- Quick fix per violation (`quickfix`)
- File-level fix all (`source.fixAll.tally`)

For fix-all action:

- collect eligible fixes (default safe only)
- run through existing `internal/fix` conflict resolution before emitting edits
- return one consolidated workspace edit to avoid overlapping-edit failures

Unsafe fixes:

- default excluded from save-time fix-all
- optional manual command path for unsafe mode via setting

---

## 8. VS Code extension design

### 8.1 Language client startup

- Use `vscode-languageclient/node`.
- Command: resolved binary path with args `lsp --stdio`.
- Document selector:
  - language `dockerfile`
  - file patterns: `**/Dockerfile*`, `**/Containerfile*`

### 8.2 Configuration model (best of Ruff + Oxc)

Split settings classes:

- `VSCodeConfig`: global extension settings
- `WorkspaceConfig`: per-workspace-folder settings
- `ConfigService`: aggregate, refresh, and dispatch change events

Expose both workspace and global config to server in initialization options and `didChangeConfiguration`.

### 8.3 Proposed settings

```json
{
  "tally.enable": true,
  "tally.path": [],
  "tally.importStrategy": "fromEnvironment",
  "tally.interpreter": [],
  "tally.configuration": null,
  "tally.configurationPreference": "editorFirst",
  "tally.lint.run": "onType",
  "tally.fixAll": true,
  "tally.fixUnsafe": false,
  "tally.format.mode": "style",
  "tally.format.requireParseable": true,
  "tally.trace.server": "off"
}
```

`configurationPreference` semantics:

- `editorFirst`: extension settings override filesystem config
- `filesystemFirst`: filesystem config wins, extension only fills gaps
- `editorOnly`: ignore filesystem config and use extension settings only

### 8.4 Fix-on-save UX

Recommend scoped action in docs:

```json
{
  "[dockerfile]": {
    "editor.codeActionsOnSave": {
      "source.fixAll.tally": "explicit"
    }
  }
}
```

Also support explicit command `Tally: Fix all auto-fixable issues`.

### 8.5 Formatter UX (default formatter support)

`tally` should also register as a Dockerfile formatter through LSP formatting capability so users can select it as default formatter.

Recommended docs snippet:

```json
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

Why `formatOnSaveMode = "file"`:

- VS Code supports `file|modifications|modificationsIfAvailable`, but modification-only modes require selection/range formatting support.
- Oxc uses the same recommendation for its formatter-first flow.

Add helper command:

- `tally.configureDefaultFormatterForDockerfile`
  - writes language-scoped workspace settings for `[dockerfile]` default formatter and recommended save options.

### 8.6 Config schema validation for `.tally.toml`

`tally` should provide Oxc-like schema-backed config validation, adapted for TOML:

- Source of truth:
  - keep `gen/jsonschema.go` as canonical schema generator
  - generated artifact remains `schema.json` in repo root
- Editor integration model:
  - JSON `contributes.jsonValidation` is not applicable to TOML files
  - use Taplo/Even Better TOML schema discovery model:
    - publish schema to SchemaStore with file matches:
      - `tally.toml`
      - `.tally.toml`
    - in VS Code extension `package.json`, contribute `tomlValidation` entry:
      - `fileMatch`: `["tally.toml", ".tally.toml"]`
      - `url`: `https://json.schemastore.org/tally.json`
    - this is auto-consumed by `tamasfe.even-better-toml` (Taplo), no setup command needed
- Compatibility fallback:
  - document manual setup via `evenBetterToml.schema.associations` only as fallback when users need custom overrides.
  - optional per-file directive (`#:schema ...`) can remain a documented escape hatch.
- Policy:
  - do not silently mutate user/workspace settings for schema setup.
  - schema integration should work automatically through contribution metadata + SchemaStore.

---

## 9. Release integration in existing GitHub workflow

Goal: release extension from same `.github/workflows/release.yml`, no separate repo.

### 9.1 Workflow changes

Add jobs after `sign-macos` (so signed mac binaries are available):

1. `build-vscode-vsix` (matrix by VS Code target platform)
2. `publish-vscode-marketplace` (optional, secret-driven)
3. `publish-openvsx` (optional, secret-driven)

Job runtime setup should use Bun only (no `actions/setup-node` required for package management):

- `oven-sh/setup-bun`
- `bun install --frozen-lockfile`

### 9.2 Build-vsix flow

- Checkout repo
- Download `dist-signed` artifact
- Extract signed binaries
- Copy target binary into `extensions/vscode-tally/bundled/bin/...`
- Setup Bun (`oven-sh/setup-bun`)
- Build extension (`bun install --frozen-lockfile`, `bun run compile`)
- Package per-target VSIX (`bunx @vscode/vsce package --target <code-target>`)
- Upload VSIX as workflow artifact
- Upload VSIX to GitHub release assets

### 9.3 Publish flow

Marketplace publishing should be conditional on secrets:

- `VSCE_PAT` for Visual Studio Marketplace
- `OVSX_TOKEN` for OpenVSX

Use the same tagged release trigger already in place.
Use Bun invocation for publisher CLIs:

- `bunx @vscode/vsce publish ...`
- `bunx ovsx publish ...` (if OpenVSX publishing is enabled)

### 9.4 Schema publishing and freshness in release pipeline

Add schema-specific steps to the same release workflow:

- During build:
  - run `make jsonschema`
  - fail CI if working tree changes after generation (prevents stale checked-in schema)
- As release artifact:
  - upload `schema.json` to GitHub release assets
- Stable URL strategy:
  - primary target: `https://json.schemastore.org/tally.json` (matches schema `$id`)
  - maintain fallback URL for direct raw GitHub consumption
- Ongoing maintenance:
  - when config shape changes, schema is regenerated in same PR and validated in CI.

---

## 10. Implementation phases

### Phase 1: Scaffold and diagnostics (**Go server: DONE**)

- Create extension package and minimal activation.
- Create Bun-based extension scaffolding (`packageManager: bun`, `bunfig.toml`, `bun.lock`, Bun scripts).
- ~~Add `tally lsp --stdio` command.~~ **Done** (`cmd/tally/cmd/lsp.go`)
- ~~Add `tally version --json` in CLI and version package for machine-readable probing.~~ **Done** (`internal/version/version.go`,
  `cmd/tally/cmd/version.go`)
- ~~Implement LSP initialize/open/change/save/close + publish diagnostics.~~ **Done** (`internal/lspserver/`)
- ~~Implement code actions (quickfix from SuggestedFix).~~ **Done** (`internal/lspserver/codeactions.go`)
- Add simple binary resolution: explicit path + bundled fallback. (VS Code client — not started)

### Phase 2: Full binary resolution + config model

- Add npm and Python local resolution.
- Add workspace trust gating and path validation.
- Add `ConfigService` split model and didChangeConfiguration propagation.
- Add configuration preference logic.

### Phase 2.5: TOML schema integration

- Wire schema generation checks (`make jsonschema`) into CI/release flow.
- Publish/update SchemaStore entry for `tally.toml` and `.tally.toml`.
- Add `contributes.tomlValidation` entry in extension `package.json`.
- Add docs for automatic schema discovery and manual fallback association override.

### Phase 3: Code actions and fix on save

- Implement quick fixes and `source.fixAll.tally`.
- ~~Implement `textDocument/formatting` with style-safe formatting profile.~~ **Done** (`internal/lspserver/formatting.go`, composes
  `internal/linter/` + `internal/fix/`)
- Implement command-based full-file fix.
- Add fix conflict-safe aggregation using existing fixer pipeline.
- Add integration tests for save behavior.

### Phase 3.5: Formatter-as-default experience

- Add formatter configuration command (`tally.configureDefaultFormatterForDockerfile`).
- Add docs for selecting `tally` as default formatter via `editor.defaultFormatter`.
- Add explicit guidance for `editor.formatOnSaveMode = "file"` until range formatting exists.

### Phase 4: Release pipeline and publishing

- Add VSIX build/publish jobs to `.github/workflows/release.yml`.
- Add extension packaging docs in repo.
- Validate artifacts on macOS/Linux/Windows.

---

## 11. Test strategy

### 11.1 Test boundary (critical for LSP-first + Zed reuse)

Split tests into two independent layers:

- **Pure LSP server tests (editor-agnostic)**:
  - validate `tally lsp --stdio` protocol and behavior without VS Code APIs.
  - this is the primary quality gate for all editors (VS Code now, Zed next).
- **VS Code integration tests (client adapter)**:
  - validate only VS Code-specific wiring and UX behavior.
  - should not be the only place where server correctness is verified.

Policy:

- Any behavior expected to work in both VS Code and Zed must be asserted in pure LSP tests first.
- VS Code tests then assert extension adapter concerns (settings mapping, activation, commands).

### 11.2 Pure LSP server tests (editor-agnostic, reusable by Zed)

#### 11.2.1 In-process Go tests (fast path)

- Keep and expand Go unit/component tests under `internal/lspserver/**`:
  - position conversion (1-based to 0-based lines)
  - diagnostics mapping
  - code action construction and edit conflict handling
  - formatting edit generation and idempotence
  - config merge behavior
- Keep protocol framing tests (reader/writer, invalid headers, EOF paths).
- Add lifecycle/concurrency tests:
  - initialize/shutdown/exit
  - cancellation
  - no-deadlock behavior after shutdown

#### 11.2.2 Black-box protocol tests (subprocess) — **DONE** (`internal/lsptest/`)

- ~~Run `tally lsp --stdio` as a subprocess and drive it as a real client.~~ **Done**
- ~~Default tooling (monorepo-native):~~ **Done**
  - ~~**Go test harness** in this repo using:~~ **Done**
    - ~~`os/exec` to launch `tally lsp --stdio`~~ **Done**
    - ~~VS Code LSP framing over stdio (Content-Length protocol)~~ **Done** (via `go.lsp.dev/jsonrpc2`)
    - ~~`github.com/sourcegraph/jsonrpc2` as transport helper (test-only dependency)~~ Used `go.lsp.dev/jsonrpc2` instead (already in go.mod, handles
      Content-Length framing)
    - ~~`go.lsp.dev/protocol` message types for strongly typed requests/responses~~ **Done**
- Optional external tooling (not required for CI):
  - `pytest-lsp` for extra cross-ecosystem protocol validation
  - `lsp-devtools` (`agent`/`record`/`inspect`) for ad-hoc trace debugging
- ~~Assert contract-level scenarios:~~ **Done** (10 parallel tests)
  - ~~`initialize` + capabilities~~ **Done** (`TestLSP_Initialize`)
  - ~~`didOpen`/`didChange`/`didSave` diagnostic lifecycle~~ **Done** (`TestLSP_DiagnosticsOnDidOpen`, `TestLSP_DiagnosticsUpdatedOnDidChange`,
    `TestLSP_DiagnosticsOnDidSave`, `TestLSP_DiagnosticsClearedOnClose`)
  - ~~`textDocument/codeAction` (`quickfix`)~~ **Done** (`TestLSP_CodeAction`)
  - `textDocument/codeAction` (`source.fixAll.tally`) — blocked on server implementation
  - `workspace/executeCommand` (`tally.applyAllFixes`) — blocked on server implementation
  - ~~`textDocument/formatting`~~ **Done** (`TestLSP_Formatting`, `TestLSP_FormattingNoChanges`)
  - `workspace/didChangeConfiguration` recomputation behavior — blocked on server implementation
- ~~Lifecycle test: shutdown + exit~~ **Done** (`TestLSP_ShutdownExit`)
- ~~Error handling: unknown method~~ **Done** (`TestLSP_MethodNotFound`)
- ~~Coverage tracking via GOCOVERDIR (same mechanism as `internal/integration/`)~~ **Done**

#### 11.2.3 Golden fixtures for cross-editor stability

- Maintain fixture-based golden assertions for:
  - diagnostics payloads
  - workspace edits returned by code actions
  - formatted output
- Goldens must be independent from VS Code APIs so they can be reused for Zed validation.

### 11.3 VS Code integration tests (adapter-specific)

- Unit tests for VS Code extension internals:
  - binary resolver (npm, venv, explicit path, fallback)
  - settings mapping and precedence handling
  - workspace trust gating logic
- Integration tests with `@vscode/test-electron`:
  - diagnostics appear on open/edit/save in VS Code
  - `source.fixAll.tally` on `editor.codeActionsOnSave` applies expected edits
  - `editor.action.formatDocument` applies deterministic formatting edits
  - `editor.formatOnSave` + `source.fixAll.tally` remains stable/idempotent
  - workspace settings changes propagate to running server
- Use `@vscode/test-cli` config file (`.vscode-test.mjs`) for deterministic suite selection/timeouts.

These tests intentionally cover VS Code behavior, not generic LSP correctness.

### 11.4 Schema tests

- Add schema generation test/check:
  - `make jsonschema` produces deterministic output
  - CI fails if `schema.json` is out-of-date
- Validate sample `.tally.toml` fixtures against generated schema:
  - valid config passes
  - invalid enum/value cases fail with useful diagnostics

### 11.5 CI gating recommendation

- Required on every PR:
  - Go unit/component tests
  - ~~pure LSP black-box tests (subprocess, Go-native harness)~~ **Done** (separate CI step in `.github/workflows/ci.yml`)
  - schema freshness check
- Required before extension release:
  - VS Code integration suite
  - extension packaging smoke tests
- Optional periodic/non-blocking lane:
  - external protocol checks using `pytest-lsp` (only if we later decide the extra dependency is worth it)

---

## 12. Risks and mitigations

- LSP library lock-in risk:
  - Mitigation: keep protocol/transport behind adapter package.
- Binary resolution complexity across ecosystems:
  - Mitigation: strict resolution order + exhaustive tests on fixture workspaces.
- Fix-all overlap/ordering bugs:
  - Mitigation: reuse existing `internal/fix` conflict logic rather than ad-hoc edit merging.
- Release workflow complexity growth:
  - Mitigation: isolate extension jobs and keep artifacts contract explicit (`dist-signed` -> VSIX).
- TOML schema tooling fragmentation (different TOML extensions):
  - Mitigation: primary path is SchemaStore + `contributes.tomlValidation`; document manual association fallback for non-Taplo workflows.

---

## 13. `vscode-containers` integration and conflict analysis

Repository analyzed: `https://github.com/microsoft/vscode-containers`

### 13.1 Relevant behaviors observed

- The extension contributes and activates on Dockerfile language (`onLanguage:dockerfile`) and registers Dockerfile file patterns including
  `Dockerfile*` and `Containerfile*`.
- It starts its own Dockerfile LSP client (`dockerfile-language-server-nodejs`) from bundled JS server code.
- It contributes Dockerfile diagnostics settings under `docker.languageserver.diagnostics.*`.
- It adds Dockerfile editor/explorer context commands such as `vscode-containers.images.build` and `vscode-containers.registries.azure.buildImage`.
- It registers an extra Dockerfile completion provider for `FROM` image suggestions.
- It has no top-level setting to disable Dockerfile language service (only compose language service has `containers.enableComposeLanguageService`).

### 13.2 Direct conflict points with `tally`

- Duplicate diagnostics when both LSPs analyze Dockerfiles.
- Highest overlap is on rules equivalent to:
  - deprecated `MAINTAINER`
  - empty continuation line
  - instruction/directive casing
  - multiple `CMD` / `ENTRYPOINT` / `HEALTHCHECK`
  - relative `WORKDIR`
- Potentially duplicated quick fixes / code actions if both servers offer fixes.
- UX noise in Problems panel and save-time actions if users use generic `source.fixAll` instead of scoped action.

### 13.3 Automatic integration opportunities

- Add optional bridge command: `tally.checkAndBuildImage`.
  - Runs `tally` diagnostics/fixes first.
  - If successful (or user confirms), calls `vscode.commands.executeCommand('vscode-containers.images.build', activeDocumentUri)`.
- Add context menu entry for Dockerfiles with the same trust guard style as Container Tools (`isWorkspaceTrusted && editorLangId == dockerfile`).
- Reuse their Dockerfile glob coverage (`Dockerfile*`, `Containerfile*`) for command/file discovery parity.

### 13.4 Coexistence strategy to adopt in `tally` extension

- Keep `tally` command and setting namespaces isolated (`tally.*` only).
- Prefer scoped save action in docs: `source.fixAll.tally`, not generic `source.fixAll`.
- Add extension-detection behavior (pattern inspired by their compose coexistence feature):
  - Detect `ms-azuretools.vscode-containers` at activation.
  - Offer one-click setup command to reduce duplicate diagnostics by setting overlapping `docker.languageserver.diagnostics.*` entries to `ignore` in
    workspace settings.
  - Do not auto-mutate settings silently.
- Keep `tally` document selector restricted to local files for v1 (`scheme: file`) to avoid unsupported custom FS schemes (for example `containers:`).

### 13.5 Design updates resulting from this analysis

- Add `tally.integration.vscodeContainers.enableCheckThenBuild` (boolean, default `false`).
- Add command `tally.integration.configureContainersCoexistence` to write recommended `docker.languageserver.diagnostics.*` overrides.
- Add “coexistence” subsection to README/settings docs with two supported modes:
  - `preferTally` (disable overlapping docker language-server diagnostics)
  - `allowBoth` (keep both diagnostics)

---

## 14. `docker/vscode-extension` (Docker DX) integration and conflict analysis

Repository analyzed: `https://github.com/docker/vscode-extension`

### 14.1 Relevant behaviors observed

- Docker DX is LSP-first and activates on Dockerfile + Compose languages.
- It starts a native Docker Language Server binary and may start `dockerfile-language-server-nodejs` as a fallback path.
- It explicitly checks if `ms-azuretools.vscode-containers` is installed.
- When `ms-azuretools.vscode-containers` is installed, Docker DX:
  - avoids starting overlapping fallback Dockerfile LS
  - passes an init option (`dockerfileExperimental.removeOverlappingIssues`) to suppress duplicate issues
- It also integrates with Container Tools UI contexts (for example commands enabled in `vscode-containers.views.images`).
- Compose coexistence is handled by settings and documented guidance (especially around YAML/compose language features overlap).

### 14.2 Interaction model between Docker DX and Microsoft Container Tools

- Relationship is cooperative, not exclusive:
  - both extensions can be installed together
  - Docker DX includes runtime logic to reduce overlap
  - Docker DX exposes feature commands into Container Tools views where useful
- Key pattern to copy for `tally`: detect peer extension presence and adjust overlap behavior via explicit options, not by assuming single-owner
  control of Dockerfile diagnostics.

### 14.3 Direct conflict points with `tally`

- Potential triple overlap for Dockerfile diagnostics:
  - `ms-azuretools.vscode-containers`
  - `docker.vscode-extension` (Docker DX)
  - `tally`
- Code action overlap on save when users configure generic `source.fixAll` instead of `source.fixAll.tally`.
- Problem panel source ambiguity if diagnostics are not clearly labeled.
- Command/context crowding in Dockerfile editor menus.

### 14.4 Coexistence strategy to adopt in `tally`

- Keep `tally` LSP diagnostics source explicit (`tally`) to preserve origin clarity.
- Prefer scoped save action in docs/examples: `source.fixAll.tally`.
- Add peer extension detection for:
  - `ms-azuretools.vscode-containers`
  - `docker.vscode-extension`
- Add an explicit workspace command:
  - `tally.integration.configureDockerExtensionsCoexistence`
  - Applies recommended workspace overrides only after user action.
- Recommended coexistence modes in docs:
  - `preferTally`: disable overlapping Dockerfile diagnostics in other extensions where configurable
  - `allowBoth`: keep all providers enabled and accept duplicates
- Do not silently mutate user/workspace settings on activation.

### 14.5 Automatic integration opportunities

- Optional bridge command `tally.checkThenDockerBuild`:
  - runs `tally` checks/fixes on active Dockerfile
  - triggers known build commands if installed (`vscode-containers.images.build` and Docker DX build command variants when available)
- Keep the bridge opt-in via setting:
  - `tally.integration.dockerBuildBridge.enabled` default `false`
- Guard command enablement with context:
  - trusted workspace
  - Dockerfile/Containerfile active editor
  - required peer command exists

### 14.6 Design updates resulting from this analysis

- Add peer-extension integration module in client:
  - `extensions/vscode-tally/src/integration/dockerExtensions.ts`
- Add telemetry-free local logging (output channel) for coexistence decisions (detected peers, chosen mode, applied recommendations).
- Add integration tests with mocked installed extensions/commands to verify:
  - no silent settings mutation
  - command availability toggles correctly
  - scoped fix-on-save remains stable with peers installed

---

## 15. `vscode-hadolint` coexistence analysis and integration design

Repository analyzed: `https://github.com/michaellzc/vscode-hadolint`

### 15.1 What this extension does (relevant to overlap)

- Extension ID is `exiasr.hadolint` (`package.json` publisher `exiasr`, name `hadolint`).
- Activation is on `dockerfile` language and a manual command (`hadolint.selectExecutable`).
- It is LSP-based (`vscode-languageclient` + `vscode-languageserver`), but diagnostics-only in practice:
  - no code action provider / fix-all flow implemented
  - diagnostics are produced by spawning `hadolint` CLI on open/save/change
  - lint execution is synchronous CLI spawn per validation event, so overlap can amplify CPU/latency in large files
- Binary model is system-local only:
  - uses `hadolint` from PATH or `hadolint.hadolintPath`
  - no bundled hadolint binary
- It watches `.hadolint.yaml` and revalidates Dockerfiles per workspace.

### 15.2 Conflict profile with `tally`

- Main conflict is duplicated diagnostics (same rules surfaced by both extensions).
- No direct quick-fix collision from Hadolint extension itself (it does not provide autofixes/code actions).
- Problem panel noise is likely when both are active.
- Save-time behavior can still look noisy if users enable generic `source.fixAll` and also keep Hadolint diagnostics enabled.

### 15.3 Best detection strategy in `tally` client

- Detect by extension ID:
  - `vscode.extensions.getExtension('exiasr.hadolint')`
- Re-evaluate on extension lifecycle changes:
  - subscribe to `vscode.extensions.onDidChange`
- Only prompt/intervene when:
  - active editor is Dockerfile/Containerfile
  - workspace is trusted
  - user has not already chosen a coexistence mode for this workspace

### 15.4 Best interaction model (pragmatic and API-safe)

- Important API constraint:
  - VS Code stable extension API does not provide a guaranteed, documented method to programmatically disable another extension directly.
- Important Hadolint setting constraint:
  - `hadolint.outputLevel` only supports `error|info|warning|hint` (no `ignore` value in its schema).
  - Setting `ignore` is unsafe because the extension maps severity by enum key and unknown keys are not handled.
- Recommended UX:
  - show a one-time actionable prompt when `exiasr.hadolint` is detected:
    - `Auto-suppress Hadolint diagnostics in this workspace` (fully automatic)
    - `Disable Hadolint for this workspace` (guided flow)
    - `Disable tally Hadolint-parity rules`
    - `Keep both`
- `Auto-suppress Hadolint diagnostics in this workspace` implementation:
  - update `hadolint.maxNumberOfProblems = -1` in workspace settings.
  - rationale: current `vscode-hadolint` implementation skips all diagnostics when max is negative.
  - store previous workspace value in `workspaceState` for reversible rollback.
- `Disable Hadolint for this workspace` implementation:
  - open extension details page via `vscode.commands.executeCommand('extension.open', 'exiasr.hadolint')`
  - show clear next step text: use extension gear menu -> Disable (Workspace)
- `Disable tally Hadolint-parity rules` implementation:
  - update workspace setting that gates the parity ruleset (for example `tally.rulesets.hadolint.enabled = false`)
  - immediately send `workspace/didChangeConfiguration` so LSP server recomputes diagnostics

### 15.5 Settings and command additions for `tally`

- Add setting:
  - `tally.integration.hadolint.coexistenceMode` with values:
    - `ask` (default)
    - `preferTally`
    - `preferHadolint`
    - `allowBoth`
- Add setting:
  - `tally.integration.hadolint.autoSuppressStrategy` with values:
    - `maxProblemsMinusOne` (default)
    - `guidedDisableOnly`
- Add setting:
  - `tally.rulesets.hadolint.enabled` (boolean, default `true`)
- Add command:
  - `tally.integration.resolveHadolintOverlap`
  - Re-opens the coexistence chooser and applies the selected action.
- Add command:
  - `tally.integration.restoreHadolintSettings`
  - Restores `hadolint.*` values previously modified by `tally`.

### 15.6 Behavior matrix

- `preferTally`:
  - keep `tally.rulesets.hadolint.enabled = true`
  - if `autoSuppressStrategy = maxProblemsMinusOne`, auto-set `hadolint.maxNumberOfProblems = -1`
  - otherwise prompt guided disable of `exiasr.hadolint` in workspace
- `preferHadolint`:
  - set `tally.rulesets.hadolint.enabled = false`
  - keep other `tally`-specific rules active
- `allowBoth`:
  - no changes; optional warning in output channel about possible duplicate diagnostics
- `ask`:
  - one-time prompt on first Dockerfile session in workspace

### 15.7 Implementation nuance to copy from researched extensions

- Follow Docker DX pattern: detect peer extension presence and adapt behavior explicitly.
- Follow Ruff/Oxc pattern: keep coexistence decisions in workspace configuration/state, not hidden heuristics.
- Do not mutate unrelated extension settings silently.
- If mutating `hadolint.*` settings, always keep an explicit restore command and one-time notification.

---

## 16. Formatter API integration (`tally` as default Dockerfile formatter)

### 16.1 Research findings to apply

- Ruff and Oxc both expose formatter capabilities and document language-scoped `editor.defaultFormatter` usage.
- Oxc explicitly recommends `editor.formatOnSaveMode = "file"` for reliable whole-file formatting.
- VS Code supports `editor.formatOnSaveMode` values:
  - `file`
  - `modifications`
  - `modificationsIfAvailable`
- `modifications` modes require a formatter that supports selection/range formatting.
- VS Code save participant order runs code actions before formatting (`CodeActionOnSaveParticipant` before `FormatOnSaveParticipant`), so
  `source.fixAll.*` can run first and formatter can normalize final output.

### 16.2 LSP capability design for `tally`

- LSP-first formatter implementation in Go server:
  - `textDocument/formatting` in phase 1
  - `textDocument/rangeFormatting` in phase 2 (optional, only after correctness tests)
- Capability advertisement:
  - `documentFormattingProvider: true`
  - `documentRangeFormattingProvider: false` initially
- Formatting capability is always advertised; users opt-in by selecting `wharflab.tally` as the default formatter and enabling
  `editor.formatOnSave`.

### 16.3 Formatting scope and safety contract

- Formatter should be deterministic and idempotent.
- Formatter should be style-focused (casing/indent/heredoc layout and other formatting-only transforms), not semantic lint rewrites.
- Unsafe/non-style fixes remain in code actions (`quickfix`, `source.fixAll.tally`) and are controlled by fix settings.
- Formatting request with no effective change should return empty edits.

### 16.4 VS Code client behavior

- Rely on `vscode-languageclient` capability wiring; no manual `registerDocumentFormattingEditProvider` needed.
- Add user-facing command to reduce setup friction:
  - `tally.configureDefaultFormatterForDockerfile`
  - writes workspace-level language override for `[dockerfile]`:
    - `editor.defaultFormatter = "wharflab.tally"`
    - `editor.formatOnSave = true`
    - `editor.formatOnSaveMode = "file"`
    - optionally keep/enable `source.fixAll.tally`
- Keep this command explicit (user-triggered), not silent mutation on activation.

### 16.5 Recommended settings model additions

- `tally.format.mode` (string, default `style`)
- `tally.format.requireParseable` (boolean, default `true`)

Proposed interaction with existing settings:

- `tally.fixAll` controls code actions/fix-all registration.
- Formatting is controlled by VS Code settings like `editor.defaultFormatter` / `editor.formatOnSave`, not by a `tally`-specific toggle.
- `tally.fixUnsafe` does not affect formatter path in v1.

### 16.6 Recommended user configuration snippet

```json
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

### 16.7 Validation requirements

- `Format Document` produces expected edits for Dockerfile/Containerfile.
- Repeat formatting is a no-op (idempotence).
- Save with both format + fixAll enabled is stable.
- Multi-root workspace settings apply per folder.
- Coexistence tests when other Docker extensions are installed (no formatter registration conflicts beyond standard default formatter selection).

---

## 17. Final recommendation

Proceed with the LSP-first architecture now, with:

- Go LSP server integrated into `tally`
- TypeScript VS Code client in `extensions/vscode-tally`
- Bun-first extension build/test/release tooling
- Hybrid binary strategy (local-first, bundled fallback)
- Ruff+Oxc-inspired settings, fix-on-save behavior, and formatter-default UX
- Unified monorepo release via `.github/workflows/release.yml`

This gives low user friction, good project-local parity (npm + Python), and maximum reuse of `tally`’s existing lint/fix internals.
