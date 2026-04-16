# Tally for Zed

[Tally](https://github.com/wharflab/tally) is a fast, configurable linter, formatter, and fixer for Dockerfiles and Containerfiles.

This extension brings full tally support to [Zed](https://zed.dev), including:

- Real-time diagnostics (linting)
- Document formatting
- Quick fix code actions
- Fix-all code action
- Syntax highlighting for Dockerfiles (bundled grammar)

## Installation

Search for **Tally** in Zed's extension marketplace (`zed: extensions`) and click **Install**.

This extension includes the Dockerfile grammar and syntax highlighting, so you do not need a separate Dockerfile extension.

## Binary Resolution

The extension locates the `tally` binary using the following order:

1. **Custom path** — `lsp.tally.binary.path` in Zed settings
2. **npm project-local** — detects `tally-cli` in your project's `package.json`
3. **Python venv** — checks `.venv/bin/tally` and `venv/bin/tally`
4. **System PATH** — finds `tally` on your `$PATH` (Homebrew, `go install`, etc.)
5. **Auto-download (npm)** — installs the platform-specific `@wharflab/tally-*` package
6. **Auto-download (GitHub)** — downloads from GitHub releases as a fallback

## Configuration

Add to your Zed `settings.json`:

### Format on Save

```jsonc
{
  "languages": {
    "Dockerfile": {
      "format_on_save": "on",
      "formatter": [
        { "language_server": { "name": "Tally" } }
      ]
    }
  }
}
```

### Custom Binary Path

```jsonc
{
  "lsp": {
    "tally": {
      "binary": {
        "path": "/path/to/tally",
        "arguments": ["lsp", "--stdio"]
      }
    }
  }
}
```

### LSP Settings

```jsonc
{
  "lsp": {
    "tally": {
      "initialization_options": {
        "disablePushDiagnostics": true
      },
      "settings": {
        "configurationPreference": "filesystemFirst",
        "fixUnsafe": false
      }
    }
  }
}
```

## Development

To test the extension locally:

1. Open Zed and run `zed: install dev extension`
2. Select the `_integrations/zed-tally/` directory
3. Open a Dockerfile to verify diagnostics, formatting, and code actions

## Links

- [Tally documentation](https://github.com/wharflab/tally)
- [Rule reference](https://tally.wharflab.com/rules/overview)
- [Zed extension docs](https://zed.dev/docs/extensions)
