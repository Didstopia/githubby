# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GitHubby is a Go CLI utility for interacting with GitHub. Features include:
- Syncing GitHub repositories locally (user or organization)
- Git LFS support with automatic detection, installation, and configuration
- Release cleanup with flexible filtering

**Status**: Actively maintained. Go 1.22+, modern dependencies.

## Build Commands

```bash
make deps          # Download dependencies
make build         # Compile binary to ./githubby
make test          # Run tests with race detection and coverage
make lint          # Run go vet and golangci-lint
make install       # Build and install to $GOPATH/bin/
make clean         # Clean build artifacts
make upgrade       # Update dependencies
make tidy          # Run go mod tidy
```

Run directly: `go run main.go`

## Architecture

```
githubby/
├── main.go                   # Entry point (legacy location, calls internal/cli)
├── cmd/githubby/main.go      # Entry point (preferred)
├── internal/
│   ├── auth/                 # Authentication
│   │   ├── auth.go           # Token resolution (flag > env > keychain > config)
│   │   ├── device_flow.go    # OAuth device flow implementation
│   │   └── storage.go        # Secure token storage (keychain + config fallback)
│   ├── cli/                  # Cobra commands
│   │   ├── root.go           # Root command, global flags, signal handling
│   │   ├── login.go          # Login command (OAuth device flow)
│   │   ├── logout.go         # Logout command
│   │   ├── auth_status.go    # Auth status command
│   │   ├── clean.go          # Clean command (release cleanup)
│   │   ├── sync.go           # Sync command (repository sync)
│   │   └── version.go        # Version command
│   ├── config/               # Configuration management
│   │   ├── config.go         # Config struct, load/save
│   │   └── loader.go         # Viper integration
│   ├── errors/               # Custom error types
│   │   └── errors.go         # ValidationError, APIError, AuthError
│   ├── git/                  # Git operations
│   │   ├── git.go            # Clone, pull, fetch operations
│   │   └── lfs.go            # LFS detection, installation, pull
│   ├── github/               # GitHub API
│   │   ├── interfaces.go     # Client interface (for mocking)
│   │   ├── client.go         # Client implementation
│   │   ├── mock_client.go    # Mock client for tests
│   │   └── ratelimit.go      # Rate limiting and retry logic
│   └── sync/                 # Repository sync logic
│       └── sync.go           # Syncer, filtering, clone/pull
├── pkg/util/                 # Public utilities
│   └── repository.go         # Repository validation
├── cmd/                      # Legacy commands (backward compat)
└── .github/workflows/        # CI/CD (GitHub Actions)
```

**Key patterns**:
- **Interface-based design**: `github.Client` interface enables mocking for tests
- **Context support**: All API operations accept `context.Context` for cancellation
- **Graceful shutdown**: Signal handling for SIGINT/SIGTERM
- **Iterative pagination**: No stack overflow risk (replaced recursive calls)
- **Secure defaults**: Config file permissions 0600, URL path encoding
- **Cross-platform auth**: Keychain storage (macOS/Linux/Windows) with config fallback

## Configuration

Config file: `~/.githubby.yaml`

```yaml
# Global settings
verbose: false
dry-run: false
token: ""

# Clean command
repository: ""
filter-days: -1
filter-count: -1

# Sync command
user: ""
org: ""
target: ""
include-private: false
include: []
exclude: []
```

Priority: CLI flags > environment variables > stored token (keychain/config)

## CLI Usage

```bash
# Authentication (recommended for interactive use)
githubby login                              # OAuth device flow - opens browser
githubby login --with-token < token.txt     # Use PAT from stdin
githubby logout                             # Remove stored credentials
githubby auth status                        # Check authentication status

# Sync repositories (uses stored token, or --token flag)
githubby sync --user <username> --target ~/repos
githubby sync --org <orgname> --target ~/repos --include-private
githubby sync --user <username> --target ~/repos --include "prefix-*" --exclude "*-archive"

# Clean releases
githubby clean --repository owner/repo --filter-days 30
githubby clean --repository owner/repo --filter-count 10 --dry-run

# Version
githubby version
```

## Adding New Commands

1. Create file in `internal/cli/` (e.g., `internal/cli/newcmd.go`)
2. Define command with `&cobra.Command{}` and `RunE` function
3. Register in `init()`: `rootCmd.AddCommand(newCmd)`
4. Bind flags to viper for config file support
5. Use `cmd.Context()` for context-aware operations
6. Add tests in `internal/cli/newcmd_test.go`

## Testing

```bash
# All tests
make test

# Specific package
go test -v ./internal/github/...

# With coverage
go test -coverprofile=coverage.txt ./...
```

Tests use mock implementations (`internal/github/mock_client.go`) for isolation.

## CI/CD

- **CI**: `.github/workflows/ci.yml` - tests on Go 1.21/1.22, Linux/macOS/Windows
- **Release**: `.github/workflows/release.yml` - GoReleaser on tag push
- Builds: linux/darwin/windows, amd64/arm64
