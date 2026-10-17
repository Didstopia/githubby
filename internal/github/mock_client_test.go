package github

import (
	"context"
	"errors"
	"testing"
	"time"

	gh "github.com/google/go-github/v86/github"
)

func TestMockClient_GetReleases(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	// Test with nil func (should return nil)
	releases, err := mock.GetReleases(ctx, "owner", "repo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if releases != nil {
		t.Error("expected nil releases")
	}

	// Test call tracking
	if mock.CallCount("GetReleases") != 1 {
		t.Errorf("expected 1 call, got %d", mock.CallCount("GetReleases"))
	}

	// Test with custom func
	expectedReleases := []*gh.RepositoryRelease{
		{TagName: gh.Ptr("v1.0.0")},
		{TagName: gh.Ptr("v1.1.0")},
	}

	mock.GetReleasesFunc = func(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error) {
		if owner != "test-owner" || repo != "test-repo" {
			return nil, errors.New("unexpected args")
		}
		return expectedReleases, nil
	}

	releases, err = mock.GetReleases(ctx, "test-owner", "test-repo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(releases) != 2 {
		t.Errorf("expected 2 releases, got %d", len(releases))
	}
}

func TestMockClient_RemoveRelease(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	release := &gh.RepositoryRelease{
		TagName: gh.Ptr("v1.0.0"),
	}

	// Test with nil func (should return nil)
	err := mock.RemoveRelease(ctx, "owner", "repo", release)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Test call tracking
	if mock.CallCount("RemoveRelease") != 1 {
		t.Errorf("expected 1 call, got %d", mock.CallCount("RemoveRelease"))
	}

	// Test with error returning func
	expectedErr := errors.New("delete failed")
	mock.RemoveReleaseFunc = func(ctx context.Context, owner, repo string, release *gh.RepositoryRelease) error {
		return expectedErr
	}

	err = mock.RemoveRelease(ctx, "owner", "repo", release)
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestMockClient_ListUserRepos(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	// Test with custom func
	expectedRepos := []*gh.Repository{
		{Name: gh.Ptr("repo1")},
		{Name: gh.Ptr("repo2")},
	}

	mock.ListUserReposFunc = func(ctx context.Context, username string, opts *ListOptions) ([]*gh.Repository, error) {
		return expectedRepos, nil
	}

	repos, err := mock.ListUserRepos(ctx, "testuser", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(repos))
	}

	// Verify call tracking
	if mock.CallCount("ListUserRepos") != 1 {
		t.Errorf("expected 1 call, got %d", mock.CallCount("ListUserRepos"))
	}
}

func TestMockClient_ListOrgRepos(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	// Test with custom func
	mock.ListOrgReposFunc = func(ctx context.Context, org string, opts *ListOptions) ([]*gh.Repository, error) {
		return []*gh.Repository{
			{Name: gh.Ptr("org-repo")},
		}, nil
	}

	repos, err := mock.ListOrgRepos(ctx, "testorg", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("expected 1 repo, got %d", len(repos))
	}
}

func TestMockClient_GetRepository(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	expectedRepo := &gh.Repository{
		Name:     gh.Ptr("test-repo"),
		FullName: gh.Ptr("owner/test-repo"),
	}

	mock.GetRepositoryFunc = func(ctx context.Context, owner, repo string) (*gh.Repository, error) {
		return expectedRepo, nil
	}

	repo, err := mock.GetRepository(ctx, "owner", "test-repo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if repo.GetName() != "test-repo" {
		t.Errorf("expected repo name 'test-repo', got %s", repo.GetName())
	}
}

func TestMockClient_GetRateLimit(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	expectedLimits := &gh.RateLimits{
		Core: &gh.Rate{
			Limit:     5000,
			Remaining: 4999,
			Reset:     gh.Timestamp{Time: time.Now().Add(time.Hour)},
		},
	}

	mock.GetRateLimitFunc = func(ctx context.Context) (*gh.RateLimits, error) {
		return expectedLimits, nil
	}

	limits, err := mock.GetRateLimit(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if limits.Core.Remaining != 4999 {
		t.Errorf("expected 4999 remaining, got %d", limits.Core.Remaining)
	}
}

func TestMockClient_Reset(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	// Make some calls
	mock.GetReleases(ctx, "owner", "repo")
	mock.GetRepository(ctx, "owner", "repo")

	if len(mock.Calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(mock.Calls))
	}

	// Reset
	mock.Reset()

	if len(mock.Calls) != 0 {
		t.Errorf("expected 0 calls after reset, got %d", len(mock.Calls))
	}
}

