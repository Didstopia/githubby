// Package github provides interfaces and implementation for GitHub API operations
package github

import (
	"context"

	gh "github.com/google/go-github/v68/github"
)

// Client defines the interface for GitHub API operations
type Client interface {
	// GetReleases returns all releases for a repository
	GetReleases(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error)

	// RemoveRelease deletes a release and its associated tag
	RemoveRelease(ctx context.Context, owner, repo string, release *gh.RepositoryRelease) error

	// ListUserRepos returns all repositories for a user
	ListUserRepos(ctx context.Context, username string, opts *ListOptions) ([]*gh.Repository, error)

	// ListOrgRepos returns all repositories for an organization
	ListOrgRepos(ctx context.Context, org string, opts *ListOptions) ([]*gh.Repository, error)

	// GetRepository returns information about a single repository
	GetRepository(ctx context.Context, owner, repo string) (*gh.Repository, error)

	// GetRateLimit returns the current rate limit status
	GetRateLimit(ctx context.Context) (*gh.RateLimits, error)
}

// ListOptions specifies optional parameters for list operations
type ListOptions struct {
	// Type of repositories to list: all, owner, member (default: all)
	Type string

	// Include private repositories
	IncludePrivate bool

	// PerPage specifies the number of results per page (max 100)
	PerPage int
}

// DefaultListOptions returns default list options
func DefaultListOptions() *ListOptions {
	return &ListOptions{
		Type:           "all",
		IncludePrivate: false,
		PerPage:        100,
	}
}
