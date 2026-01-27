package github

import (
	"context"

	gh "github.com/google/go-github/v68/github"
)

// MockClient is a mock implementation of the Client interface for testing
type MockClient struct {
	// GetReleasesFunc can be set to mock GetReleases behavior
	GetReleasesFunc func(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error)

	// RemoveReleaseFunc can be set to mock RemoveRelease behavior
	RemoveReleaseFunc func(ctx context.Context, owner, repo string, release *gh.RepositoryRelease) error

	// ListUserReposFunc can be set to mock ListUserRepos behavior
	ListUserReposFunc func(ctx context.Context, username string, opts *ListOptions) ([]*gh.Repository, error)

	// ListOrgReposFunc can be set to mock ListOrgRepos behavior
	ListOrgReposFunc func(ctx context.Context, org string, opts *ListOptions) ([]*gh.Repository, error)

	// GetRepositoryFunc can be set to mock GetRepository behavior
	GetRepositoryFunc func(ctx context.Context, owner, repo string) (*gh.Repository, error)

	// GetRateLimitFunc can be set to mock GetRateLimit behavior
	GetRateLimitFunc func(ctx context.Context) (*gh.RateLimits, error)

	// Call tracking
	Calls []MockCall
}

// MockCall records a method call for verification
type MockCall struct {
	Method string
	Args   []interface{}
}

// NewMockClient creates a new mock client
func NewMockClient() *MockClient {
	return &MockClient{
		Calls: make([]MockCall, 0),
	}
}

// GetReleases implements Client.GetReleases
func (m *MockClient) GetReleases(ctx context.Context, owner, repo string) ([]*gh.RepositoryRelease, error) {
	m.Calls = append(m.Calls, MockCall{Method: "GetReleases", Args: []interface{}{owner, repo}})
	if m.GetReleasesFunc != nil {
		return m.GetReleasesFunc(ctx, owner, repo)
	}
	return nil, nil
}

// RemoveRelease implements Client.RemoveRelease
func (m *MockClient) RemoveRelease(ctx context.Context, owner, repo string, release *gh.RepositoryRelease) error {
	m.Calls = append(m.Calls, MockCall{Method: "RemoveRelease", Args: []interface{}{owner, repo, release}})
	if m.RemoveReleaseFunc != nil {
		return m.RemoveReleaseFunc(ctx, owner, repo, release)
	}
	return nil
}

// ListUserRepos implements Client.ListUserRepos
func (m *MockClient) ListUserRepos(ctx context.Context, username string, opts *ListOptions) ([]*gh.Repository, error) {
	m.Calls = append(m.Calls, MockCall{Method: "ListUserRepos", Args: []interface{}{username, opts}})
	if m.ListUserReposFunc != nil {
		return m.ListUserReposFunc(ctx, username, opts)
	}
	return nil, nil
}

// ListOrgRepos implements Client.ListOrgRepos
func (m *MockClient) ListOrgRepos(ctx context.Context, org string, opts *ListOptions) ([]*gh.Repository, error) {
	m.Calls = append(m.Calls, MockCall{Method: "ListOrgRepos", Args: []interface{}{org, opts}})
	if m.ListOrgReposFunc != nil {
		return m.ListOrgReposFunc(ctx, org, opts)
	}
	return nil, nil
}

// GetRepository implements Client.GetRepository
func (m *MockClient) GetRepository(ctx context.Context, owner, repo string) (*gh.Repository, error) {
	m.Calls = append(m.Calls, MockCall{Method: "GetRepository", Args: []interface{}{owner, repo}})
	if m.GetRepositoryFunc != nil {
		return m.GetRepositoryFunc(ctx, owner, repo)
	}
	return nil, nil
}

// GetRateLimit implements Client.GetRateLimit
func (m *MockClient) GetRateLimit(ctx context.Context) (*gh.RateLimits, error) {
	m.Calls = append(m.Calls, MockCall{Method: "GetRateLimit", Args: []interface{}{}})
	if m.GetRateLimitFunc != nil {
		return m.GetRateLimitFunc(ctx)
	}
	return nil, nil
}

// Reset clears all recorded calls
func (m *MockClient) Reset() {
	m.Calls = make([]MockCall, 0)
}

// CallCount returns the number of times a method was called
func (m *MockClient) CallCount(method string) int {
	count := 0
	for _, call := range m.Calls {
		if call.Method == method {
			count++
		}
	}
	return count
}
