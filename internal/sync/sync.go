// Package sync provides repository synchronization functionality
package sync

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	gh "github.com/google/go-github/v86/github"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

// ProgressStatus represents the status of a sync operation
type ProgressStatus int

const (
	// ProgressPending indicates the repo is queued for sync
	ProgressPending ProgressStatus = iota
	// ProgressInProgress indicates the repo is currently being synced
	ProgressInProgress
	// ProgressCloned indicates the repo was cloned
	ProgressCloned
	// ProgressUpdated indicates the repo was updated
	ProgressUpdated
	// ProgressUpToDate indicates the repo is already up-to-date (fast check)
	ProgressUpToDate
	// ProgressSkipped indicates the repo was skipped
	ProgressSkipped
	// ProgressFailed indicates the repo sync failed
	ProgressFailed
)

// ProgressCallback is called to report sync progress
// repoName is the full repository name (owner/repo)
// status is the current status
// message provides additional context (e.g., error message)
type ProgressCallback func(repoName string, status ProgressStatus, message string)

// Options configures the sync operation
type Options struct {
	// Target directory for synced repositories
	Target string

	// Include patterns for repository names (glob-style)
	Include []string

	// Exclude patterns for repository names (glob-style)
	Exclude []string

	// IncludePrivate includes private repositories
	IncludePrivate bool

	// DryRun simulates the sync without making changes
	DryRun bool

	// Verbose enables verbose output
	Verbose bool

	// OnProgress is called to report sync progress (optional)
	OnProgress ProgressCallback

	// SkipArchiveDetection skips the detectArchived() call which walks the entire
	// target directory. This is useful when syncing single repos (e.g., TUI worker)
	// where archive detection should be done once at the end, not per-repo.
	SkipArchiveDetection bool

	// Submodules checks out submodules (recursively) after each clone or fetch
	Submodules bool

	// Wikis clones or fetches each repo's wiki into <owner>/<repo>.wiki
	Wikis bool
}

// Result represents the result of a sync operation
type Result struct {
	// Cloned repositories
	Cloned []string

	// Updated repositories (pulled)
	Updated []string

	// UpToDate repositories (already current, no pull needed)
	UpToDate []string

	// Skipped repositories (filtered out)
	Skipped []string

	// Failed repositories with errors
	Failed map[string]error

	// Archived repositories (exist locally but not on remote - preserved for backup)
	Archived []string

	// Warnings are non-fatal problems (e.g. a submodule that failed to check out)
	Warnings []string

	// Wikis that were cloned or fetched
	Wikis []string

	// Gists that were cloned or fetched
	Gists []string
}

// NewResult creates a new sync result
func NewResult() *Result {
	return &Result{
		Cloned:   make([]string, 0),
		Updated:  make([]string, 0),
		UpToDate: make([]string, 0),
		Skipped:  make([]string, 0),
		Failed:   make(map[string]error),
		Archived: make([]string, 0),
		Warnings: make([]string, 0),
		Wikis:    make([]string, 0),
		Gists:    make([]string, 0),
	}
}

// warn records a non-fatal problem for a repo
func (r *Result) warn(repoName, format string, args ...any) {
	r.Warnings = append(r.Warnings, repoName+": "+fmt.Sprintf(format, args...))
}

// Syncer handles repository synchronization
type Syncer struct {
	ghClient github.Client
	git      *git.Git
	lfs      *git.LFS
	opts     *Options
}

// New creates a new Syncer
func New(ghClient github.Client, g *git.Git, opts *Options) *Syncer {
	return &Syncer{
		ghClient: ghClient,
		git:      g,
		lfs:      git.NewLFS(g),
		opts:     opts,
	}
}

// SyncUserRepos syncs all repositories for a user
func (s *Syncer) SyncUserRepos(ctx context.Context, username string) (*Result, error) {
	listOpts := &github.ListOptions{
		IncludePrivate: s.opts.IncludePrivate,
	}

	repos, err := s.ghClient.ListUserRepos(ctx, username, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list user repos: %w", err)
	}

	return s.syncRepos(ctx, repos)
}

// SyncOrgRepos syncs all repositories for an organization
func (s *Syncer) SyncOrgRepos(ctx context.Context, org string) (*Result, error) {
	listOpts := &github.ListOptions{
		IncludePrivate: s.opts.IncludePrivate,
	}

	repos, err := s.ghClient.ListOrgRepos(ctx, org, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list org repos: %w", err)
	}

	return s.syncRepos(ctx, repos)
}

