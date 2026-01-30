package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	gh "github.com/google/go-github/v68/github"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gherrors "github.com/Didstopia/githubby/internal/errors"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test-token")
	assert.NotNil(t, client, "client should not be nil")

	// Verify it implements the Client interface
	var _ Client = client //nolint:staticcheck // Interface compliance check
}

func TestDefaultListOptions(t *testing.T) {
	opts := DefaultListOptions()

	assert.Equal(t, "all", opts.Type)
	assert.False(t, opts.IncludePrivate)
	assert.Equal(t, 100, opts.PerPage)
}

func TestWrapAPIError(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		err            error
		expectedError  error
		checkAPIError  bool
		expectedStatus int
		nilResponse    bool
	}{
		{
			name:          "nil error returns nil",
			statusCode:    200,
			err:           nil,
			expectedError: nil,
		},
		{
			name:          "401 returns ErrUnauthorized",
			statusCode:    401,
			err:           errors.New("test error"),
			expectedError: gherrors.ErrUnauthorized,
		},
		{
			name:          "403 returns ErrForbidden (not rate limited)",
			statusCode:    403,
			err:           errors.New("test error"),
			expectedError: gherrors.ErrForbidden,
		},
		{
			name:          "429 returns ErrRateLimited",
			statusCode:    429,
			err:           errors.New("test error"),
			expectedError: gherrors.ErrRateLimited,
		},
		{
			name:          "404 returns ErrNotFound",
			statusCode:    404,
			err:           errors.New("test error"),
			expectedError: gherrors.ErrNotFound,
		},
		{
			name:           "500 returns APIError",
			statusCode:     500,
			err:            errors.New("test error"),
			checkAPIError:  true,
			expectedStatus: 500,
		},
		{
			name:           "nil response returns APIError with 0 status",
			err:            errors.New("test error"),
			checkAPIError:  true,
			expectedStatus: 0,
			nilResponse:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp *gh.Response
			if !tt.nilResponse && tt.statusCode > 0 {
				resp = &gh.Response{
					Response: &http.Response{
						StatusCode: tt.statusCode,
					},
				}
			}

			result := wrapAPIError(resp, tt.err)

			if tt.expectedError != nil {
				assert.ErrorIs(t, result, tt.expectedError)
			} else if tt.checkAPIError {
				var apiErr *gherrors.APIError
				require.ErrorAs(t, result, &apiErr)
				assert.Equal(t, tt.expectedStatus, apiErr.StatusCode)
			} else {
				assert.NoError(t, result)
			}
		})
	}
}

func TestGetReleases(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	t.Run("single page of releases", func(t *testing.T) {
		httpmock.Reset()

		releases := []map[string]interface{}{
			{"id": 1, "tag_name": "v1.0.0"},
			{"id": 2, "tag_name": "v1.1.0"},
		}
		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo/releases",
			func(req *http.Request) (*http.Response, error) {
				resp, _ := httpmock.NewJsonResponse(200, releases)
				return resp, nil
			})

		client := NewClient("test-token")
		result, err := client.GetReleases(context.Background(), "owner", "repo")

		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("empty releases", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo/releases",
			httpmock.NewJsonResponderOrPanic(200, []interface{}{}))

		client := NewClient("test-token")
		result, err := client.GetReleases(context.Background(), "owner", "repo")

		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("multi-page pagination", func(t *testing.T) {
		httpmock.Reset()

		page1 := []map[string]interface{}{{"id": 1, "tag_name": "v1.0.0"}}
		page2 := []map[string]interface{}{{"id": 2, "tag_name": "v2.0.0"}}

		callCount := 0
		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo/releases",
			func(req *http.Request) (*http.Response, error) {
				callCount++
				var data []map[string]interface{}
				resp := &http.Response{
					StatusCode: 200,
					Header:     make(http.Header),
				}
				if callCount == 1 {
					data = page1
					resp.Header.Set("Link", `<https://api.github.com/repos/owner/repo/releases?page=2>; rel="next"`)
				} else {
					data = page2
				}
				body, _ := json.Marshal(data)
				resp.Body = httpmock.NewRespBodyFromString(string(body))
				resp.Header.Set("Content-Type", "application/json")
				return resp, nil
			})

		client := NewClient("test-token")
		result, err := client.GetReleases(context.Background(), "owner", "repo")

		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, 2, callCount)
	})

	t.Run("unauthorized error", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo/releases",
			httpmock.NewJsonResponderOrPanic(401, map[string]string{"message": "Bad credentials"}))

		client := NewClient("test-token")
		_, err := client.GetReleases(context.Background(), "owner", "repo")

		assert.ErrorIs(t, err, gherrors.ErrUnauthorized)
	})

	t.Run("not found error", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo/releases",
			httpmock.NewJsonResponderOrPanic(404, map[string]string{"message": "Not Found"}))

		client := NewClient("test-token")
		_, err := client.GetReleases(context.Background(), "owner", "repo")

		assert.ErrorIs(t, err, gherrors.ErrNotFound)
	})

	t.Run("forbidden error (403 without rate limit headers)", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo/releases",
			httpmock.NewJsonResponderOrPanic(403, map[string]string{"message": "Resource not accessible by integration"}))

		client := NewClient("test-token")
		_, err := client.GetReleases(context.Background(), "owner", "repo")

		assert.ErrorIs(t, err, gherrors.ErrForbidden)
		// Verify the API message is preserved
		assert.Contains(t, err.Error(), "Resource not accessible by integration")
	})

	t.Run("rate limited error (429)", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo/releases",
			httpmock.NewJsonResponderOrPanic(429, map[string]string{"message": "Too many requests"}))

		client := NewClient("test-token")
		_, err := client.GetReleases(context.Background(), "owner", "repo")

		assert.ErrorIs(t, err, gherrors.ErrRateLimited)
	})

	t.Run("unauthorized error preserves API message", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo/releases",
			httpmock.NewJsonResponderOrPanic(401, map[string]string{"message": "Bad credentials"}))

		client := NewClient("test-token")
		_, err := client.GetReleases(context.Background(), "owner", "repo")

		assert.ErrorIs(t, err, gherrors.ErrUnauthorized)
		assert.Contains(t, err.Error(), "Bad credentials")
	})
}

