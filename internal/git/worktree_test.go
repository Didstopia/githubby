package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commitFile adds a commit to a repo
func commitFile(t *testing.T, repo, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte(body), 0644))
	runGit(t, "-C", repo, "add", "-A")
	runGit(t, "-C", repo, "-c", "user.email=t@e", "-c", "user.name=T", "commit", "-q", "-m", "add "+name)
}

// cloneOf clones up into a new directory and returns it
func cloneOf(t *testing.T, up string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	runGit(t, "clone", "-q", up, dir)
	return dir
}

func quietGit(t *testing.T) *Git {
	t.Helper()
	g, err := NewQuiet()
	require.NoError(t, err)
	return g
}

func TestUpdateWorkTree(t *testing.T) {
	ctx := context.Background()
	g := quietGit(t)

	t.Run("branch that was never fetched is left alone", func(t *testing.T) {
		moved, err := g.UpdateWorkTree(ctx, cloneOf(t, newUpstream(t, map[string]string{"a": "a"})), "nope")
		require.NoError(t, err)
		assert.False(t, moved)
	})

	t.Run("up to date checkout does not move", func(t *testing.T) {
		clone := cloneOf(t, newUpstream(t, map[string]string{"a": "a"}))
		moved, err := g.UpdateWorkTree(ctx, clone, "main")
		require.NoError(t, err)
		assert.False(t, moved)
	})

	t.Run("fast-forwards a clean checkout", func(t *testing.T) {
		up := newUpstream(t, map[string]string{"a": "a"})
		clone := cloneOf(t, up)
		commitFile(t, up, "b", "b")
		require.NoError(t, g.FetchAll(ctx, clone))

		moved, err := g.UpdateWorkTree(ctx, clone, "main")
		require.NoError(t, err)
		assert.True(t, moved)
		assert.FileExists(t, filepath.Join(clone, "b"))
	})

	t.Run("switches to the branch when another one is checked out", func(t *testing.T) {
		up := newUpstream(t, map[string]string{"a": "a"})
		clone := cloneOf(t, up)
		runGit(t, "-C", clone, "checkout", "-q", "-b", "other")
		commitFile(t, up, "b", "b")
		require.NoError(t, g.FetchAll(ctx, clone))

		moved, err := g.UpdateWorkTree(ctx, clone, "main")
		require.NoError(t, err)
		assert.True(t, moved)
		assert.Equal(t, "main", runGit(t, "-C", clone, "symbolic-ref", "--short", "HEAD"))
		assert.FileExists(t, filepath.Join(clone, "b"))
	})

	t.Run("creates a tracking branch that exists only on the remote", func(t *testing.T) {
		up := newUpstream(t, map[string]string{"a": "a"})
		runGit(t, "-C", up, "branch", "feature")
		clone := cloneOf(t, up)
		require.NoError(t, g.FetchAll(ctx, clone))

		moved, err := g.UpdateWorkTree(ctx, clone, "feature")
		require.NoError(t, err)
		assert.True(t, moved)
		assert.Equal(t, "feature", runGit(t, "-C", clone, "symbolic-ref", "--short", "HEAD"))
	})

	t.Run("local changes are never overwritten", func(t *testing.T) {
		up := newUpstream(t, map[string]string{"a": "a"})
		clone := cloneOf(t, up)
		commitFile(t, up, "b", "b")
		require.NoError(t, g.FetchAll(ctx, clone))
		require.NoError(t, os.WriteFile(filepath.Join(clone, "a"), []byte("mine"), 0644))

		moved, err := g.UpdateWorkTree(ctx, clone, "main")
		assert.ErrorIs(t, err, ErrDirtyWorkTree)
		assert.False(t, moved)
		body, _ := os.ReadFile(filepath.Join(clone, "a"))
		assert.Equal(t, "mine", string(body))
	})

	t.Run("a diverged branch fails and keeps the local commit", func(t *testing.T) {
		up := newUpstream(t, map[string]string{"a": "a"})
		clone := cloneOf(t, up)
		commitFile(t, clone, "local", "x")
		localHead := runGit(t, "-C", clone, "rev-parse", "HEAD")
		commitFile(t, up, "b", "b")
		require.NoError(t, g.FetchAll(ctx, clone))

		moved, err := g.UpdateWorkTree(ctx, clone, "main")
		assert.ErrorIs(t, err, ErrCheckoutFailed)
		assert.False(t, moved)
		assert.Equal(t, localHead, runGit(t, "-C", clone, "rev-parse", "HEAD"))
	})

	t.Run("a failed fast-forward after a switch still reports the move", func(t *testing.T) {
		up := newUpstream(t, map[string]string{"a": "a"})
		clone := cloneOf(t, up)
		runGit(t, "-C", clone, "checkout", "-q", "-b", "other")
		runGit(t, "-C", clone, "checkout", "-q", "main")
		commitFile(t, clone, "local", "x")
		runGit(t, "-C", clone, "checkout", "-q", "other")
		commitFile(t, up, "b", "b")
		require.NoError(t, g.FetchAll(ctx, clone))

		moved, err := g.UpdateWorkTree(ctx, clone, "main")
		assert.ErrorIs(t, err, ErrCheckoutFailed)
		assert.True(t, moved, "HEAD did change to main")
	})

	t.Run("a checkout that git refuses fails", func(t *testing.T) {
		up := newUpstream(t, map[string]string{"a": "a"})
		clone := cloneOf(t, up)
		commitFile(t, up, "z", "from upstream")
		require.NoError(t, g.FetchAll(ctx, clone))
		runGit(t, "-C", clone, "merge", "-q", "--ff-only", "origin/main")
		runGit(t, "-C", clone, "checkout", "-q", "-b", "other", "HEAD~1")
		// An untracked file that main would overwrite
		require.NoError(t, os.WriteFile(filepath.Join(clone, "z"), []byte("mine"), 0644))

		moved, err := g.UpdateWorkTree(ctx, clone, "main")
		assert.ErrorIs(t, err, ErrCheckoutFailed)
		assert.False(t, moved)
		body, _ := os.ReadFile(filepath.Join(clone, "z"))
		assert.Equal(t, "mine", string(body))
	})

	t.Run("status failure is returned", func(t *testing.T) {
		clone := cloneOf(t, newUpstream(t, map[string]string{"a": "a"}))
		commitFile(t, clone, "x", "x") // head != remote, so status runs
		require.NoError(t, os.WriteFile(filepath.Join(clone, ".git", "index"), []byte("garbage"), 0644))

		_, err := g.UpdateWorkTree(ctx, clone, "main")
		assert.Error(t, err)
	})
}

