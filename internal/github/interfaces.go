// Package github provides interfaces and implementation for GitHub API operations
package github

import (
	"context"

	gh "github.com/google/go-github/v86/github"
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

	// ListUserOrgs returns all organizations the authenticated user belongs to
	ListUserOrgs(ctx context.Context) ([]*gh.Organization, error)

	// GetRepository returns information about a single repository
	GetRepository(ctx context.Context, owner, repo string) (*gh.Repository, error)

	// GetRateLimit returns the current rate limit status
	GetRateLimit(ctx context.Context) (*gh.RateLimits, error)

	// GetBranchRef returns the SHA of a branch (used for fast sync check)
	GetBranchRef(ctx context.Context, owner, repo, branch string) (string, error)

	// GetAuthenticatedUser returns the account the token belongs to
	GetAuthenticatedUser(ctx context.Context) (*gh.User, error)

	// ListGists returns all gists of the authenticated user, secret ones included
	ListGists(ctx context.Context) ([]*gh.Gist, error)

	// GetIssuePRCounts returns how many issues and pull requests (all states) a repo has
	GetIssuePRCounts(ctx context.Context, owner, repo string) (issues, pulls int, err error)
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