// Account is everything one token's account owns: its own repos plus its orgs' repos
type Account struct {
	// Login is the user whose repos were listed
	Login string
	// Orgs are the orgs the token's account belongs to
	Orgs []string
	// Repos of the user and of every org that could be listed
	Repos []*gh.Repository
	// Failed holds orgs whose repos could not be listed, keyed by org name
	Failed map[string]error
}

// ListAccount returns the repos of a user plus every org the token's account
// belongs to. An empty username means the token's own account. An org whose repos
// can't be listed lands in Failed; the rest still come back.
func (s *Syncer) ListAccount(ctx context.Context, username string) (*Account, error) {
	if username == "" {
		user, err := s.ghClient.GetAuthenticatedUser(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get authenticated user: %w", err)
		}
		username = user.GetLogin()
	}

	listOpts := &github.ListOptions{IncludePrivate: s.opts.IncludePrivate}

	repos, err := s.ghClient.ListUserRepos(ctx, username, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list user repos: %w", err)
	}

	orgs, err := s.ghClient.ListUserOrgs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list orgs: %w", err)
	}

	account := &Account{Login: username, Failed: make(map[string]error)}
	seen := make(map[string]bool)
	for _, repo := range repos {
		seen[repo.GetFullName()] = true
	}

	for _, org := range orgs {
		account.Orgs = append(account.Orgs, org.GetLogin())
		orgRepos, err := s.ghClient.ListOrgRepos(ctx, org.GetLogin(), listOpts)
		if err != nil {
			account.Failed[org.GetLogin()] = fmt.Errorf("failed to list org repos: %w", err)
			continue
		}
		for _, repo := range orgRepos {
			if !seen[repo.GetFullName()] {
				seen[repo.GetFullName()] = true
				repos = append(repos, repo)
			}
		}
	}

	account.Repos = repos
	return account, nil
}

// SyncRepos syncs the given repositories, with archive detection over all of them
func (s *Syncer) SyncRepos(ctx context.Context, repos []*gh.Repository) (*Result, error) {
	return s.syncRepos(ctx, repos)
}

// Matches reports whether a repo passes the include/exclude filters
func (s *Syncer) Matches(repo *gh.Repository) bool {
	return s.shouldSync(repo.GetName())
}

// SyncRepo syncs a single repository
func (s *Syncer) SyncRepo(ctx context.Context, owner, repo string) (*Result, error) {
	repository, err := s.ghClient.GetRepository(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}

	return s.syncRepos(ctx, []*gh.Repository{repository})
}

// SyncRepoWithData syncs a single repository using pre-fetched data.
// This avoids redundant API calls when the repository data is already available.
func (s *Syncer) SyncRepoWithData(ctx context.Context, repo *gh.Repository) (*Result, error) {
	return s.syncRepos(ctx, []*gh.Repository{repo})
}

// SyncSelectedRepos syncs only the given "owner/repo" names
func (s *Syncer) SyncSelectedRepos(ctx context.Context, fullNames []string) (*Result, error) {
	failed := make(map[string]error)
	var repos []*gh.Repository

	for _, fullName := range fullNames {
		owner, name, ok := strings.Cut(fullName, "/")
		if !ok {
			failed[fullName] = fmt.Errorf("invalid repository name %q, want owner/repo", fullName)
			continue
		}
		repo, err := s.ghClient.GetRepository(ctx, owner, name)
		if err != nil {
			failed[fullName] = err
			continue
		}
		repos = append(repos, repo)
	}

	result, err := s.syncRepos(ctx, repos)
	if result != nil {
		for name, ferr := range failed {
			result.Failed[name] = ferr
		}
	}
	return result, err
}

func (s *Syncer) syncRepos(ctx context.Context, repos []*gh.Repository) (*Result, error) {
	result := NewResult()

	// Ensure target directory exists
	if !s.opts.DryRun {
		if err := os.MkdirAll(s.opts.Target, 0755); err != nil {
			return nil, fmt.Errorf("failed to create target directory: %w", err)
		}
	}

	return s.syncReposSequential(ctx, repos, result)
}

func (s *Syncer) syncReposSequential(ctx context.Context, repos []*gh.Repository, result *Result) (*Result, error) {
	for _, repo := range repos {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		s.syncSingleRepo(ctx, repo, result)
	}

	// Detect archived repos (exist locally but not on remote)
	// Skip if SkipArchiveDetection is set (e.g., TUI syncs single repos and handles this separately)
	if !s.opts.SkipArchiveDetection {
		result.Archived = s.detectArchived(repos)
	}

	return result, nil
}

