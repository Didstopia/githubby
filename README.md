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
- **Full Backup** - Submodules, wikis, gists, and issues, pull requests, releases, settings and projects as JSON
- **Backup Check** - `githubby verify` compares a backup against GitHub before you delete anything
- **Secure Auth** - OAuth device flow with system keychain storage
- **Release Cleanup** - Filter and remove old GitHub releases
- **Auto-Update** - Automatic updates on launch with seamless restart
- **Scheduled Sync** - Cron-based recurring sync for unattended operation
- **Profile-Based CLI** - Run saved TUI profiles from the command line
- **Docker Support** - Minimal `scratch` image for containerized sync
- **Cross-Platform** - Linux, macOS, and Windows

---

## Installation

### Homebrew (macOS / Linux)

```bash
brew install Didstopia/tap/githubby
```

### Download Binary

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

### Docker

```bash
docker pull ghcr.io/didstopia/githubby:latest
docker run --rm ghcr.io/didstopia/githubby:latest version
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

### Authentication for Non-Interactive Use

CLI, scheduled, and Docker workflows cannot use the interactive OAuth browser flow. Instead, provide a GitHub personal access token (PAT) via the `GITHUB_TOKEN` environment variable:

1. Go to **[github.com/settings/tokens](https://github.com/settings/tokens)**
2. Click **Generate new token (classic)** (or use a fine-grained token)
3. Select the **`repo`** scope (required for private repositories; public repos work without scopes).
   For a full backup also select `read:org`, `gist`, `read:project` and `admin:org` (see [Full Backup](#full-backup))
4. Copy the token and export it (`GITHUBBY_TOKEN` also works and wins over `GITHUB_TOKEN`):

```bash
export GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Or pass it directly with `--token`:

```bash
githubby sync --token ghp_... --user <username> --target ~/repos
```

For Docker Compose, create a `.env` file next to `docker-compose.yaml`:

```
GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

> **Tip**: The interactive TUI (`githubby login`) stores tokens in the system keychain. CLI and scheduled modes will pick up that stored token automatically if `GITHUB_TOKEN` is not set — but headless environments (Docker, CI) have no keychain, so the environment variable is required.

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

CLI sync also checks out submodules, clones each repo's wiki next to it (`<repo>.wiki`), fetches
the Git LFS objects of every branch, and fetches the head of every pull request into
`refs/pull/<n>/head`, so commits from forks and deleted branches are kept too. It exits with a non-zero code if anything failed.

### Full Backup

Back up your own repositories and every organization you belong to, in one run:

```bash
githubby sync --all-orgs --include-private --gists --metadata --target ~/backup
```

| Flag | What it adds |
|------|--------------|
| `--all-orgs` | Your own repositories plus those of every organization you are a member of |
| `--gists` | Your gists, secret ones too, in `<you>/gists/<id>` with the gist details in `<id>.json` |
| `--metadata` | Issues, pull requests, releases and settings as JSON, see below |

Layout of the target directory:

```
backup/
├── <owner>/<repo>/              # git clone, all branches, pull request heads, submodules, LFS objects
├── <owner>/<repo>.wiki/         # the wiki, if the repo has one
├── <owner>/<repo>.meta/         # --metadata
│   ├── issues.json              # all issues with comments
│   ├── pulls.json               # all pull requests with comments, review comments and reviews
│   ├── releases.json            # plus releases/<tag>/<asset> files
│   ├── discussions.json         # with comments and replies, if discussions are on
│   ├── repo.json, labels.json, milestones.json, topics.json, branch-protection.json,
│   ├── hooks.json, collaborators.json, deploy-keys.json, environments.json, rulesets.json,
│   └── actions-variables.json, actions-secrets.json (names only, GitHub never returns values)
├── <org>.meta/                  # org.json, members, teams, Actions variables, secret names, projects, rulesets
├── <you>.meta/projects-v2.json  # your own projects
├── <you>/gists/<id>/            # --gists
└── verify-report.json           # written by githubby verify
```

Metadata is exported in full on every run. Files are written to a temp file first and then renamed,
so a crash never leaves half a file. Release assets are only downloaded again when the size changed.
When the token lacks a scope for one part (for example webhooks without admin access), that part is
skipped with a warning naming the missing scope; it does not fail the run. Git sync and metadata
export run independently: a failure in one does not stop the other, both show up in the summary and
in the exit code.

Token scopes for a full backup: `repo`, `read:org`, `gist`, `read:project`, `admin:org`.
`githubby login` asks for all of these. `admin:org` is only needed for org secrets, variables,
rulesets and webhooks; it is read here, never written.

#### Checking a Backup

```bash
githubby verify --all-orgs --gists --metadata --target ~/backup
```

`verify` takes the same `--all-orgs`, `--user`, `--org`, `--gists` and `--metadata` flags as `sync` and checks:

- every repository (private ones always included), wiki and gist on GitHub exists locally
- `git fsck` passes for each of them
- every branch on GitHub exists locally at the same commit
- every pull request head (`refs/pull/<n>/head`) on GitHub exists locally at the same commit
- every Git LFS object in the history is present locally with the right content (needs `git-lfs`)
- every submodule (nested ones too) is checked out at the commit the repo records and passes
  `git fsck`; a submodule whose repo was deleted upstream can't be backed up, and fails here
- with `--metadata`: the metadata directory exists and the issue and pull request counts match GitHub
- with `--metadata`: `releases.json` holds as many releases as GitHub has, and every release asset
  is saved with the same size as on GitHub

A check that can't run (network error, missing `git-lfs`, a scope the token lacks) is a failure,
never a skip. The exit code is 0 only when everything is clean. The report, with a timestamp and the
list of every item checked, is written to `<target>/verify-report.json`. Check that a repository is
listed in `items` there before you delete it from GitHub.

### Profile-Based Sync

Run saved TUI profiles directly from the CLI — no interactive mode needed:

```bash
# Sync a specific profile by name
githubby sync --profile "my-profile"