func TestRemoveRelease(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	t.Run("successful deletion", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("DELETE", "https://api.github.com/repos/owner/repo/releases/123",
			httpmock.NewStringResponder(204, ""))

		httpmock.RegisterResponder("DELETE", "https://api.github.com/repos/owner/repo/git/refs/tags/v1.0.0",
			httpmock.NewStringResponder(204, ""))

		client := NewClient("test-token")
		id := int64(123)
		tagName := "v1.0.0"
		release := &gh.RepositoryRelease{ID: &id, TagName: &tagName}

		err := client.RemoveRelease(context.Background(), "owner", "repo", release)
		require.NoError(t, err)
	})

	t.Run("release deletion error", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("DELETE", "https://api.github.com/repos/owner/repo/releases/123",
			httpmock.NewJsonResponderOrPanic(404, map[string]string{"message": "Not Found"}))

		client := NewClient("test-token")
		id := int64(123)
		tagName := "v1.0.0"
		release := &gh.RepositoryRelease{ID: &id, TagName: &tagName}

		err := client.RemoveRelease(context.Background(), "owner", "repo", release)
		assert.ErrorIs(t, err, gherrors.ErrNotFound)
	})

	t.Run("tag deletion error", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("DELETE", "https://api.github.com/repos/owner/repo/releases/123",
			httpmock.NewStringResponder(204, ""))

		httpmock.RegisterResponder("DELETE", "https://api.github.com/repos/owner/repo/git/refs/tags/v1.0.0",
			httpmock.NewJsonResponderOrPanic(404, map[string]string{"message": "Not Found"}))

		client := NewClient("test-token")
		id := int64(123)
		tagName := "v1.0.0"
		release := &gh.RepositoryRelease{ID: &id, TagName: &tagName}

		err := client.RemoveRelease(context.Background(), "owner", "repo", release)
		// Tag deletion failure should return error
		assert.ErrorIs(t, err, gherrors.ErrNotFound)
	})

	t.Run("no tag to delete", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("DELETE", "https://api.github.com/repos/owner/repo/releases/123",
			httpmock.NewStringResponder(204, ""))

		client := NewClient("test-token")
		id := int64(123)
		emptyTag := ""
		release := &gh.RepositoryRelease{ID: &id, TagName: &emptyTag}

		err := client.RemoveRelease(context.Background(), "owner", "repo", release)
		require.NoError(t, err)
	})
}