func (s *Syncer) syncSingleRepo(ctx context.Context, repo *gh.Repository, result *Result) {
	repoName := repo.GetFullName()

	// Check include/exclude filters
	if !s.shouldSync(repo.GetName()) {
		if s.opts.Verbose {
			fmt.Printf("Skipping %s (filtered)\n", repoName)
		}
		s.reportProgress(repoName, ProgressSkipped, "filtered")
		result.Skipped = append(result.Skipped, repoName)
		return
	}

	// Determine local path
	localPath := filepath.Join(s.opts.Target, repo.GetOwner().GetLogin(), repo.GetName())

	if s.opts.Verbose {
		fmt.Printf("Processing %s -> %s\n", repoName, localPath)
	}

	if s.opts.DryRun {
		if s.git.IsGitRepo(localPath) {
			if s.opts.Verbose {
				fmt.Printf("[DRY RUN] Would update: %s\n", repoName)
			}
			s.reportProgress(repoName, ProgressUpdated, "dry-run")
			result.Updated = append(result.Updated, repoName)
		} else {
			if s.opts.Verbose {
				fmt.Printf("[DRY RUN] Would clone: %s\n", repoName)
			}
			s.reportProgress(repoName, ProgressCloned, "dry-run")
			result.Cloned = append(result.Cloned, repoName)
		}
		return
	}

	// Report progress: starting
	s.reportProgress(repoName, ProgressInProgress, "")

	// Sync the repository
	if s.git.IsGitRepo(localPath) {
		// Pull existing repo
		status, err := s.pullRepo(ctx, repo, localPath)
		if err != nil {
			result.Failed[repoName] = err
			s.reportProgress(repoName, ProgressFailed, err.Error())
			if s.opts.Verbose {
				fmt.Printf("Failed to update %s: %v\n", repoName, err)
			}
		} else {
			if status == ProgressUpToDate {
				result.UpToDate = append(result.UpToDate, repoName)
			} else {
				result.Updated = append(result.Updated, repoName)
			}
			s.afterSync(ctx, repo, localPath, result)
			s.reportProgress(repoName, status, "")
			if s.opts.Verbose {
				if status == ProgressUpToDate {
					fmt.Printf("Up-to-date: %s\n", repoName)
				} else {
					fmt.Printf("Updated: %s\n", repoName)
				}
			}
		}
	} else {
		// Clone new repo
		if err := s.cloneRepo(ctx, repo, localPath); err != nil {
			result.Failed[repoName] = err
			s.reportProgress(repoName, ProgressFailed, err.Error())
			if s.opts.Verbose {
				fmt.Printf("Failed to clone %s: %v\n", repoName, err)
			}
		} else {
			result.Cloned = append(result.Cloned, repoName)
			s.afterSync(ctx, repo, localPath, result)
			s.reportProgress(repoName, ProgressCloned, "")
			if s.opts.Verbose {
				fmt.Printf("Cloned: %s\n", repoName)
			}
		}
	}
	// The wiki is its own git repo, so it syncs even if the main repo failed
	if s.opts.Wikis && repo.GetHasWiki() {
		s.syncWiki(ctx, repo, result)
	}
}

// WikiURL returns the clone URL of a repo's wiki
func WikiURL(repo *gh.Repository) string {
	return strings.TrimSuffix(repo.GetCloneURL(), ".git") + ".wiki.git"
}

// WikiPath returns where a repo's wiki lives under target
func WikiPath(target string, repo *gh.Repository) string {
	return filepath.Join(target, repo.GetOwner().GetLogin(), repo.GetName()+".wiki")
}

// syncWiki clones or fetches a repo's wiki. GitHub reports has_wiki even when the
// wiki was never created, so a missing wiki is skipped quietly.
func (s *Syncer) syncWiki(ctx context.Context, repo *gh.Repository, result *Result) {
	name := repo.GetFullName() + ".wiki"
	localPath := WikiPath(s.opts.Target, repo)
	url := WikiURL(repo)

	var err error
	if s.git.IsGitRepo(localPath) {
		err = withGitRetry(ctx, DefaultGitRetryConfig(),
			func() error { return s.git.FetchAll(ctx, localPath) },
			nil, isTransientGitError)
	} else {
		err = withGitRetry(ctx, DefaultGitRetryConfig(),
			func() error { return s.git.Clone(ctx, url, localPath) },
			func() error { return os.RemoveAll(localPath) },
			isTransientGitError)
	}

	switch {
	case err == nil:
		result.Wikis = append(result.Wikis, name)
	case IsRepoNotFound(err):
		if s.opts.Verbose {
			fmt.Printf("No wiki for %s\n", repo.GetFullName())
		}
	default:
		result.Failed[name] = err
	}
}

