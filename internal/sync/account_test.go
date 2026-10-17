package sync

import (
	"context"
	"errors"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

func accountMock() *github.MockClient {
	m := github.NewMockClient()
	m.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) {
		return &gh.User{Login: gh.Ptr("dids")}, nil
	}
	m.ListUserReposFunc = func(ctx context.Context, username string, opts *github.ListOptions) ([]*gh.Repository, error) {
		return []*gh.Repository{createMockRepo("mine", username+"/mine", true)}, nil
	}
	m.ListUserOrgsFunc = func(ctx context.Context) ([]*gh.Organization, error) {
		return []*gh.Organization{{Login: gh.Ptr("Didstopia")}, {Login: gh.Ptr("Broken")}}, nil
	}
	m.ListOrgReposFunc = func(ctx context.Context, org string, opts *github.ListOptions) ([]*gh.Repository, error) {
		if org == "Broken" {
			return nil, errors.New("boom")
		}
		return []*gh.Repository{
			createMockRepo("tool", org+"/tool", false),
			createMockRepo("mine", "dids/mine", true), // duplicate is dropped
		}, nil
	}
	return m
}

func TestListAccountRepos(t *testing.T) {
	m := accountMock()
	syncer := New(m, &git.Git{}, &Options{IncludePrivate: true})

	repos, failed, err := syncer.ListAccountRepos(context.Background(), "")
	require.NoError(t, err)

	var names []string
	for _, r := range repos {
		names = append(names, r.GetFullName())
	}
	assert.Equal(t, []string{"dids/mine", "Didstopia/tool"}, names)
	require.Contains(t, failed, "Broken")
	assert.Contains(t, failed["Broken"].Error(), "boom")

	// The user's own private repos were asked for
	for _, c := range m.Calls {
		if c.Method == "ListUserRepos" {
			assert.Equal(t, "dids", c.Args[0])
			assert.True(t, c.Args[1].(*github.ListOptions).IncludePrivate)
		}
	}
}

func TestListAccountRepos_ExplicitUserSkipsLookup(t *testing.T) {
	m := accountMock()
	syncer := New(m, &git.Git{}, &Options{})

	repos, _, err := syncer.ListAccountRepos(context.Background(), "someone")
	require.NoError(t, err)
	assert.Equal(t, "someone/mine", repos[0].GetFullName())
	assert.Equal(t, 0, m.CallCount("GetAuthenticatedUser"))
}

func TestListAccountRepos_Errors(t *testing.T) {
	t.Run("user lookup fails", func(t *testing.T) {
		m := accountMock()
		m.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) { return nil, errors.New("401") }
		_, _, err := New(m, &git.Git{}, &Options{}).ListAccountRepos(context.Background(), "")
		assert.ErrorContains(t, err, "authenticated user")
	})

	t.Run("user repos fail", func(t *testing.T) {
		m := accountMock()
		m.ListUserReposFunc = func(ctx context.Context, u string, o *github.ListOptions) ([]*gh.Repository, error) {
			return nil, errors.New("down")
		}
		_, _, err := New(m, &git.Git{}, &Options{}).ListAccountRepos(context.Background(), "x")
		assert.ErrorContains(t, err, "user repos")
	})

	t.Run("org list fails", func(t *testing.T) {
		m := accountMock()
		m.ListUserOrgsFunc = func(ctx context.Context) ([]*gh.Organization, error) { return nil, errors.New("down") }
		_, _, err := New(m, &git.Git{}, &Options{}).ListAccountRepos(context.Background(), "x")
		assert.ErrorContains(t, err, "orgs")
	})
}

func TestSyncRepos_LayoutIsOwnerSlashRepo(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	upstream := newUpstream(t, map[string]string{"README": "x"})
	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target})

	result, err := syncer.SyncRepos(context.Background(), []*gh.Repository{
		localRepo("dids", "a", upstream),
		localRepo("Didstopia", "b", upstream),
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"dids/a", "Didstopia/b"}, result.Cloned)
	assert.DirExists(t, target+"/dids/a/.git")
	assert.DirExists(t, target+"/Didstopia/b/.git")
	assert.Empty(t, result.Archived)
	assert.True(t, syncer.Matches(localRepo("x", "a", "")))
}
