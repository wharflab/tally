# LSP Protocol Generator

This tool generates Go LSP protocol types for `tally` at:

- `internal/lsp/protocol/lsp_generated.go`

## Usage

From repository root:

```bash
make lsp-protocol
```

or directly:

```bash
bun run tools/lspgen/fetchModel.mts
bun run tools/lspgen/generate.mts
```

## Notes

- `fetchModel.mts` pins and downloads the LSP meta model.
- `generate.mts` is adapted from `microsoft/typescript-go` (`internal/lsp/lsproto/_generate`).
- Generated code is formatted with `gofmt`.