func TestLsRemoteDefaultBranch(t *testing.T) {
	ctx := context.Background()
	g := quietGit(t)

	up := newUpstream(t, map[string]string{"a": "a"})
	branch, err := g.LsRemoteDefaultBranch(ctx, up)
	require.NoError(t, err)
	assert.Equal(t, "main", branch)

	empty := filepath.Join(t.TempDir(), "empty")
	runGit(t, "init", "-q", "-b", "main", empty)
	branch, err = g.LsRemoteDefaultBranch(ctx, empty)
	require.NoError(t, err)
	assert.Equal(t, "", branch, "an empty repo has no default branch")

	_, err = g.LsRemoteDefaultBranch(ctx, filepath.Join(t.TempDir(), "missing"))
	assert.ErrorIs(t, err, ErrLsRemoteFailed)
}

// The LFS filter downloads files while the work tree moves, so it needs the token
func TestUpdateWorkTree_FiltersGetTheToken(t *testing.T) {
	up := newUpstream(t, map[string]string{".gitattributes": "b filter=probe\n"})
	clone := cloneOf(t, up)
	// Stands in for git-lfs: fails unless the auth header reaches it
	runGit(t, "-C", clone, "config", "filter.probe.smudge", "git config --get http.https://github.com/.extraheader >/dev/null && cat")
	runGit(t, "-C", clone, "config", "filter.probe.required", "true")
	commitFile(t, up, "b", "b")

	g := quietGit(t)
	g.Token = "tok"
	require.NoError(t, g.FetchAll(context.Background(), clone))
	moved, err := g.UpdateWorkTree(context.Background(), clone, "main")
	require.NoError(t, err)
	assert.True(t, moved)
	assert.FileExists(t, filepath.Join(clone, "b"))
}
