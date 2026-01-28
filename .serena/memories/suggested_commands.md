# Suggested Commands

## Build & Run

```bash
# Build the project
go build ./...
make build

# Run the CLI
go run . check --help
go run . check Dockerfile
go run . check --max-lines 100 Dockerfile
```

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test ./... -v

# Update snapshots for integration tests
UPDATE_SNAPS=true go test ./internal/integration/...

# Run integration tests with coverage
go test ./internal/integration/...
```

## Coverage Collection

```bash
# Build with coverage
go build -cover -o tally-cover .

# Run tests with coverage directory
mkdir coverage
GOCOVERDIR=coverage go test ./internal/integration/...

# Generate coverage reports
go tool covdata percent -i=coverage
go tool covdata textfmt -i=coverage -o=coverage.txt
go tool cover -html=coverage.txt -o=coverage.html
```

## Linting & Formatting

```bash
# Lint and auto-fix
make lint-fix

# Format TOML files
npx -y -q @taplo/cli format <file>

# Format Markdown
uv tool run rumdl check --fix --config .rumdl.toml --output-format concise <file>

# Check YAML
uvx yamllint <file>

# Check spelling
uv tool run typos
```

## Git Hooks

Pre-commit hooks are managed by Lefthook (`.lefthook.yml`):

- Go linting (golangci-lint)
- TOML formatting (taplo)
- Markdown linting (rumdl)
- YAML linting (yamllint)
- Spell checking (typos)
- Commit message validation (commitlint)

## Darwin-Specific Notes

Standard Unix commands work on Darwin (macOS). The project uses `uv` for Python tool management and `npx` for Node.js tools.
