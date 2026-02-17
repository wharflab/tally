# IntelliJ Plugin for `tally` (Lean build, no Gradle)

Research date: 2026-02-17

Status: Draft for implementation

## Executive summary

We want an IntelliJ Platform plugin for `tally` that lives in this monorepo under `extensions/`, but **without Gradle** (or Maven) and with a
purposely-built build pipeline that talks directly to the **Kotlin compiler**.

The proposed approach is:

- **LSP-first**: the IntelliJ plugin is a thin client that launches `tally lsp --stdio` and uses IntelliJ’s `com.intellij.platform.lsp.api`
  integration.
- **Lean build**: a small, repo-local builder (preferably in Go) that:
  1) downloads an IntelliJ distribution to use as the compile-time SDK,
  2) downloads a Kotlin compiler distribution,
  3) compiles Kotlin sources with `kotlinc` against the IDE jars,
  4) packages a Marketplace-installable plugin ZIP.
- **Hybrid binary strategy** (same philosophy as `extensions/vscode-tally`): prefer a user/project-provided `tally` binary; optionally fall back to a
  bundled binary set for “works out of the box”.

## Constraints / requirements

- Plugin source lives at `extensions/intellij-tally/` in this repo.
- Avoid Gradle (and similar “full build systems”); keep dependencies explicit and minimal.
- Build should be reproducible and cache downloads (IDE SDK + Kotlin compiler).
- Prefer a single supported IntelliJ baseline initially; expand later once the build pipeline is stable.

## Findings from `oxc-intellij-plugin` (local repo exploration)

`oxc-intellij-plugin` is a modern IntelliJ plugin that:

- Uses Gradle + JetBrains “IntelliJ Platform Gradle Plugin” (`org.jetbrains.intellij.platform`).
- Uses Kotlin JVM plugin; version catalog pins Kotlin `2.2.21`.
- Targets IDE builds where LSP APIs are available in free IDEs:
  - `pluginSinceBuild = 252.25557` (comment: first version that supports the LSP APIs in free IDEs)
- Declares LSP via plugin.xml dependency and extension point:
  - `<depends>com.intellij.modules.lsp</depends>`
  - `<platform.lsp.serverSupportProvider implementation="..."/>`
- Starts the LSP server by launching an external executable via `OSProcessHandler`, and wires `workspace/configuration` by enabling
  `clientCapabilities.workspace.configuration = true`.

Takeaway: the **runtime architecture** (IntelliJ LSP support provider + descriptor that spawns a process) is directly reusable for `tally`, but the
**Gradle-based build & publishing pipeline** is exactly what we want to replace.

### Dependency and platform notes (relevant to our build plan)

- Oxc pins `platformType = WS` (WebStorm) and `platformBundledPlugins = JavaScript`, because it integrates with JavaScript features (JSON schema EPs
  under `JavaScript`).
- For `tally`, we should **avoid non-essential plugin dependencies** so we can build and run against IntelliJ IDEA Community (`IC`) and other “free
  IDEs” where possible.
- If we adopt IntelliJ’s LSP APIs like Oxc, plugin.xml will need at least:
  - `<depends>com.intellij.modules.platform</depends>`
  - `<depends>com.intellij.modules.lsp</depends>`
  which likely implies a newer baseline build (Oxc uses `sinceBuild = 252.25557`).

## Proposed plugin architecture (runtime)

### 1) LSP-first integration

- IntelliJ plugin launches: `tally lsp --stdio`.
- Uses IntelliJ Platform LSP APIs:
  - `LspServerSupportProvider` to start/stop server per project/content root
  - `LspServerDescriptor` to:
    - filter supported files
    - create initialization options (workspace config)
    - support `workspace/configuration` for live settings changes

### 1.1) Fixes and code actions

How Oxc handles it (and what we should copy):

- IntelliJ’s LSP integration can surface LSP `textDocument/codeAction` results as editor intentions (i.e., quick fixes) when the language server
  provides them.
- Oxc implements **explicit “Fix all”** and **fix-on-save** by *calling* `textDocument/codeAction` with `only = ["source.fixAll.oxc"]` and then
  applying preferred actions via `LspIntentionAction(server, codeAction).invoke(...)`.
- For `tally`, we should implement the same pattern with `only = ["source.fixAll.tally"]` and keep “disable-next-line” style actions as non-preferred
  so fix-all stays clean.

### 2) File targeting

Start conservative:

- Treat as supported if file name matches Docker conventions:
  - `Dockerfile*`, `Containerfile*`, `*.Dockerfile` (optional)

Later (optional): integrate with IntelliJ’s Dockerfile file type (may require Ultimate/bundled Docker plugins depending on IDE).

### 3) Settings surface (minimal)

Mirror the VS Code extension concepts where possible:

- `tally.executablePaths`: list of explicit paths; first existing wins.
- `tally.importStrategy`: `fromEnvironment | useBundled` (optional for v1).
- `tally.fixUnsafe`: gate unsafe fixes behind explicit opt-in.
- `tally.configurationOverride`: optional inline config object (sent via LSP init options / workspace/configuration).

### 4) Bundled vs external `tally` binary

Two viable modes (can implement incrementally):

1. **External-only (v1)**: require users to have `tally` on PATH or configured via plugin settings.
2. **Hybrid (v2)**: ship `tally` binaries inside plugin ZIP under a predictable layout, and choose at runtime based on OS/arch.