func TestListUserRepos(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	t.Run("list public repos", func(t *testing.T) {
		httpmock.Reset()

		repos := []map[string]interface{}{
			{"id": 1, "name": "repo1", "full_name": "user/repo1", "private": false},
			{"id": 2, "name": "repo2", "full_name": "user/repo2", "private": false},
		}
		httpmock.RegisterResponder("GET", "https://api.github.com/users/testuser/repos",
			httpmock.NewJsonResponderOrPanic(200, repos))

		client := NewClient("test-token")
		result, err := client.ListUserRepos(context.Background(), "testuser", nil)

		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("filters private repos when IncludePrivate is false", func(t *testing.T) {
		httpmock.Reset()

		repos := []map[string]interface{}{
			{"id": 1, "name": "repo1", "full_name": "user/repo1", "private": false},
			{"id": 2, "name": "repo2", "full_name": "user/repo2", "private": true},
		}
		httpmock.RegisterResponder("GET", "https://api.github.com/users/testuser/repos",
			httpmock.NewJsonResponderOrPanic(200, repos))

		client := NewClient("test-token")
		opts := &ListOptions{IncludePrivate: false}
		result, err := client.ListUserRepos(context.Background(), "testuser", opts)

		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "repo1", result[0].GetName())
	})

	t.Run("includes private repos when IncludePrivate is true", func(t *testing.T) {
		httpmock.Reset()

		repos := []map[string]interface{}{
			{"id": 1, "name": "repo1", "full_name": "user/repo1", "private": false},
			{"id": 2, "name": "repo2", "full_name": "user/repo2", "private": true},
		}
		httpmock.RegisterResponder("GET", "https://api.github.com/users/testuser/repos",
			httpmock.NewJsonResponderOrPanic(200, repos))

		client := NewClient("test-token")
		opts := &ListOptions{IncludePrivate: true}
		result, err := client.ListUserRepos(context.Background(), "testuser", opts)

		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("not found error", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/users/nonexistent/repos",
			httpmock.NewJsonResponderOrPanic(404, map[string]string{"message": "Not Found"}))

		client := NewClient("test-token")
		_, err := client.ListUserRepos(context.Background(), "nonexistent", nil)

		assert.ErrorIs(t, err, gherrors.ErrNotFound)
	})
}

func TestListOrgRepos(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	t.Run("list org repos", func(t *testing.T) {
		httpmock.Reset()

		repos := []map[string]interface{}{
			{"id": 1, "name": "repo1", "full_name": "org/repo1", "private": false},
			{"id": 2, "name": "repo2", "full_name": "org/repo2", "private": false},
		}
		httpmock.RegisterResponder("GET", "https://api.github.com/orgs/testorg/repos",
			httpmock.NewJsonResponderOrPanic(200, repos))

		client := NewClient("test-token")
		result, err := client.ListOrgRepos(context.Background(), "testorg", nil)

		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("filters private repos", func(t *testing.T) {
		httpmock.Reset()

		repos := []map[string]interface{}{
			{"id": 1, "name": "repo1", "full_name": "org/repo1", "private": false},
			{"id": 2, "name": "repo2", "full_name": "org/repo2", "private": true},
		}
		httpmock.RegisterResponder("GET", "https://api.github.com/orgs/testorg/repos",
			httpmock.NewJsonResponderOrPanic(200, repos))

		client := NewClient("test-token")
		opts := &ListOptions{IncludePrivate: false}
		result, err := client.ListOrgRepos(context.Background(), "testorg", opts)

		require.NoError(t, err)
		assert.Len(t, result, 1)
	})

	t.Run("not found error", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/orgs/nonexistent/repos",
			httpmock.NewJsonResponderOrPanic(404, map[string]string{"message": "Not Found"}))

		client := NewClient("test-token")
		_, err := client.ListOrgRepos(context.Background(), "nonexistent", nil)

		assert.ErrorIs(t, err, gherrors.ErrNotFound)
	})
}

func TestGetRepository(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	t.Run("successful get", func(t *testing.T) {
		httpmock.Reset()

		repo := map[string]interface{}{
			"id":        1,
			"name":      "repo1",
			"full_name": "owner/repo1",
			"private":   false,
		}
		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/repo1",
			httpmock.NewJsonResponderOrPanic(200, repo))

		client := NewClient("test-token")
		result, err := client.GetRepository(context.Background(), "owner", "repo1")

		require.NoError(t, err)
		assert.Equal(t, "repo1", result.GetName())
	})

	t.Run("not found", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/repos/owner/nonexistent",
			httpmock.NewJsonResponderOrPanic(404, map[string]string{"message": "Not Found"}))

		client := NewClient("test-token")
		_, err := client.GetRepository(context.Background(), "owner", "nonexistent")

		assert.ErrorIs(t, err, gherrors.ErrNotFound)
	})
}

func TestGetRateLimit(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	t.Run("successful get", func(t *testing.T) {
		httpmock.Reset()

		rateLimit := map[string]interface{}{
			"resources": map[string]interface{}{
				"core": map[string]interface{}{
					"limit":     5000,
					"remaining": 4999,
					"reset":     1234567890,
				},
			},
		}
		httpmock.RegisterResponder("GET", "https://api.github.com/rate_limit",
			httpmock.NewJsonResponderOrPanic(200, rateLimit))

		client := NewClient("test-token")
		result, err := client.GetRateLimit(context.Background())

		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("unauthorized", func(t *testing.T) {
		httpmock.Reset()

		httpmock.RegisterResponder("GET", "https://api.github.com/rate_limit",
			httpmock.NewJsonResponderOrPanic(401, map[string]string{"message": "Bad credentials"}))

		client := NewClient("test-token")
		_, err := client.GetRateLimit(context.Background())

		assert.ErrorIs(t, err, gherrors.ErrUnauthorized)
	})
}