# Sync all saved profiles
githubby sync --all-profiles
```

Profiles are created in the interactive TUI and stored in `~/.githubby/state.yaml`. Each profile saves the sync type (user/org), source, target directory, and filter settings.

### Scheduled Sync

Use `--schedule` with any sync mode to run recurring syncs in the foreground. The schedule uses standard cron syntax:

```bash
# Sync every 6 hours
githubby sync --user <username> --target ~/repos --schedule "0 */6 * * *"

# Sync all profiles every 30 minutes
githubby sync --all-profiles --schedule "@every 30m"

# Sync a profile on a cron schedule
githubby sync --profile "my-profile" --schedule "@hourly"
```

The scheduler runs an immediate sync on start, then follows the cron schedule. Overlapping runs are automatically skipped. Use `Ctrl+C` to stop.

**Supported schedule formats:**
| Format | Example | Description |
|--------|---------|-------------|
| Cron | `0 */6 * * *` | Standard 5-field cron expression |
| `@every` | `@every 30m` | Fixed interval (s, m, h) |
| `@hourly` | `@hourly` | Predefined schedules |
| `@daily` | `@daily` | Once per day at midnight |

### Docker

Run GitHubby in a container for unattended scheduled sync:

```bash
# One-off sync
docker run --rm \
  -e GITHUB_TOKEN=ghp_... \
  -v ~/repos:/repos \
  ghcr.io/didstopia/githubby:latest \
  sync --user <username> --target /repos

# Scheduled sync (runs every 6 hours)
docker run -d --restart unless-stopped \
  -e GITHUB_TOKEN=ghp_... \
  -v ~/repos:/repos \
  ghcr.io/didstopia/githubby:latest \
  sync --user <username> --target /repos --schedule "0 */6 * * *"

# Nightly full backup at 03:00, plus a check on demand
docker run -d --restart unless-stopped \
  -e GITHUBBY_TOKEN=ghp_... \
  -v /mnt/user/Backups/github:/backup \
  ghcr.io/didstopia/githubby:latest \
  sync --all-orgs --include-private --gists --metadata --target /backup --schedule "0 3 * * *"

docker run --rm -e GITHUBBY_TOKEN=ghp_... -v /mnt/user/Backups/github:/backup \
  ghcr.io/didstopia/githubby:latest \
  verify --all-orgs --gists --metadata --target /backup
```

Or use `docker-compose.yaml` for persistent setups:

```yaml
services:
  # Syncs every saved TUI profile every 6 hours (at :00 past every 6th hour).
  sync-all-profiles:
    image: ghcr.io/didstopia/githubby:latest
    command: ["sync", "--all-profiles", "--schedule", "0 */6 * * *"]
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
    volumes:
      - ./repos:/repos                   # local directory for cloned repositories
      - githubby-state:/root/.githubby   # persists profiles & sync history
    restart: unless-stopped

  # Syncs a single user's repositories every 30 minutes.
  sync-user:
    image: ghcr.io/didstopia/githubby:latest
    command: ["sync", "--user", "myuser", "--target", "/repos", "--schedule", "*/30 * * * *"]
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
    volumes:
      - ./repos:/repos
    restart: unless-stopped

volumes:
  githubby-state:
```

A fully commented `docker-compose.yaml` is included in the repository.

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

**Turning updates off**: pass `--no-update` or set `GITHUBBY_NO_UPDATE=1` (CLI and TUI). Inside Docker
updates are always off.

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
2. `GITHUBBY_TOKEN`, then `GITHUB_TOKEN` environment variable
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

- Go 1.26+
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
├── main.go                   # Entry point
├── internal/
│   ├── auth/                 # OAuth & keychain storage
│   ├── cli/                  # Cobra commands
│   ├── config/               # Configuration management
│   ├── git/                  # Git and LFS operations
│   ├── fileutil/             # Atomic file writes
│   ├── github/               # GitHub API client (REST and GraphQL)
│   ├── metadata/             # Metadata export (issues, pulls, releases, settings, projects)
│   ├── schedule/             # Cron-based sync scheduling
│   ├── sync/                 # Repository sync logic
│   ├── state/                # TUI state management
│   ├── tui/                  # Terminal UI (Bubble Tea)
│   │   └── screens/          # TUI screens (onboarding, dashboard, etc.)
│   ├── update/               # Auto-update functionality
│   └── verify/               # Backup check
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
