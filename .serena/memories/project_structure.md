# Project Structure

```text
.
├── main.go                           # Entry point
├── cmd/tally/cmd/                    # CLI commands (urfave/cli)
│   ├── root.go                       # Root command setup
│   ├── check.go                      # Check subcommand (linting)
│   └── version.go                    # Version subcommand
├── internal/
│   ├── config/                       # Configuration loading (koanf)
│   │   ├── config.go                 # Config struct, loading, cascading discovery
│   │   └── config_test.go
│   ├── dockerfile/                   # Dockerfile parsing (buildkit)
│   │   ├── parser.go
│   │   └── parser_test.go
│   ├── lint/                         # Linting rules
│   │   ├── rules.go
│   │   └── rules_test.go
│   ├── version/
│   │   └── version.go
│   ├── integration/                  # Integration tests (go-snaps)
│   │   ├── integration_test.go
│   │   ├── __snapshots__/            # Snapshot files
│   │   └── testdata/                 # Test fixtures (each in own directory)
│   └── testutil/                     # Test utilities
├── packaging/
│   ├── pack.rb                       # Packaging orchestration
│   ├── npm/                          # npm package (@contino/tally)
│   ├── pypi/                         # Python package (tally-cli)
│   └── rubygems/                     # Ruby gem (tally-cli)
└── README.md
```

## Key Files

- `main.go` - Entry point
- `internal/config/config.go` - Config system with cascading discovery
- `internal/lint/rules.go` - Linting rule implementations
- `cmd/tally/cmd/check.go` - Main check command with CLI flags
- `.lefthook.yml` - Git hooks configuration
- `.golangci.yaml` - Go linter configuration
- `.goreleaser.yml` - Multi-platform release configuration

## Test Organization

- Test fixtures in separate directories under `testdata/` to support future context-aware features (dockerignore, config files, etc.)
- Integration tests use snapshot testing for output validation