// IsRepoNotFound reports whether a git error means the remote repo does not exist
func IsRepoNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")
}

// afterSync runs the optional backup steps for a repo whose git data synced fine.
// Failures here are warnings: the repo itself is safe.
func (s *Syncer) afterSync(ctx context.Context, repo *gh.Repository, localPath string, result *Result) {
	if s.opts.Submodules {
		if err := s.git.SubmoduleUpdate(ctx, localPath); err != nil {
			result.warn(repo.GetFullName(), "submodules: %v", err)
		}
	}
}

// detectArchived finds local git repos that no longer exist on remote
// These are "archived" repos - preserved locally for backup purposes
func (s *Syncer) detectArchived(remoteRepos []*gh.Repository) []string {
	// Build set of expected repo paths from remote
	remoteSet := make(map[string]bool)
	for _, repo := range remoteRepos {
		// Use owner/repo path format
		path := filepath.Join(repo.GetOwner().GetLogin(), repo.GetName())
		remoteSet[filepath.ToSlash(path)] = true
		if repo.GetHasWiki() {
			remoteSet[filepath.ToSlash(path)+".wiki"] = true
		}
	}

	// Scan local target directory for git repos
	var archived []string

	// Target directory might not exist yet
	if _, err := os.Stat(s.opts.Target); os.IsNotExist(err) {
		return archived
	}

	_ = filepath.WalkDir(s.opts.Target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Look for .git directories
		if d.Name() == ".git" && d.IsDir() {
			repoPath := filepath.Dir(path)
			relPath, err := filepath.Rel(s.opts.Target, repoPath)
			if err != nil {
				return nil
			}

			// Check if this repo exists on remote
			// Normalize path separators for cross-platform
			normalizedPath := filepath.ToSlash(relPath)
			if !remoteSet[normalizedPath] && !isGistPath(normalizedPath) {
				archived = append(archived, normalizedPath)
			}

			return fs.SkipDir // Don't recurse into .git
		}

		return nil
	})

	return archived
}

// isGistPath reports whether a slash path relative to target is <user>/gists/<id>
func isGistPath(rel string) bool {
	parts := strings.Split(rel, "/")
	return len(parts) == 3 && parts[1] == "gists"
}

// reportProgress calls the progress callback if set
func (s *Syncer) reportProgress(repoName string, status ProgressStatus, message string) {
	if s.opts.OnProgress != nil {
		s.opts.OnProgress(repoName, status, message)
	}
}