func TestMockClient_CallCount(t *testing.T) {
	ctx := context.Background()
	mock := NewMockClient()

	// Make calls
	mock.GetReleases(ctx, "owner", "repo")
	mock.GetReleases(ctx, "owner", "repo2")
	mock.GetRepository(ctx, "owner", "repo")

	if mock.CallCount("GetReleases") != 2 {
		t.Errorf("expected 2 GetReleases calls, got %d", mock.CallCount("GetReleases"))
	}
	if mock.CallCount("GetRepository") != 1 {
		t.Errorf("expected 1 GetRepository call, got %d", mock.CallCount("GetRepository"))
	}
	if mock.CallCount("NonExistent") != 0 {
		t.Errorf("expected 0 NonExistent calls, got %d", mock.CallCount("NonExistent"))
	}
}

func TestMockClient_DefaultsAndOverrides(t *testing.T) {
	ctx := context.Background()
	m := NewMockClient()

	if repos, err := m.ListUserRepos(ctx, "u", nil); err != nil || repos != nil {
		t.Errorf("default ListUserRepos = %v, %v", repos, err)
	}
	if repos, err := m.ListOrgRepos(ctx, "o", nil); err != nil || repos != nil {
		t.Errorf("default ListOrgRepos = %v, %v", repos, err)
	}
	orgs, err := m.ListUserOrgs(ctx)
	if err != nil || orgs != nil {
		t.Errorf("default ListUserOrgs = %v, %v", orgs, err)
	}
	sha, err := m.GetBranchRef(ctx, "o", "r", "main")
	if err != nil || sha != "mock-sha-12345" {
		t.Errorf("default GetBranchRef = %q, %v", sha, err)
	}
	user, err := m.GetAuthenticatedUser(ctx)
	if err != nil || user.GetLogin() != "mock-user" {
		t.Errorf("default GetAuthenticatedUser = %v, %v", user, err)
	}
	gists, err := m.ListGists(ctx)
	if err != nil || gists != nil {
		t.Errorf("default ListGists = %v, %v", gists, err)
	}
	issues, pulls, err := m.GetIssuePRCounts(ctx, "o", "r")
	if err != nil || issues != 0 || pulls != 0 {
		t.Errorf("default GetIssuePRCounts = %d, %d, %v", issues, pulls, err)
	}
	if rl, err := m.GetRateLimit(ctx); err != nil || rl != nil {
		t.Errorf("default GetRateLimit = %v, %v", rl, err)
	}

	m.ListUserOrgsFunc = func(context.Context) ([]*gh.Organization, error) {
		return []*gh.Organization{{Login: gh.Ptr("o")}}, nil
	}
	m.GetBranchRefFunc = func(context.Context, string, string, string) (string, error) { return "", errors.New("x") }
	m.GetAuthenticatedUserFunc = func(context.Context) (*gh.User, error) { return nil, errors.New("x") }
	m.ListGistsFunc = func(context.Context) ([]*gh.Gist, error) { return []*gh.Gist{{}}, nil }
	m.GetIssuePRCountsFunc = func(context.Context, string, string) (int, int, error) { return 3, 4, nil }

	if orgs, _ := m.ListUserOrgs(ctx); len(orgs) != 1 {
		t.Error("ListUserOrgsFunc not used")
	}
	if _, err := m.GetBranchRef(ctx, "o", "r", "b"); err == nil {
		t.Error("GetBranchRefFunc not used")
	}
	if _, err := m.GetAuthenticatedUser(ctx); err == nil {
		t.Error("GetAuthenticatedUserFunc not used")
	}
	if gists, _ := m.ListGists(ctx); len(gists) != 1 {
		t.Error("ListGistsFunc not used")
	}
	if i, p, _ := m.GetIssuePRCounts(ctx, "o", "r"); i != 3 || p != 4 {
		t.Error("GetIssuePRCountsFunc not used")
	}
	if m.CallCount("ListUserOrgs") != 2 {
		t.Errorf("calls recorded = %d", m.CallCount("ListUserOrgs"))
	}
}
