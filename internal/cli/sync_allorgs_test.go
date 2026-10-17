package cli

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gitpkg "github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	synpkg "github.com/Didstopia/githubby/internal/sync"
)

// resetSyncFlags restores the sync flag globals after a test
func resetSyncFlags(t *testing.T) {
	t.Helper()
	user, org, target, all := syncUser, syncOrg, syncTarget, syncAllOrgs
	t.Cleanup(func() { syncUser, syncOrg, syncTarget, syncAllOrgs = user, org, target, all })
}

// localUpstream creates a git repo with one commit to clone from
func localUpstream(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "up")
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=T", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		out, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	return dir
}

func repoAt(owner, name, url string) *gh.Repository {
	return &gh.Repository{
		Name: gh.Ptr(name), FullName: gh.Ptr(owner + "/" + name),
		Owner: &gh.User{Login: gh.Ptr(owner)}, CloneURL: gh.Ptr(url),
	}
}

func TestRunSync_RequiresSource(t *testing.T) {
	resetSyncFlags(t)
	syncTarget, syncUser, syncOrg, syncAllOrgs = t.TempDir(), "", "", false
	err := runSync(syncCmd, nil)
	assert.ErrorContains(t, err, "--all-orgs")
}

func TestSyncListedRepos_AllOrgs(t *testing.T) {
	g, err := gitpkg.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	resetSyncFlags(t)
	syncAllOrgs, syncUser = true, ""

	up := localUpstream(t)
	m := github.NewMockClient()
	m.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) { return &gh.User{Login: gh.Ptr("Dids")}, nil }
	m.ListUserReposFunc = func(ctx context.Context, u string, o *github.ListOptions) ([]*gh.Repository, error) {
		return []*gh.Repository{repoAt("Dids", "mine", up)}, nil
	}
	m.ListUserOrgsFunc = func(ctx context.Context) ([]*gh.Organization, error) {
		return []*gh.Organization{{Login: gh.Ptr("Didstopia")}, {Login: gh.Ptr("Gone")}}, nil
	}
	m.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		if org == "Gone" {
			return nil, errors.New("forbidden")
		}
		return []*gh.Repository{repoAt(org, "tool", up)}, nil
	}

	target := t.TempDir()
	syncer := synpkg.New(m, g, &synpkg.Options{Target: target})
	result, err := syncListedRepos(context.Background(), m, syncer)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"Dids/mine", "Didstopia/tool"}, result.Cloned)
	assert.DirExists(t, filepath.Join(target, "Didstopia", "tool", ".git"))
	assert.Contains(t, result.Failed, "Gone")
	assert.ErrorContains(t, failedError(result), "1 item(s) failed")
}

func TestSyncListedRepos_UserAndOrg(t *testing.T) {
	resetSyncFlags(t)
	m := github.NewMockClient()
	syncer := synpkg.New(m, &gitpkg.Git{}, &synpkg.Options{Target: t.TempDir(), DryRun: true})

	syncAllOrgs, syncUser, syncOrg = false, "someone", ""
	_, err := syncListedRepos(context.Background(), m, syncer)
	require.NoError(t, err)
	assert.Equal(t, 1, m.CallCount("ListUserRepos"))

	syncUser, syncOrg = "", "org"
	_, err = syncListedRepos(context.Background(), m, syncer)
	require.NoError(t, err)
	assert.Equal(t, 1, m.CallCount("ListOrgRepos"))

	m.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		return nil, errors.New("down")
	}
	_, err = syncListedRepos(context.Background(), m, syncer)
	assert.ErrorContains(t, err, "failed to list repos")
}

func TestFailedError(t *testing.T) {
	assert.NoError(t, failedError(nil))
	assert.NoError(t, failedError(synpkg.NewResult()))
}