func (s *Syncer) cloneRepo(ctx context.Context, repo *gh.Repository, localPath string) error {
	// Create parent directory
	parentDir := filepath.Dir(localPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Clone the repository using HTTPS (token auth is handled by Git instance)
	cloneURL := repo.GetCloneURL()

	// Retry clone on transient failures (e.g., Dropbox file locking on Windows)
	if err := withGitRetry(ctx, DefaultGitRetryConfig(),
		func() error {
			return s.git.Clone(ctx, cloneURL, localPath)
		},
		func() error {
			// Remove partial clone directory before retrying
			return os.RemoveAll(localPath)
		},
		isTransientGitError,
	); err != nil {
		return err
	}

	// Handle LFS if needed (non-fatal - repo still usable without LFS objects)
	if s.lfs.RepoUsesLFS(localPath) {
		if s.opts.Verbose {
			fmt.Printf("Repository uses LFS, pulling LFS objects...\n")
		}
		if err := s.lfs.EnsureLFS(ctx); err != nil {
			// LFS not available - warn but don't fail the clone
			if s.opts.Verbose {
				fmt.Printf("Warning: LFS not available (%v). Large files will be pointer files.\n", err)
			}
			// Continue without LFS - repo is still cloned, just without LFS objects
			return nil
		}
		if err := s.lfs.Pull(ctx, localPath); err != nil {
			// LFS pull failed - warn but don't fail
			if s.opts.Verbose {
				fmt.Printf("Warning: Failed to pull LFS objects: %v\n", err)
			}
		}
	}

	return nil
}

// pullRepo pulls updates for an existing repository.
// Returns ProgressUpToDate if already current, ProgressUpdated if pulled, or error.
func (s *Syncer) pullRepo(ctx context.Context, repo *gh.Repository, localPath string) (ProgressStatus, error) {
	// Fast check: compare repo's pushed_at timestamp with our last fetch time
	// This works for ALL branches, not just the default branch
	if repo.PushedAt != nil && !repo.PushedAt.IsZero() {
		lastFetch, err := s.git.GetLastFetchTime(localPath)
		if err == nil && !lastFetch.IsZero() {
			// Add a buffer (2 seconds) to handle clock skew between GitHub and local system
			// Up-to-date if pushed_at is not after (lastFetch + buffer)
			buffer := 2 * time.Second
			// Use .GetTime() to extract time.Time for comparison with time.Time
			pushedAtTime := repo.PushedAt.GetTime()
			if !pushedAtTime.After(lastFetch.Add(buffer)) {
				// No pushes since our last fetch (within tolerance), we're up-to-date
				if s.opts.Verbose {
					fmt.Printf("[fast-sync] %s: skipping fetch (up-to-date, pushed_at=%v, last_fetch=%v)\n",
						repo.GetFullName(), repo.PushedAt.Time, lastFetch)
				}
				return ProgressUpToDate, nil
			}
			if s.opts.Verbose {
				fmt.Printf("[fast-sync] %s: fetch needed (pushed_at=%v > last_fetch=%v + %v buffer)\n",
					repo.GetFullName(), repo.PushedAt.Time, lastFetch, buffer)
			}
		} else if s.opts.Verbose {
			if err != nil {
				fmt.Printf("[fast-sync] %s: fetch needed (FETCH_HEAD not found: %v)\n", repo.GetFullName(), err)
			} else {
				fmt.Printf("[fast-sync] %s: fetch needed (last_fetch is zero)\n", repo.GetFullName())
			}
		}
	} else if s.opts.Verbose {
		if repo.PushedAt == nil {
			fmt.Printf("[fast-sync] %s: fetch needed (pushed_at is nil)\n", repo.GetFullName())
		} else {
			fmt.Printf("[fast-sync] %s: fetch needed (pushed_at is zero)\n", repo.GetFullName())
		}
	}

	// Fetch all branches from all remotes (for complete backup of all branches)
	// Using fetch instead of pull so we update all remote-tracking branches
	// without modifying the working directory
	// Retry on transient failures (e.g., Dropbox file locking on Windows)
	if err := withGitRetry(ctx, DefaultGitRetryConfig(),
		func() error {
			return s.git.FetchAll(ctx, localPath)
		},
		nil, // No cleanup needed for fetch
		isTransientGitError,
	); err != nil {
		return ProgressFailed, fmt.Errorf("fetch failed: %w", err)
	}

	// Handle LFS if needed (non-fatal - repo still usable without LFS objects)
	if s.lfs.RepoUsesLFS(localPath) {
		if s.opts.Verbose {
			fmt.Printf("Repository uses LFS, pulling LFS objects...\n")
		}
		if err := s.lfs.EnsureLFS(ctx); err != nil {
			// LFS not available - warn but don't fail the pull
			if s.opts.Verbose {
				fmt.Printf("Warning: LFS not available (%v). Large files will be pointer files.\n", err)
			}
			// Continue without LFS
			return ProgressUpdated, nil
		}
		if err := s.lfs.Pull(ctx, localPath); err != nil {
			// LFS pull failed - warn but don't fail
			if s.opts.Verbose {
				fmt.Printf("Warning: Failed to pull LFS objects: %v\n", err)
			}
		}
	}

	return ProgressUpdated, nil
}

func (s *Syncer) shouldSync(repoName string) bool {
	// Check exclude patterns first
	for _, pattern := range s.opts.Exclude {
		if matchGlob(pattern, repoName) {
			return false
		}
	}

	// If no include patterns, include everything
	if len(s.opts.Include) == 0 {
		return true
	}

	// Check include patterns
	for _, pattern := range s.opts.Include {
		if matchGlob(pattern, repoName) {
			return true
		}
	}

	return false
}

// matchGlob performs simple glob matching
func matchGlob(pattern, name string) bool {
	// Handle simple wildcards
	if pattern == "*" {
		return true
	}

	// Handle contains (*pattern*)
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		middle := strings.Trim(pattern, "*")
		return strings.Contains(name, middle)
	}

	// Handle prefix match (pattern*)
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(name, prefix)
	}

	// Handle suffix match (*pattern)
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(name, suffix)
	}

	// Exact match
	return pattern == name
}
