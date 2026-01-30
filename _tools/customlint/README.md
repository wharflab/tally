# Custom Linter Plugin for tally

This directory contains tally-specific linting rules implemented as a golangci-lint module plugin.

## Status

✅ **Code Complete** - The custom linter plugin is fully implemented and tested
⚠️ **Integration Blocked** - Cannot build with golangci-lint 2.8.0 due to upstream checksum issue

## Issue

golangci-lint v2.8.0 has a checksum mismatch in its dependencies:

```text
verifying github.com/MirrexOne/unqueryvet@v1.4.0: checksum mismatch
	downloaded: h1:90SVOet9GPeRFp9gFJx7ysDm/x4r27PErSwd6/rRzA4=
	go.sum:     h1:6KAkqqW2KUnkl9Z0VuTphC3IXRPoFqEkJEtyxxHj5eQ=
```

This prevents `golangci-lint custom` from building a custom binary with our plugin.

**Tracking**: This is an upstream issue in golangci-lint itself, not in our code.

## Implemented Rules

### rulestruct

Checks that rule structs in `internal/rules/` follow tally's conventions:

- Exported `*Rule` structs must have documentation comments
- Rule structs should have configuration fields

**Test**: `go test ./...` (all tests pass ✅)

## Structure

```text
_tools/
├── go.mod                     # Tools module with plugin dependencies
├── go.sum
└── customlint/
    ├── plugin.go              # Plugin registration
    ├── rulestruct.go          # Rule struct analyzer
    ├── rulestruct_test.go     # Test suite
    └── testdata/              # Test fixtures
        └── src/internal/rules/
```

## Usage (When Unblocked)

1. Build custom golangci-lint:

   ```bash
   golangci-lint custom
   ```

2. Use the custom binary:

   ```bash
   bin/custom-golangci-lint run
   ```

3. Or add to Makefile:

   ```makefile
   lint: bin/custom-golangci-lint
   	bin/custom-golangci-lint run
   ```

## Configuration

Defined in `.golangci.yaml`:

```yaml
linters:
  enable:
    - customlint

linters-settings:
  custom:
    customlint:
      type: module
```

And `.custom-gcl.yml`:

```yaml
version: v2.8.0
destination: ./bin
plugins:
  - module: 'github.com/tinovyatkin/tally/_tools'
    import: 'github.com/tinovyatkin/tally/_tools/customlint'
    path: ./_tools
```

## Adding New Rules

1. Create new analyzer file in this directory (e.g., `myrule.go`)
2. Implement using `golang.org/x/tools/go/analysis` framework
3. Register in `plugin.go`:

   ```go
   func (p *plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
       return []*analysis.Analyzer{
           ruleStructAnalyzer,
           myRuleAnalyzer,  // Add here
       }, nil
   }
   ```

4. Add test file: `myrule_test.go`
5. Add test fixtures in `testdata/`
6. Run: `go test ./...`

## References

- Pattern based on [microsoft/typescript-go](https://github.com/microsoft/typescript-go/_tools/customlint)
- [golangci-lint Module Plugins](https://golangci-lint.run/docs/plugins/module-plugins/)
- [golang.org/x/tools/go/analysis](https://pkg.go.dev/golang.org/x/tools/go/analysis)