## Proposed build system (no Gradle)

### Why a custom builder is needed

Gradle normally provides:

- IntelliJ SDK dependency resolution
- Kotlin compilation setup
- IDE sandbox run tasks
- plugin ZIP packaging
- plugin verifier + signing integration

Without Gradle, we need a small purpose-built replacement for the parts we actually need:

- download SDK
- compile Kotlin
- package ZIP
- (optional) verify and sign

### Repository layout

Create:

```text
extensions/
  intellij-tally/
    src/main/kotlin/...
    src/main/resources/META-INF/plugin.xml
    src/main/resources/icons/...
    build/
      versions.toml (or versions.json)
    dist/ (generated)
    .cache/ (generated; ignored)
```

### Version pinning

Keep all non-Go versions in one small file, for example:

- IntelliJ baseline (product + build)
- Kotlin compiler version
- Plugin version derivation strategy (git tag vs `tally --version`)

This replaces Gradle version catalogs with something maintainable in-repo.

### Build inputs

1) **IntelliJ distribution (compile-time SDK)**

- Download a single IDE distribution (e.g., IntelliJ IDEA Community) for a pinned version.
- Extract jars from:
  - `<IDE>/lib/*.jar`
  - `<IDE>/plugins/lsp/lib/*.jar` (and any other required plugin libs)

2) **Kotlin compiler distribution**

- Download Kotlin compiler ZIP for a pinned version.
- Use its `kotlinc` entrypoint (or invoke compiler main class directly).

### Build outputs

- `dist/tally-intellij-plugin-<version>.zip` containing:

```text
Tally/
  lib/tally-intellij-plugin.jar
  (optional) bin/<os>/<arch>/tally[.exe]
```

Where `tally-intellij-plugin.jar` contains:

- compiled classes
- resources (including `META-INF/plugin.xml`)

### Implementation: bash script first (recommended)

Start with a small bash builder script (plus a couple Makefile targets) that does only what we need:

- downloads and caches (to `extensions/intellij-tally/.cache/`)
  - IDE distribution
  - Kotlin compiler distribution
  - (optional) JetBrains plugin verifier
  - (optional) zip-signer
- computes the compile classpath from IDE jars
- invokes `kotlinc` with explicit args:
  - `-jvm-target 21` (match IntelliJ’s JDK baseline; keep configurable)
  - `-no-stdlib` / `-no-reflect` as appropriate (avoid bundling Kotlin stdlib)
  - `-classpath <IDE jars + kotlin stdlib for compilation>`
- packages jar + plugin zip

If/when the script starts to get brittle (more platforms, retries/checksums, better dependency caching, Windows support), we can migrate the same
steps into a small Go tool under `tools/` without changing the plugin layout.

### Makefile integration

Add targets (names illustrative):

- `make intellij-plugin` → builds ZIP under `extensions/intellij-tally/dist/`
- `make intellij-plugin-verify` → runs plugin verifier against one or more IDE versions
- `make intellij-plugin-run` (optional) → runs an IDE sandbox by launching downloaded IDE with the plugin installed

### What we will *not* replicate initially

To keep the build lean, defer these until the plugin is working:

- searchable options generation
- bytecode instrumentation
- UI designer form compilation
- Marketplace publishing automation

(These can be added later if needed, but many plugins don’t require them for an LSP-only workflow.)

## CI plan (GitHub Actions)

Add a workflow that:

- sets up Java 21
- runs `make build` (to ensure `tally` still builds)
- runs `make intellij-plugin`
- optionally runs plugin verifier on a small IDE matrix (start with 1-2 IDE versions)
- uploads the plugin ZIP as an artifact

## Implementation plan (phased)

### Phase 0 — scaffolding

- Create `extensions/intellij-tally/` skeleton with:
  - minimal `plugin.xml`
  - minimal Kotlin source that registers an `LspServerSupportProvider`
- Decide baseline IDE build and pin it in `extensions/intellij-tally/build/versions.*`.

### Phase 1 — build pipeline MVP (no Gradle)

- Add a bash builder script (e.g. `extensions/intellij-tally/build/build.sh`) to:
  - download IDE distro
  - download Kotlin compiler
  - compile Kotlin
  - produce plugin ZIP
- Add `.gitignore` entries for `.cache/` and `dist/`.

### Phase 2 — runtime MVP

- Start `tally lsp --stdio` when a supported file opens.
- Basic settings: user-provided binary path, otherwise PATH.
- Confirm diagnostics appear for a Dockerfile.

### Phase 3 — configuration + UX

- Send workspace configuration to server:
  - config override
  - unsafe fix gate
- Add status widget (optional) similar to Oxc’s LSP widget pattern.

### Phase 4 — binary distribution strategy

- Implement `useBundled` strategy and include `tally` binaries in plugin ZIP.
- Reuse `goreleaser` outputs to populate `extensions/intellij-tally/bundled/...` during release builds.

### Phase 5 — verification and hardening

- Add plugin verifier in CI.
- Expand IDE coverage carefully (start with latest + oldest supported).

## Open questions / decisions to confirm

1) What baseline IDE line do we want for the first release (252.* like Oxc, or earlier)?
2) Do we require LSP-based formatting in IntelliJ (i.e., formatting support), or is diagnostics-only enough for v1?
3) Do we want bundled `tally` binaries in the first Marketplace release (bigger zip) or require user-managed `tally`?
