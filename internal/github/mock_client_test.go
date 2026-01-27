package github

import (
	"context"
	"errors"
	"testing"
	"time"

	gh "github.com/google/go-github/v68/github"
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
