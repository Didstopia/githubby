[![CI](https://github.com/Didstopia/githubby/actions/workflows/ci.yml/badge.svg)](https://github.com/Didstopia/githubby/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Didstopia/githubby)](https://goreportcard.com/report/github.com/Didstopia/githubby)

# GitHubby

A multi-purpose CLI utility for interacting with GitHub. Sync repositories locally with Git LFS support, clean up old releases, and more.

## Features

- **Easy authentication** - OAuth device flow or personal access tokens
- **Secure credential storage** - System keychain on macOS/Linux/Windows
- **Sync repositories** - Clone and pull repositories for a user or organization
- **Git LFS support** - Automatic detection and configuration of Git LFS
- **Release cleanup** - Filter and remove old GitHub releases
- **Cross-platform** - Works on Linux, macOS, and Windows

## Installation

### From releases

Download the latest release for your platform from the [releases page](https://github.com/Didstopia/githubby/releases).

### From source

```bash
go install github.com/Didstopia/githubby@latest
```

### Build from source

```bash
git clone https://github.com/Didstopia/githubby.git
cd githubby
make build
```

## Usage

### Authentication

GitHubby supports two authentication methods:

**1. OAuth Device Flow (Recommended)**

The easiest way to authenticate - no need to manually create tokens:

```bash
githubby login
```

This opens your browser to complete authentication. Your token is securely stored in:
- **macOS**: Keychain
- **Linux**: Secret Service (GNOME Keyring/KWallet)
- **Windows**: Credential Manager

**2. Personal Access Token**

For automation or if you prefer manual token management:

```bash
# Via stdin
echo "ghp_your_token" | githubby login --with-token

# Via environment variable
export GITHUB_TOKEN=ghp_your_token

# Via command line flag
githubby sync --token ghp_your_token ...
```

**Other auth commands:**

```bash
githubby auth status    # Check authentication status
githubby logout         # Remove stored credentials
```

### Global Flags

```
--token, -t     GitHub API token (overrides stored token)
--verbose, -v   Enable verbose output
--dry-run, -D   Simulate operations without making changes
```

### Sync Command

Sync GitHub repositories to a local directory.

```bash
# Sync all repositories for a user (uses stored token)
githubby sync --user <username> --target ~/repos

# Sync all repositories for an organization
githubby sync --org <orgname> --target ~/repos

# Include private repositories
githubby sync --user <username> --target ~/repos --include-private

# Filter repositories with patterns
githubby sync --user <username> --target ~/repos \
  --include "myproject-*" \
  --exclude "*-archive"

# Dry run (show what would be done)
githubby sync --user <username> --target ~/repos --dry-run
```

**Flags:**
- `--user, -u` - GitHub username to sync repositories from
- `--org, -o` - GitHub organization to sync repositories from
- `--target, -T` - Target directory for synced repositories (required)
- `--include-private, -p` - Include private repositories
- `--include, -i` - Include repositories matching pattern (can be specified multiple times)
- `--exclude, -e` - Exclude repositories matching pattern (can be specified multiple times)

### Clean Command

Filter and remove old GitHub releases.

```bash
# Remove releases older than 30 days (uses stored token)
githubby clean --repository owner/repo --filter-days 30

# Keep only the 10 most recent releases
githubby clean --repository owner/repo --filter-count 10

# Combine filters (releases must match both)
githubby clean --repository owner/repo --filter-days 30 --filter-count 10

# Dry run (show what would be deleted)
githubby clean --repository owner/repo --filter-days 30 --dry-run
```

**Flags:**
- `--repository, -r` - Target repository in `owner/repo` format (required)
- `--filter-days, -d` - Remove releases older than N days
- `--filter-count, -c` - Keep only the N most recent releases

### Version Command

Print version information.

```bash
githubby version
```

## Configuration

GitHubby looks for a configuration file at `~/.githubby.yaml`. Command line flags override config file values.

**Token resolution priority:**
1. `--token` flag
2. `GITHUB_TOKEN` environment variable
3. Stored token (keychain or config file)

Example configuration:

```yaml
# Global settings
verbose: false
dry-run: false

# Clean command defaults
repository: ""
filter-days: -1
filter-count: -1

# Sync command defaults
user: ""
org: ""
target: ""
include-private: false
include: []
exclude: []
```

### Environment Variables

You can set the token via environment variable:

```bash
export GITHUB_TOKEN=ghp_your_token_here
```

## Development

### Prerequisites

- Go 1.22 or later
- Git

### Build

```bash
make build       # Build binary
make test        # Run tests with race detection
make lint        # Run linters
make clean       # Clean build artifacts
```

### Project Structure

```
githubby/
├── cmd/githubby/main.go      # Entry point
├── internal/
│   ├── auth/                 # Authentication (OAuth, keychain storage)
│   ├── cli/                  # Cobra commands
│   ├── config/               # Configuration management
│   ├── errors/               # Custom error types
│   ├── git/                  # Git and LFS operations
│   ├── github/               # GitHub API client
│   └── sync/                 # Repository sync logic
├── pkg/util/                 # Public utilities
└── .github/workflows/        # CI/CD
```

### Running Tests

```bash
# Run all tests
make test

# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test -v ./internal/github/...
```

## License

See [LICENSE](LICENSE).
