// Package sync provides repository synchronization functionality
package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gh "github.com/google/go-github/v68/github"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

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
}

// Result represents the result of a sync operation
type Result struct {
	// Cloned repositories
	Cloned []string

	// Updated repositories (pulled)
	Updated []string

	// Skipped repositories (already up to date or filtered out)
	Skipped []string

	// Failed repositories with errors
	Failed map[string]error
}

// NewResult creates a new sync result
func NewResult() *Result {
	return &Result{
		Cloned:  make([]string, 0),
		Updated: make([]string, 0),
		Skipped: make([]string, 0),
		Failed:  make(map[string]error),
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

func (s *Syncer) syncRepos(ctx context.Context, repos []*gh.Repository) (*Result, error) {
	result := NewResult()

	// Ensure target directory exists
	if !s.opts.DryRun {
		if err := os.MkdirAll(s.opts.Target, 0755); err != nil {
			return nil, fmt.Errorf("failed to create target directory: %w", err)
		}
	}

	for _, repo := range repos {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		repoName := repo.GetFullName()

		// Check include/exclude filters
		if !s.shouldSync(repo.GetName()) {
			if s.opts.Verbose {
				fmt.Printf("Skipping %s (filtered)\n", repoName)
			}
			result.Skipped = append(result.Skipped, repoName)
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
				result.Updated = append(result.Updated, repoName)
			} else {
				fmt.Printf("[DRY RUN] Would clone: %s\n", repoName)
				result.Cloned = append(result.Cloned, repoName)
			}
			continue
		}

		// Sync the repository
		if s.git.IsGitRepo(localPath) {
			// Pull existing repo
			if err := s.pullRepo(ctx, localPath, repoName); err != nil {
				result.Failed[repoName] = err
				fmt.Printf("Failed to update %s: %v\n", repoName, err)
			} else {
				result.Updated = append(result.Updated, repoName)
				if s.opts.Verbose {
					fmt.Printf("Updated: %s\n", repoName)
				}
			}
		} else {
			// Clone new repo
			if err := s.cloneRepo(ctx, repo, localPath); err != nil {
				result.Failed[repoName] = err
				fmt.Printf("Failed to clone %s: %v\n", repoName, err)
			} else {
				result.Cloned = append(result.Cloned, repoName)
				if s.opts.Verbose {
					fmt.Printf("Cloned: %s\n", repoName)
				}
			}
		}
	}

	return result, nil
}

func (s *Syncer) cloneRepo(ctx context.Context, repo *gh.Repository, localPath string) error {
	// Create parent directory
	parentDir := filepath.Dir(localPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Clone the repository
	cloneURL := repo.GetCloneURL()
	if repo.GetPrivate() {
		// Use SSH for private repos
		cloneURL = repo.GetSSHURL()
	}

	if err := s.git.Clone(ctx, cloneURL, localPath); err != nil {
		return err
	}

	// Handle LFS if needed
	if s.lfs.RepoUsesLFS(localPath) {
		if s.opts.Verbose {
			fmt.Printf("Repository uses LFS, pulling LFS objects...\n")
		}
		if err := s.lfs.EnsureLFS(ctx); err != nil {
			return fmt.Errorf("failed to ensure LFS: %w", err)
		}
		if err := s.lfs.Pull(ctx, localPath); err != nil {
			return fmt.Errorf("failed to pull LFS objects: %w", err)
		}
	}

	return nil
}

func (s *Syncer) pullRepo(ctx context.Context, localPath, repoName string) error {
	// Fetch first
	if err := s.git.Fetch(ctx, localPath); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// Pull changes
	if err := s.git.Pull(ctx, localPath); err != nil {
		return fmt.Errorf("pull failed: %w", err)
	}

	// Handle LFS if needed
	if s.lfs.RepoUsesLFS(localPath) {
		if s.opts.Verbose {
			fmt.Printf("Repository uses LFS, pulling LFS objects...\n")
		}
		if err := s.lfs.EnsureLFS(ctx); err != nil {
			return fmt.Errorf("failed to ensure LFS: %w", err)
		}
		if err := s.lfs.Pull(ctx, localPath); err != nil {
			return fmt.Errorf("failed to pull LFS objects: %w", err)
		}
	}

	return nil
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
