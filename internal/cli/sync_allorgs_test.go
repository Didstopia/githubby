package cli

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gitpkg "github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
	"github.com/Didstopia/githubby/internal/metadata"
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

	syncTarget = t.TempDir()
	result, report, err := runFlagSync(context.Background(), m, g, nil)
	require.NoError(t, err)
	assert.Nil(t, report)

	assert.ElementsMatch(t, []string{"Dids/mine", "Didstopia/tool"}, result.Cloned)
	assert.DirExists(t, filepath.Join(syncTarget, "Didstopia", "tool", ".git"))
	assert.Contains(t, result.Failed, "Gone")
	assert.ErrorContains(t, failedError(result, nil), "1 item(s) failed")
}

func TestListFlagTargets(t *testing.T) {
	resetSyncFlags(t)
	m := github.NewMockClient()
	syncer := synpkg.New(m, &gitpkg.Git{}, &synpkg.Options{Target: t.TempDir(), DryRun: true})

	syncAllOrgs, syncUser, syncOrg = false, "someone", ""
	targets, err := listFlagTargets(context.Background(), m, syncer)
	require.NoError(t, err)
	assert.Equal(t, 1, m.CallCount("ListUserRepos"))
	assert.Equal(t, []string{"someone"}, targets.users)

	syncUser, syncOrg = "", "org"
	targets, err = listFlagTargets(context.Background(), m, syncer)
	require.NoError(t, err)
	assert.Equal(t, 1, m.CallCount("ListOrgRepos"))
	assert.Equal(t, []string{"org"}, targets.orgs)

	m.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		return nil, errors.New("down")
	}
	_, err = listFlagTargets(context.Background(), m, syncer)
	assert.ErrorContains(t, err, "failed to list repos")
}

func TestFailedError(t *testing.T) {
	assert.NoError(t, failedError(nil, nil))
	assert.NoError(t, failedError(synpkg.NewResult(), metadata.NewReport()))
	report := metadata.NewReport()
	report.Failed["owner/repo"] = errors.New("x")
	assert.ErrorContains(t, failedError(synpkg.NewResult(), report), "1 item(s) failed")
}

func TestRunFlagSync_Gists(t *testing.T) {
	g, err := gitpkg.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	resetSyncFlags(t)
	gists := syncGists
	t.Cleanup(func() { syncGists = gists })

	up := localUpstream(t)
	m := github.NewMockClient()
	m.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) { return &gh.User{Login: gh.Ptr("Dids")}, nil }
	m.ListUserReposFunc = func(ctx context.Context, u string, o *github.ListOptions) ([]*gh.Repository, error) {
		return nil, errors.New("api down")
	}
	m.ListGistsFunc = func(ctx context.Context) ([]*gh.Gist, error) {
		return []*gh.Gist{{ID: gh.Ptr("g1"), GitPullURL: gh.Ptr(up)}}, nil
	}

	syncTarget, syncUser, syncOrg, syncAllOrgs, syncGists = t.TempDir(), "Dids", "", false, true
	result, _, err := runFlagSync(context.Background(), m, g, nil)

	// Repo listing failed, but gists still synced
	assert.ErrorContains(t, err, "api down")
	require.NotNil(t, result)
	assert.Equal(t, []string{"gist g1"}, result.Gists)
	assert.DirExists(t, filepath.Join(syncTarget, "Dids.gists", "g1", ".git"))

	m.ListGistsFunc = func(ctx context.Context) ([]*gh.Gist, error) { return nil, errors.New("no gist scope") }
	result, _, _ = runFlagSync(context.Background(), m, g, nil)
	assert.Contains(t, result.Failed, "gists")
}

func TestRunFlagSync_MetadataRunsWhenGitFails(t *testing.T) {
	resetSyncFlags(t)
	exclude, dry := syncExclude, dryRun
	t.Cleanup(func() { syncExclude, dryRun = exclude, dry })

	httpmock.Activate()
	t.Cleanup(httpmock.DeactivateAndReset)
	httpmock.RegisterResponder("GET", "https://api.github.com/repos/Didstopia/tool/labels",
		httpmock.NewJsonResponderOrPanic(200, []map[string]any{{"name": "bug"}}))

	m := github.NewMockClient()
	m.ListOrgReposFunc = func(ctx context.Context, org string, o *github.ListOptions) ([]*gh.Repository, error) {
		return []*gh.Repository{
			repoAt(org, "tool", filepath.Join(t.TempDir(), "missing")),
			repoAt(org, "skipme", filepath.Join(t.TempDir(), "missing")),
		}, nil
	}

	syncTarget, syncUser, syncOrg, syncAllOrgs, syncExclude, dryRun = t.TempDir(), "", "Didstopia", false, []string{"skipme"}, false
	exporter := metadata.New(gh.NewClient(nil), nil, syncTarget)

	g := &gitpkg.Git{GitPath: "git", Quiet: true}
	result, report, err := runFlagSync(context.Background(), m, g, exporter)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Contains(t, result.Failed, "Didstopia/tool", "git clone fails")
	assert.Contains(t, report.Failed, "Didstopia/tool", "metadata still ran for the repo")
	assert.NotContains(t, report.Failed, "Didstopia/skipme", "filtered repos get no metadata")
	assert.Contains(t, report.Failed, "org Didstopia")
	assert.FileExists(t, filepath.Join(syncTarget, "Didstopia", "tool.meta", "labels.json"))
	assert.Error(t, failedError(result, report))

	dryRun = true
	_, report, err = runFlagSync(context.Background(), m, g, exporter)
	require.NoError(t, err)
	assert.Nil(t, report)
}

func TestPrintMetadataSummary(t *testing.T) {
	report := metadata.NewReport()
	report.Exported = []string{"a/b"}
	report.Warnings = []string{"a/b: hooks.json skipped"}
	report.Failed["a/c"] = errors.New("boom")
	assert.NotPanics(t, func() {
		printMetadataSummary(nil)
		printMetadataSummary(report)
	})
}
