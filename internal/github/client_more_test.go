package github

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gherrors "github.com/Didstopia/githubby/internal/errors"
)

func jsonErr(status int, msg string) httpmock.Responder {
	return httpmock.NewJsonResponderOrPanic(status, map[string]string{"message": msg})
}

// paged answers page 1 with the first list and a next link, page 2 with the second
func paged(path string, first, second any) httpmock.Responder {
	return func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("page") == "2" {
			return httpmock.NewJsonResponse(200, second)
		}
		resp, _ := httpmock.NewJsonResponse(200, first)
		resp.Header.Set("Link", `<https://api.github.com`+path+`?page=2>; rel="next"`)
		return resp, nil
	}
}

func TestListUserRepos_OwnAccountIncludesPrivateRepos(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	ctx := context.Background()

	httpmock.RegisterResponder("GET", "https://api.github.com/user", httpmock.NewJsonResponderOrPanic(200, map[string]any{"login": "Dids"}))
	httpmock.RegisterResponder("GET", "https://api.github.com/user/repos", func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "all", req.URL.Query().Get("visibility"))
		assert.Equal(t, "owner", req.URL.Query().Get("affiliation"))
		if req.URL.Query().Get("page") == "2" {
			return httpmock.NewJsonResponse(200, []map[string]any{{"name": "c", "private": true}})
		}
		resp, _ := httpmock.NewJsonResponse(200, []map[string]any{{"name": "a", "private": true}, {"name": "b"}})
		resp.Header.Set("Link", `<https://api.github.com/user/repos?page=2>; rel="next"`)
		return resp, nil
	})

	repos, err := NewClient("t").ListUserRepos(ctx, "dids", &ListOptions{IncludePrivate: true})
	require.NoError(t, err)
	require.Len(t, repos, 3, "private repos and both pages are kept")

	t.Run("error is typed", func(t *testing.T) {
		httpmock.RegisterResponder("GET", "https://api.github.com/user/repos", jsonErr(401, "Bad credentials"))
		_, err := NewClient("t").ListUserRepos(ctx, "dids", &ListOptions{IncludePrivate: true})
		assert.ErrorIs(t, err, gherrors.ErrUnauthorized)
	})

	t.Run("another user's repos come from the public listing", func(t *testing.T) {
		httpmock.RegisterResponder("GET", "https://api.github.com/users/other/repos",
			httpmock.NewJsonResponderOrPanic(200, []map[string]any{{"name": "x"}}))
		repos, err := NewClient("t").ListUserRepos(ctx, "other", &ListOptions{IncludePrivate: true})
		require.NoError(t, err)
		assert.Len(t, repos, 1)
	})

	t.Run("failed identity lookup falls back to the public listing", func(t *testing.T) {
		httpmock.RegisterResponder("GET", "https://api.github.com/user", jsonErr(500, "down"))
		httpmock.RegisterResponder("GET", "https://api.github.com/users/dids/repos",
			httpmock.NewJsonResponderOrPanic(200, []map[string]any{{"name": "pub"}}))
		repos, err := NewClient("t").ListUserRepos(ctx, "dids", &ListOptions{IncludePrivate: true})
		require.NoError(t, err)
		assert.Len(t, repos, 1)
	})
}

func TestListUserRepos_PagesAndErrors(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	ctx := context.Background()

	httpmock.RegisterResponder("GET", "https://api.github.com/users/u/repos",
		paged("/users/u/repos", []map[string]any{{"name": "a"}}, []map[string]any{{"name": "b"}}))
	repos, err := NewClient("t").ListUserRepos(ctx, "u", nil)
	require.NoError(t, err)
	assert.Len(t, repos, 2)

	httpmock.RegisterResponder("GET", "https://api.github.com/users/u/repos", jsonErr(404, "Not Found"))
	_, err = NewClient("t").ListUserRepos(ctx, "u", nil)
	assert.ErrorIs(t, err, gherrors.ErrNotFound)
}

