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

	gh "github.com/google/go-github/v68/github"

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

	// Concurrency sets the number of parallel sync operations (default: 1)
	Concurrency int
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
	}
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

// syncResult holds the result of syncing a single repo
type syncResult struct {
	repoName string
	status   ProgressStatus
	err      error
}

func (s *Syncer) syncRepos(ctx context.Context, repos []*gh.Repository) (*Result, error) {
	result := NewResult()

	// Ensure target directory exists
	if !s.opts.DryRun {
		if err := os.MkdirAll(s.opts.Target, 0755); err != nil {
			return nil, fmt.Errorf("failed to create target directory: %w", err)
		}
	}

	// Default concurrency to 1 (sequential), max 8 to avoid rate limits
	concurrency := s.opts.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 8 {
		concurrency = 8
	}

	// For sequential processing, use the simple loop
	if concurrency == 1 {
		return s.syncReposSequential(ctx, repos, result)
	}

	// Parallel processing with worker pool
	return s.syncReposParallel(ctx, repos, result, concurrency)
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
	result.Archived = s.detectArchived(repos)

	return result, nil
}

func (s *Syncer) syncReposParallel(ctx context.Context, repos []*gh.Repository, result *Result, concurrency int) (*Result, error) {
	// Create channels
	jobs := make(chan *gh.Repository, len(repos))
	results := make(chan syncResult, len(repos))

	// Create a child context for cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start workers
	for i := 0; i < concurrency; i++ {
		go s.syncWorker(ctx, jobs, results)
	}

	// Send jobs
	for _, repo := range repos {
		jobs <- repo
	}
	close(jobs)

	// Collect results
	for i := 0; i < len(repos); i++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case res := <-results:
			switch res.status {
			case ProgressCloned:
				result.Cloned = append(result.Cloned, res.repoName)
			case ProgressUpdated:
				result.Updated = append(result.Updated, res.repoName)
			case ProgressUpToDate:
				result.UpToDate = append(result.UpToDate, res.repoName)
			case ProgressSkipped:
				result.Skipped = append(result.Skipped, res.repoName)
			case ProgressFailed:
				result.Failed[res.repoName] = res.err
			}
		}
	}

	// Detect archived repos (exist locally but not on remote)
	result.Archived = s.detectArchived(repos)

	return result, nil
}

func (s *Syncer) syncWorker(ctx context.Context, jobs <-chan *gh.Repository, results chan<- syncResult) {
	for repo := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
		}

		repoName := repo.GetFullName()

		// Check include/exclude filters
		if !s.shouldSync(repo.GetName()) {
			if s.opts.Verbose {
				fmt.Printf("Skipping %s (filtered)\n", repoName)
			}
			s.reportProgress(repoName, ProgressSkipped, "filtered")
			results <- syncResult{repoName: repoName, status: ProgressSkipped}
			continue
		}

		// Determine local path
		localPath := filepath.Join(s.opts.Target, repo.GetOwner().GetLogin(), repo.GetName())

		if s.opts.Verbose {
			fmt.Printf("Processing %s -> %s\n", repoName, localPath)
		}

		if s.opts.DryRun {
			if s.git.IsGitRepo(localPath) {
				fmt.Printf("[DRY RUN] Would update: %s\n", repoName)
				s.reportProgress(repoName, ProgressUpdated, "dry-run")
				results <- syncResult{repoName: repoName, status: ProgressUpdated}
			} else {
				fmt.Printf("[DRY RUN] Would clone: %s\n", repoName)
				s.reportProgress(repoName, ProgressCloned, "dry-run")
				results <- syncResult{repoName: repoName, status: ProgressCloned}
			}
			continue
		}

		// Report progress: starting
		s.reportProgress(repoName, ProgressInProgress, "")

		// Sync the repository
		if s.git.IsGitRepo(localPath) {
			// Pull existing repo
			status, err := s.pullRepo(ctx, repo, localPath)
			if err != nil {
				s.reportProgress(repoName, ProgressFailed, err.Error())
				if s.opts.Verbose {
					fmt.Printf("Failed to update %s: %v\n", repoName, err)
				}
				results <- syncResult{repoName: repoName, status: ProgressFailed, err: err}
			} else {
				s.reportProgress(repoName, status, "")
				if s.opts.Verbose {
					if status == ProgressUpToDate {
						fmt.Printf("Up-to-date: %s\n", repoName)
					} else {
						fmt.Printf("Updated: %s\n", repoName)
					}
				}
				results <- syncResult{repoName: repoName, status: status}
			}
		} else {
			// Clone new repo
			if err := s.cloneRepo(ctx, repo, localPath); err != nil {
				s.reportProgress(repoName, ProgressFailed, err.Error())
				if s.opts.Verbose {
					fmt.Printf("Failed to clone %s: %v\n", repoName, err)
				}
				results <- syncResult{repoName: repoName, status: ProgressFailed, err: err}
			} else {
				s.reportProgress(repoName, ProgressCloned, "")
				if s.opts.Verbose {
					fmt.Printf("Cloned: %s\n", repoName)
				}
				results <- syncResult{repoName: repoName, status: ProgressCloned}
			}
		}
	}
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
			fmt.Printf("[DRY RUN] Would update: %s\n", repoName)
			s.reportProgress(repoName, ProgressUpdated, "dry-run")
			result.Updated = append(result.Updated, repoName)
		} else {
			fmt.Printf("[DRY RUN] Would clone: %s\n", repoName)
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
			fmt.Printf("Failed to update %s: %v\n", repoName, err)
		} else {
			if status == ProgressUpToDate {
				result.UpToDate = append(result.UpToDate, repoName)
			} else {
				result.Updated = append(result.Updated, repoName)
			}
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
			fmt.Printf("Failed to clone %s: %v\n", repoName, err)
		} else {
			result.Cloned = append(result.Cloned, repoName)
			s.reportProgress(repoName, ProgressCloned, "")
			if s.opts.Verbose {
				fmt.Printf("Cloned: %s\n", repoName)
			}
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
		remoteSet[path] = true
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
			if !remoteSet[normalizedPath] {
				archived = append(archived, normalizedPath)
			}

			return fs.SkipDir // Don't recurse into .git
		}

		return nil
	})

	return archived
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

	if err := s.git.Clone(ctx, cloneURL, localPath); err != nil {
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
			// Add a small buffer (1 second) to handle timing edge cases
			if repo.PushedAt.Time.Before(lastFetch.Add(-time.Second)) || repo.PushedAt.Time.Equal(lastFetch) {
				// No pushes since our last fetch, we're up-to-date
				return ProgressUpToDate, nil
			}
		}
		// On any error, fall through to normal fetch
	}

	// Fetch all branches from all remotes (for complete backup of all branches)
	// Using fetch instead of pull so we update all remote-tracking branches
	// without modifying the working directory
	if err := s.git.FetchAll(ctx, localPath); err != nil {
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

	// Handle contains (*pattern*)
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		middle := strings.Trim(pattern, "*")
		return strings.Contains(name, middle)
	}

	// Exact match
	return pattern == name
}
