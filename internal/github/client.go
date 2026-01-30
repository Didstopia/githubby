package github

import (
	"context"
	"errors"
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

// ListUserOrgs returns all organizations the authenticated user belongs to
func (c *client) ListUserOrgs(ctx context.Context) ([]*gh.Organization, error) {
	var allOrgs []*gh.Organization

	opts := &gh.ListOptions{
		Page:    1,
		PerPage: 100,
	}

	for {
		orgs, resp, err := c.ghClient.Organizations.List(ctx, "", opts)
		if err != nil {
			return nil, wrapAPIError(resp, err)
		}

		allOrgs = append(allOrgs, orgs...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allOrgs, nil
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

// GetBranchRef returns the SHA of a branch (used for fast sync check)
func (c *client) GetBranchRef(ctx context.Context, owner, repo, branch string) (string, error) {
	ref, resp, err := c.ghClient.Git.GetRef(ctx, owner, repo, "refs/heads/"+branch)
	if err != nil {
		return "", wrapAPIError(resp, err)
	}
	return ref.GetObject().GetSHA(), nil
}

// wrapAPIError converts a GitHub API response error to our error type.
// It checks go-github typed errors first for accurate rate-limit detection,
// then falls back to status code mapping. GitHub API error messages are
// preserved in the returned error for better diagnostics.
func wrapAPIError(resp *gh.Response, err error) error {
	if err == nil {
		return nil
	}

	// Check go-github typed errors first (most reliable for rate limiting)
	var rateLimitErr *gh.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return fmt.Errorf("%w: %s", gherrors.ErrRateLimited, rateLimitErr.Message)
	}

	var abuseErr *gh.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		return fmt.Errorf("%w: %s", gherrors.ErrRateLimited, abuseErr.Message)
	}

	// Extract message from GitHub ErrorResponse if available
	apiMessage := ""
	var ghErr *gh.ErrorResponse
	if errors.As(err, &ghErr) {
		apiMessage = ghErr.Message
	}

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}

	switch statusCode {
	case 401:
		if apiMessage != "" {
			return fmt.Errorf("%w: %s", gherrors.ErrUnauthorized, apiMessage)
		}
		return gherrors.ErrUnauthorized
	case 403:
		// 403 without a typed rate-limit error is a permission denial
		if apiMessage != "" {
			return fmt.Errorf("%w: %s", gherrors.ErrForbidden, apiMessage)
		}
		return gherrors.ErrForbidden
	case 429:
		if apiMessage != "" {
			return fmt.Errorf("%w: %s", gherrors.ErrRateLimited, apiMessage)
		}
		return gherrors.ErrRateLimited
	case 404:
		if apiMessage != "" {
			return fmt.Errorf("%w: %s", gherrors.ErrNotFound, apiMessage)
		}
		return gherrors.ErrNotFound
	default:
		msg := "API request failed"
		if apiMessage != "" {
			msg = apiMessage
		}
		return gherrors.NewAPIError(statusCode, msg, err)
	}
}
