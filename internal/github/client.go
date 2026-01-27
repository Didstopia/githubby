package github

import (
	"context"
	"fmt"
	"net/url"

	gh "github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"

	gherrors "github.com/Didstopia/githubby/internal/errors"
)

// client implements the Client interface
type client struct {
	ghClient *gh.Client
}

// NewClient creates a new GitHub client with the provided token
func NewClient(token string) Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	return &client{
		ghClient: gh.NewClient(tc),
	}
}

// GetReleases returns all releases for a repository using iterative pagination
func (c *client) GetReleases(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error) {
	var allReleases []*gh.RepositoryRelease

	opts := &gh.ListOptions{
		Page:    1,
		PerPage: 100,
	}

	for {
		releases, resp, err := c.ghClient.Repositories.ListReleases(ctx, owner, repo, opts)
		if err != nil {
			return nil, wrapAPIError(resp, err)
		}

		allReleases = append(allReleases, releases...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allReleases, nil
}

// RemoveRelease deletes a release and its associated tag
func (c *client) RemoveRelease(ctx context.Context, owner, repo string, release *gh.RepositoryRelease) error {
	// Delete the release first
	resp, err := c.ghClient.Repositories.DeleteRelease(ctx, owner, repo, release.GetID())
	if err != nil {
		return wrapAPIError(resp, err)
	}

	// Delete the associated tag
	if release.TagName != nil && *release.TagName != "" {
		tagRef := fmt.Sprintf("refs/tags/%s", url.PathEscape(*release.TagName))
		resp, err = c.ghClient.Git.DeleteRef(ctx, owner, repo, tagRef)
		if err != nil {
			// Tag deletion failure is not critical, log but don't fail
			return wrapAPIError(resp, err)
		}
	}

	return nil
}

// ListUserRepos returns all repositories for a user
func (c *client) ListUserRepos(ctx context.Context, username string, opts *ListOptions) ([]*gh.Repository, error) {
	if opts == nil {
		opts = DefaultListOptions()
	}

	var allRepos []*gh.Repository

	ghOpts := &gh.RepositoryListByUserOptions{
		Type: opts.Type,
		ListOptions: gh.ListOptions{
			Page:    1,
			PerPage: opts.PerPage,
		},
	}

	for {
		repos, resp, err := c.ghClient.Repositories.ListByUser(ctx, username, ghOpts)
		if err != nil {
			return nil, wrapAPIError(resp, err)
		}

		for _, repo := range repos {
			if !opts.IncludePrivate && repo.GetPrivate() {
				continue
			}
			allRepos = append(allRepos, repo)
		}

		if resp.NextPage == 0 {
			break
		}
		ghOpts.Page = resp.NextPage
	}

	return allRepos, nil
}

// ListOrgRepos returns all repositories for an organization
func (c *client) ListOrgRepos(ctx context.Context, org string, opts *ListOptions) ([]*gh.Repository, error) {
	if opts == nil {
		opts = DefaultListOptions()
	}

	var allRepos []*gh.Repository

	ghOpts := &gh.RepositoryListByOrgOptions{
		Type: opts.Type,
		ListOptions: gh.ListOptions{
			Page:    1,
			PerPage: opts.PerPage,
		},
	}

	for {
		repos, resp, err := c.ghClient.Repositories.ListByOrg(ctx, org, ghOpts)
		if err != nil {
			return nil, wrapAPIError(resp, err)
		}

		for _, repo := range repos {
			if !opts.IncludePrivate && repo.GetPrivate() {
				continue
			}
			allRepos = append(allRepos, repo)
		}

		if resp.NextPage == 0 {
			break
		}
		ghOpts.Page = resp.NextPage
	}

	return allRepos, nil
}

// GetRepository returns information about a single repository
func (c *client) GetRepository(ctx context.Context, owner, repo string) (*gh.Repository, error) {
	repository, resp, err := c.ghClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return nil, wrapAPIError(resp, err)
	}
	return repository, nil
}

// GetRateLimit returns the current rate limit status
func (c *client) GetRateLimit(ctx context.Context) (*gh.RateLimits, error) {
	rateLimits, resp, err := c.ghClient.RateLimit.Get(ctx)
	if err != nil {
		return nil, wrapAPIError(resp, err)
	}
	return rateLimits, nil
}

// wrapAPIError converts a GitHub API response error to our error type
func wrapAPIError(resp *gh.Response, err error) error {
	if err == nil {
		return nil
	}

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}

	switch statusCode {
	case 401:
		return gherrors.ErrUnauthorized
	case 403, 429:
		return gherrors.ErrRateLimited
	case 404:
		return gherrors.ErrNotFound
	default:
		return gherrors.NewAPIError(statusCode, "API request failed", err)
	}
}
