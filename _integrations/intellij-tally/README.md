# IntelliJ Tally Plugin

This extension contains an IntelliJ Platform plugin for `tally`. The build is
driven by the [Kotlin Toolchain](https://kotlin-toolchain.org/) — `kotlin build`
compiles the Kotlin sources natively against the IntelliJ Platform Maven
artifacts (`com.jetbrains.intellij.platform:*`), and a custom Amper plugin
(`tally-build/`) provides `build`, `verify`, `smoke`, `ktlint`, and `ktlintFix`
custom commands for packaging, verifier integration, and lint.

Layout:

- `intellij-plugin/` — the actual `jvm/lib` module with the plugin's Kotlin
  sources (`src/`) and IntelliJ Platform deps (`module.yaml`).
- `tally-build/` — the Amper plugin defining the custom commands.
- `kotlin` / `kotlin.bat` — the committed Kotlin Toolchain wrapper.
- `dist/` — output zips (gitignored).

## Build

From repository root:

```bash
make intellij-plugin
```

Output:

- `_integrations/intellij-tally/dist/tally-intellij-plugin-<version>.zip`

## Smoke check (IntelliJ Community Edition)

From repository root:

```bash
make intellij-plugin-smoke
```

This runs JetBrains Plugin Verifier against IntelliJ IDEA Community Edition.
Expected result: plugin is reported as **Compatible**. In CE, verifier may still list
`com.intellij.modules.lsp` as an unavailable **optional** dependency.

## Direct toolchain commands

Anything `make` can do, `./kotlin do <command>` can do directly from this directory:

| Command                          | Make target                       |
|----------------------------------|-----------------------------------|
| `./kotlin do build`              | `make intellij-plugin`            |
| `./kotlin do verify`             | `make intellij-plugin-verify`     |
| `./kotlin do smoke`              | `make intellij-plugin-smoke`      |
| `./kotlin do ktlint`             | `make intellij-plugin-ktlint`     |
| `./kotlin do ktlintFix`          | `make intellij-plugin-ktlint-fix` |

The `kotlin` wrapper script self-provisions the toolchain on first run; the
distribution is cached under `~/.cache/JetBrains/Kotlin/` (or
`~/Library/Caches/JetBrains/Kotlin/` on macOS).

## Runtime settings

Configure the plugin under **Settings → Tools → Tally** (`TallyConfigurable`).
Settings persist via `TallySettingsService` and cover the executable path,
import strategy, unsafe-fix toggle, and configuration override.

The plugin always launches `tally lsp --stdio`. For trusted projects, executable
auto-resolution checks PATH first, then the project SDK interpreter directory,
then project virtualenv locations (`.venv`/`venv`), then bundled fallback.

## Build inputs

Pinned versions live in `intellij-plugin/module.yaml`:

- IntelliJ Platform Maven artifacts under `dependencies:` — these are what the
  Kotlin Toolchain compiles against. Bump the build number on every line in
  lockstep (they share the same IDE build); use a version that exists in
  `https://www.jetbrains.com/intellij-repository/releases`.
- The `tally-build` plugin settings block — IDE archive URL/SHA (used by the
  plugin verifier and the smoke check), plugin verifier release URL, ktlint
  version, plugin metadata.

Whatever build the IntelliJ Platform Maven deps target should also be the build
behind the IDEA Community archive in `ideArchiveUrl`, so the verifier exercises
the plugin against the same platform it was compiled against.

## Upgrading the Kotlin Toolchain wrapper

The `kotlin` / `kotlin.bat` scripts in this directory are the committed wrapper
that pins the Kotlin Toolchain version (and its SHA-256 chain of trust). To bump
it, use the toolchain's built-in updater rather than editing the version/SHA by
hand:

```bash
cd _integrations/intellij-tally
./kotlin update                              # latest stable
./kotlin update --target-version=0.12.0      # specific version
git diff kotlin kotlin.bat                   # sanity-check the new pin
make intellij-plugin-verify                  # confirm the build still works
make intellij-plugin-smoke
git commit -m "chore: bump Kotlin Toolchain to vX.Y.Z" kotlin kotlin.bat
```

`./kotlin update` rewrites both wrapper scripts in place — the `kotlin_cli_version=`
and `kotlin_cli_sha256=` lines are regenerated from JetBrains' published metadata,
so you do not compute the SHA yourself. The CI cache key in
`.github/workflows/intellij-plugin.yml` hashes the wrapper alongside
`intellij-plugin/module.yaml`, so a bump invalidates the cache automatically.
