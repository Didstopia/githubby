package sync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

func TestSync_FetchesPullRefsEvenWhenUpToDate(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()

	up := newUpstream(t, map[string]string{"a.txt": "a"})
	runGit(t, "-C", up, "update-ref", "refs/pull/1/head", "HEAD")
	target := t.TempDir()
	local := filepath.Join(target, "owner", "repo")

	// Without the option no pull refs are fetched
	_, err = New(github.NewMockClient(), g, &Options{Target: t.TempDir()}).SyncRepoWithData(ctx, localRepo("owner", "repo", up))
	require.NoError(t, err)

	syncer := New(github.NewMockClient(), g, &Options{Target: target, PullRefs: true})
	result, err := syncer.SyncRepoWithData(ctx, localRepo("owner", "repo", up))
	require.NoError(t, err)
	require.Empty(t, result.Warnings)
	refs, err := g.PullRefs(ctx, local)
	require.NoError(t, err)
	assert.Len(t, refs, 1)

	// FETCH_HEAD is the time of the last full fetch; pull refs don't touch it
	_, err = g.GetLastFetchTime(local)
	require.Error(t, err)
	runGit(t, "-C", local, "fetch", "-q")

	// A fork opens a pull request; pushed_at of the repo doesn't move
	runGit(t, "-C", up, "commit", "-q", "--allow-empty", "-m", "fork commit")
	pr := runGit(t, "-C", up, "rev-parse", "HEAD")
	runGit(t, "-C", up, "reset", "-q", "--hard", "HEAD~1")
	runGit(t, "-C", up, "update-ref", "refs/pull/2/head", pr)

	repo := localRepo("owner", "repo", up)
	repo.PushedAt = &gh.Timestamp{Time: time.Now().Add(-time.Hour)}
	result, err = syncer.SyncRepoWithData(ctx, repo)
	require.NoError(t, err)
	assert.Equal(t, []string{"owner/repo"}, result.UpToDate)
	refs, err = g.PullRefs(ctx, local)
	require.NoError(t, err)
	assert.Equal(t, pr, refs["2"])
}

func TestSync_PullRefFailureIsWarning(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	up := newUpstream(t, map[string]string{"a.txt": "a"})
	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target, PullRefs: true})
	_, err = syncer.SyncRepoWithData(context.Background(), localRepo("owner", "repo", up))
	require.NoError(t, err)

	local := filepath.Join(target, "owner", "repo")
	runGit(t, "-C", local, "fetch", "-q")
	runGit(t, "-C", local, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))

	// Fast sync skips the main fetch, then the pull ref fetch fails
	repo := localRepo("owner", "repo", up)
	repo.PushedAt = &gh.Timestamp{Time: time.Now().Add(-time.Hour)}
	result, err := syncer.SyncRepoWithData(context.Background(), repo)
	require.NoError(t, err)
	assert.Empty(t, result.Failed)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "owner/repo: pull request refs:")
}
