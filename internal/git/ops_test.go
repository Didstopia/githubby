package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstructors(t *testing.T) {
	g, err := NewQuiet()
	require.NoError(t, err)
	assert.True(t, g.Quiet)
	assert.Empty(t, g.Token)

	g, err = NewWithToken("tok")
	require.NoError(t, err)
	assert.Equal(t, "tok", g.Token)
	assert.False(t, g.Quiet)

	g, err = NewQuietWithToken("tok")
	require.NoError(t, err)
	assert.Equal(t, "tok", g.Token)
	assert.True(t, g.Quiet)

	t.Setenv("PATH", t.TempDir())
	_, err = NewQuiet()
	assert.ErrorIs(t, err, ErrGitNotInstalled)
	_, err = NewWithToken("tok")
	assert.ErrorIs(t, err, ErrGitNotInstalled)
	_, err = NewQuietWithToken("tok")
	assert.ErrorIs(t, err, ErrGitNotInstalled)
}

func TestFetchAllAndRefLookups(t *testing.T) {
	ctx := context.Background()
	g := quietGit(t)
	up := newUpstream(t, map[string]string{"a": "a"})
	clone := cloneOf(t, up)

	commitFile(t, up, "b", "b")
	want := runGit(t, "-C", up, "rev-parse", "HEAD")
	require.NoError(t, g.FetchAll(ctx, clone))

	sha, err := g.GetRemoteBranchSHA(ctx, clone, "origin", "main")
	require.NoError(t, err)
	assert.Equal(t, want, sha)

	head, err := g.GetHEAD(ctx, clone)
	require.NoError(t, err)
	assert.NotEqual(t, want, head, "fetch does not move the checkout")

	_, err = g.GetRemoteBranchSHA(ctx, clone, "origin", "nope")
	assert.Error(t, err)
	_, err = g.GetHEAD(ctx, t.TempDir())
	assert.Error(t, err)
}

func TestFetchAll_Errors(t *testing.T) {
	g := quietGit(t)

	err := g.FetchAll(context.Background(), t.TempDir())
	assert.ErrorIs(t, err, ErrFetchFailed)
	assert.Contains(t, err.Error(), "not a git repository")

	// A cancelled context gives no stderr, so the error carries the exec error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = g.FetchAll(ctx, cloneOf(t, newUpstream(t, map[string]string{"a": "a"})))
	assert.ErrorIs(t, err, ErrFetchFailed)
}

func TestFetchAll_ScrubsStoredCredentials(t *testing.T) {
	g := quietGit(t)
	repo := t.TempDir()
	runGit(t, "init", "-q", repo)
	runGit(t, "-C", repo, "remote", "add", "origin", "https://oauth2:old@127.0.0.1:1/x.git")

	_ = g.FetchAll(context.Background(), repo) // fails: nothing listens there
	assert.Equal(t, "https://127.0.0.1:1/x.git", runGit(t, "-C", repo, "remote", "get-url", "origin"))
}

func TestFetchAll_NotQuietPrintsToTerminal(t *testing.T) {
	g, err := New()
	require.NoError(t, err)
	up := newUpstream(t, map[string]string{"a": "a"})
	clone := cloneOf(t, up)
	commitFile(t, up, "b", "b")
	require.NoError(t, g.FetchAll(context.Background(), clone))
	assert.ErrorIs(t, g.FetchAll(context.Background(), t.TempDir()), ErrFetchFailed)
}

func TestScrubRemoteURL_Errors(t *testing.T) {
	g := quietGit(t)
	ctx := context.Background()

	t.Run("no origin", func(t *testing.T) {
		repo := t.TempDir()
		runGit(t, "init", "-q", repo)
		assert.Error(t, g.ScrubRemoteURL(ctx, repo))
	})
	t.Run("url without credentials is left alone", func(t *testing.T) {
		repo := t.TempDir()
		runGit(t, "init", "-q", repo)
		runGit(t, "-C", repo, "remote", "add", "origin", "https://github.com/o/r.git")
		require.NoError(t, g.ScrubRemoteURL(ctx, repo))
		assert.Equal(t, "https://github.com/o/r.git", runGit(t, "-C", repo, "remote", "get-url", "origin"))
	})
	t.Run("local path remote is left alone", func(t *testing.T) {
		repo := t.TempDir()
		runGit(t, "init", "-q", repo)
		runGit(t, "-C", repo, "remote", "add", "origin", "/some/path")
		require.NoError(t, g.ScrubRemoteURL(ctx, repo))
	})
}

func TestGetLastFetchTime(t *testing.T) {
	g := quietGit(t)
	clone := cloneOf(t, newUpstream(t, map[string]string{"a": "a"}))

	_, err := g.GetLastFetchTime(clone)
	assert.Error(t, err, "never fetched")

	fh := filepath.Join(clone, ".git", "FETCH_HEAD")
	require.NoError(t, os.WriteFile(fh, nil, 0644))
	when := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, os.Chtimes(fh, when, when))

	got, err := g.GetLastFetchTime(clone)
	require.NoError(t, err)
	assert.True(t, when.Equal(got))
}

func TestRun_NotQuietAndErrors(t *testing.T) {
	g, err := New()
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, g.run(ctx, nil, ErrFetchFailed, "--version"))

	err = g.run(ctx, nil, ErrFetchFailed, "-C", t.TempDir(), "rev-parse", "HEAD")
	assert.ErrorIs(t, err, ErrFetchFailed)

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	assert.ErrorIs(t, g.run(cctx, nil, ErrFetchFailed, "--version"), ErrFetchFailed)
}

func TestOutput_CancelledContextHasNoStderr(t *testing.T) {
	g := quietGit(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.output(ctx, nil, ErrLsRemoteFailed, "--version")
	assert.ErrorIs(t, err, ErrLsRemoteFailed)
}

func TestLFSObjectOK_UnreadableObject(t *testing.T) {
	// A directory opens fine but cannot be read
	assert.False(t, lfsObjectOK(t.TempDir(), "abc"))
}

func TestRepoUsesLFS_UnreadableAttributes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitattributes")
	require.NoError(t, os.WriteFile(path, []byte("*.bin filter=lfs"), 0000))
	assert.False(t, (&LFS{}).RepoUsesLFS(dir))
}

func TestSubmoduleUpdate_SyncFailureIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a shell script")
	}
	gitPath := filepath.Join(t.TempDir(), "git")
	require.NoError(t, os.WriteFile(gitPath, []byte("#!/bin/sh\necho sync broke >&2\nexit 1\n"), 0755))
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitmodules"), nil, 0644))

	err := (&Git{GitPath: gitPath, Quiet: true}).SubmoduleUpdate(context.Background(), repo)
	assert.ErrorIs(t, err, ErrSubmoduleFailed)
	assert.Contains(t, err.Error(), "sync broke")
}

func TestSubmoduleStatus_SkipsMalformedLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a shell script")
	}
	// A stand-in git that prints odd lines: a bare SHA, a short line and a good one
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf ' abc\\n\\n+def lib/x (heads/main)\\n'\n"
	gitPath := filepath.Join(dir, "git")
	require.NoError(t, os.WriteFile(gitPath, []byte(script), 0755))
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitmodules"), nil, 0644))

	subs, err := (&Git{GitPath: gitPath}).SubmoduleStatus(context.Background(), repo)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "lib/x", subs[0].Path)
	assert.Equal(t, byte('+'), subs[0].State)
}