func TestListOrgRepos_PagesFilterAndErrors(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	ctx := context.Background()

	httpmock.RegisterResponder("GET", "https://api.github.com/orgs/o/repos",
		paged("/orgs/o/repos", []map[string]any{{"name": "a"}, {"name": "p", "private": true}}, []map[string]any{{"name": "b"}}))

	repos, err := NewClient("t").ListOrgRepos(ctx, "o", nil)
	require.NoError(t, err)
	assert.Len(t, repos, 2, "private repo is left out by default")

	repos, err = NewClient("t").ListOrgRepos(ctx, "o", &ListOptions{IncludePrivate: true})
	require.NoError(t, err)
	assert.Len(t, repos, 3)

	httpmock.RegisterResponder("GET", "https://api.github.com/orgs/o/repos", jsonErr(403, "forbidden"))
	_, err = NewClient("t").ListOrgRepos(ctx, "o", nil)
	assert.ErrorIs(t, err, gherrors.ErrForbidden)
}

func TestListUserOrgs(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	ctx := context.Background()

	httpmock.RegisterResponder("GET", "https://api.github.com/user/orgs",
		paged("/user/orgs", []map[string]any{{"login": "a"}}, []map[string]any{{"login": "b"}}))
	orgs, err := NewClient("t").ListUserOrgs(ctx)
	require.NoError(t, err)
	require.Len(t, orgs, 2)
	assert.Equal(t, "b", orgs[1].GetLogin())

	httpmock.RegisterResponder("GET", "https://api.github.com/user/orgs", jsonErr(401, "Bad credentials"))
	_, err = NewClient("t").ListUserOrgs(ctx)
	assert.ErrorIs(t, err, gherrors.ErrUnauthorized)
}

func TestGetBranchRef(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	ctx := context.Background()

	httpmock.RegisterResponder("GET", "https://api.github.com/repos/o/r/git/ref/heads/main",
		httpmock.NewJsonResponderOrPanic(200, map[string]any{"ref": "refs/heads/main", "object": map[string]any{"sha": "abc123"}}))
	sha, err := NewClient("t").GetBranchRef(ctx, "o", "r", "main")
	require.NoError(t, err)
	assert.Equal(t, "abc123", sha)

	_, err = NewClient("t").GetBranchRef(ctx, "o", "r", "gone")
	assert.Error(t, err)
}

func TestGetIssuePRCounts_RepositoryMissingFromReply(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("POST", "https://api.github.com/graphql", httpmock.NewStringResponder(200, `{"data":{"repository":null}}`))

	_, _, err := NewClient("t").GetIssuePRCounts(context.Background(), "o", "r")
	assert.ErrorContains(t, err, "o/r not returned")
}

func TestWrapAPIError_TypedRateLimits(t *testing.T) {
	err := wrapAPIError(nil, &gh.RateLimitError{Message: "slow down"})
	assert.ErrorIs(t, err, gherrors.ErrRateLimited)
	assert.ErrorContains(t, err, "slow down")

	err = wrapAPIError(nil, &gh.AbuseRateLimitError{Message: "abuse"})
	assert.ErrorIs(t, err, gherrors.ErrRateLimited)
	assert.ErrorContains(t, err, "abuse")
}

func TestWrapAPIError_OtherStatusKeepsMessage(t *testing.T) {
	resp := &gh.Response{Response: &http.Response{StatusCode: 502}}
	err := wrapAPIError(resp, &gh.ErrorResponse{Message: "bad gateway"})
	var apiErr *gherrors.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 502, apiErr.StatusCode)
	assert.Contains(t, err.Error(), "bad gateway")

	err = wrapAPIError(nil, errors.New("boom"))
	require.ErrorAs(t, err, &apiErr)
	assert.Contains(t, err.Error(), "API request failed")
}

func TestGraphQL_BadBaseURLFailsBeforeSending(t *testing.T) {
	client := NewRESTClient("t")
	client.BaseURL = &url.URL{Path: "no-trailing-slash"}
	assert.Error(t, GraphQL(context.Background(), client, "q", nil, &struct{}{}))
}
