# GitHubby

[![CI](https://github.com/Didstopia/githubby/actions/workflows/ci.yml/badge.svg)](https://github.com/Didstopia/githubby/actions/workflows/ci.yml)
[![Security](https://github.com/Didstopia/githubby/actions/workflows/security.yml/badge.svg)](https://github.com/Didstopia/githubby/actions/workflows/security.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Didstopia/githubby)](https://goreportcard.com/report/github.com/Didstopia/githubby)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Didstopia/githubby)](https://github.com/Didstopia/githubby)
[![License](https://img.shields.io/github/license/Didstopia/githubby)](https://github.com/Didstopia/githubby/blob/master/LICENSE)
[![Release](https://img.shields.io/github/v/release/Didstopia/githubby)](https://github.com/Didstopia/githubby/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/Didstopia/githubby/total)](https://github.com/Didstopia/githubby/releases)

A powerful CLI and TUI utility for syncing GitHub repositories locally. Features an interactive setup wizard, fast incremental sync, Git LFS support, and release management.

---

## Quick Start

**1. Download** the latest release for your platform:

> **[Download GitHubby](https://github.com/Didstopia/githubby/releases/latest)** - Linux, macOS, Windows (amd64 & arm64)

**2. Run it:**

```bash
./githubby
```

That's it! The interactive setup wizard will guide you through:
1. GitHub authentication (opens browser for OAuth)
2. Sync configuration
3. Dashboard

---

## Features

- **Interactive TUI** - Beautiful terminal interface with setup wizard and dashboard
- **Fast Sync** - Incremental sync skips unchanged repos (seconds, not minutes)
- **Parallel Processing** - 4 concurrent workers for faster synchronization
- **Full Branch Backup** - Fetches all branches, not just the default
- **Git LFS Support** - Automatic detection and configuration
- **Secure Auth** - OAuth device flow with system keychain storage
- **Release Cleanup** - Filter and remove old GitHub releases
- **Auto-Update** - Automatic updates on launch with seamless restart
- **Cross-Platform** - Linux, macOS, and Windows

---

## Installation

### Download Binary (Recommended)

Download the latest release for your platform from the **[releases page](https://github.com/Didstopia/githubby/releases/latest)**:

| Platform | Architecture | Download |
|----------|--------------|----------|
| Linux | amd64 | `githubby_*_linux_amd64.tar.gz` |
| Linux | arm64 | `githubby_*_linux_arm64.tar.gz` |
| macOS | Intel | `githubby_*_darwin_amd64.tar.gz` |
| macOS | Apple Silicon | `githubby_*_darwin_arm64.tar.gz` |
| Windows | amd64 | `githubby_*_windows_amd64.zip` |
| Windows | arm64 | `githubby_*_windows_arm64.zip` |

### Go Install

If you have Go installed:

```bash
go install github.com/Didstopia/githubby@latest
```

### Build from Source

```bash
git clone https://github.com/Didstopia/githubby.git
cd githubby
make build
```

---

## Usage

### Interactive Mode (Recommended)

Just run `githubby` to launch the TUI:

```bash
githubby
```

**First run** opens the Setup Wizard:
- Choose authentication method (OAuth recommended)
- Browser opens for GitHub authorization
- Configure default sync directory and username
- Press Enter to open the Dashboard

**Dashboard** lets you:
- Create and manage sync profiles (user or organization)
- Sync individual profiles or all at once
- View sync history and status
- Access release cleanup tools

### CLI Mode (Automation)

For scripts and automation, use CLI flags directly:

```bash
# Sync all repositories for a user
githubby sync --user <username> --target ~/repos

# Sync an organization's repositories
githubby sync --org <orgname> --target ~/repos

# Include private repositories
githubby sync --user <username> --target ~/repos --include-private

# Filter repositories
githubby sync --user <username> --target ~/repos \
  --include "myproject-*" \
  --exclude "*-archive"

# Dry run (preview without changes)
githubby sync --user <username> --target ~/repos --dry-run

# Verbose output (shows fast-sync decisions)
githubby sync --user <username> --target ~/repos --verbose
```

### Authentication Commands

```bash
githubby login              # OAuth device flow (opens browser)
githubby login --with-token # Use personal access token from stdin
githubby auth status        # Check authentication status
githubby logout             # Remove stored credentials
```

### Release Cleanup

```bash
# Remove releases older than 30 days
githubby clean --repository owner/repo --filter-days 30

# Keep only the 10 most recent releases
githubby clean --repository owner/repo --filter-count 10

# Dry run
githubby clean --repository owner/repo --filter-days 30 --dry-run
```

### Updates

GitHubby automatically keeps itself up to date:

**Automatic updates on launch**: When you run any command (CLI or TUI), GitHubby checks for updates and automatically installs them before proceeding. The app restarts seamlessly after updating:

```
Checking for updates...
Updating to v1.1.0...
Update complete! Restarting...
```

**TUI update flow**: The TUI shows a blocking modal during startup update, then restarts automatically. After launch, you can also press `u` to update if a newer version becomes available.

**Manual update commands**:

```bash
githubby update           # Check and install updates
githubby update --check   # Check only, don't install
```

**Note**: Dev builds and the `update`, `version`, and `help` commands skip auto-update to prevent loops.

---

## How Fast Sync Works

GitHubby uses smart timestamp comparison to skip unchanged repositories:

1. **Single API call** fetches all repo metadata including `pushed_at` timestamps
2. **Local check** compares against `.git/FETCH_HEAD` modification time
3. **Skip fetch** if no pushes occurred since last sync

**Result**: Syncing 100+ repos takes ~2-10 seconds when most are unchanged, compared to minutes with traditional full-fetch approaches.

Use `--verbose` to see fast-sync decisions:
```
[fast-sync] owner/repo: skipping fetch (up-to-date, pushed_at=..., last_fetch=...)
```

---

## Configuration

Config file: `~/.githubby.yaml`

```yaml
# Global settings
verbose: false
dry-run: false

# Sync defaults
user: ""
org: ""
target: ""
include-private: false
include: []
exclude: []

# Clean defaults
repository: ""
filter-days: -1
filter-count: -1
```

**Token resolution priority:**
1. `--token` flag
2. `GITHUB_TOKEN` environment variable
3. System keychain (OAuth tokens)
4. Config file

---

## Global Flags

```
--token, -t     GitHub API token (overrides stored token)
--verbose, -v   Enable verbose output
--dry-run, -D   Simulate operations without making changes
```

---

## Development

### Prerequisites

- Go 1.24+
- Git

### Build & Test

```bash
make build    # Build binary
make test     # Run tests with race detection
make lint     # Run linters
make clean    # Clean build artifacts
```

### Project Structure

```
githubby/
├── cmd/githubby/main.go      # Entry point
├── internal/
│   ├── auth/                 # OAuth & keychain storage
│   ├── cli/                  # Cobra commands
│   ├── config/               # Configuration management
│   ├── git/                  # Git and LFS operations
│   ├── github/               # GitHub API client
│   ├── sync/                 # Repository sync logic
│   ├── state/                # TUI state management
│   ├── tui/                  # Terminal UI (Bubble Tea)
│   │   └── screens/          # TUI screens (onboarding, dashboard, etc.)
│   └── update/               # Auto-update functionality
└── .github/workflows/        # CI/CD
```

---

## Contributing

Contributions are welcome!

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Please ensure your code passes `make lint` and `make test` before submitting.

---

## License

MIT License - see [LICENSE](LICENSE) for details.
