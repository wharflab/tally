# IntelliJ Tally Plugin (Lean Build)

This extension contains an IntelliJ Platform plugin for `tally` with a lean, no-Gradle build pipeline.

## Build

From repository root:

```bash
make intellij-plugin
```

Output:

- `extensions/intellij-tally/dist/tally-intellij-plugin-<version>.zip`

## Smoke check (IntelliJ Community Edition)

From repository root:

```bash
make intellij-plugin-smoke
```

This runs JetBrains Plugin Verifier against IntelliJ IDEA Community Edition.
Expected result: plugin is reported as **Compatible**. In CE, verifier may still list
`com.intellij.modules.lsp` as an unavailable **optional** dependency.

## Runtime settings (current MVP)

Until a dedicated IntelliJ settings UI is added, runtime options can be provided via JVM system properties:

- `-Dtally.executablePaths=/abs/path/to/tally,/other/path/to/tally`
- `-Dtally.importStrategy=fromEnvironment|useBundled`
- `-Dtally.fixUnsafe=true|false`
- `-Dtally.configurationOverride=<json-string>`

The plugin always launches `tally lsp --stdio`. For trusted projects, executable auto-resolution checks PATH first, then the project SDK interpreter
directory, then project virtualenv locations (`.venv`/`venv`), then bundled fallback.

## Build inputs

Pinned versions and download URLs live in:

- `extensions/intellij-tally/build/versions.toml`
