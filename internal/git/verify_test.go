package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLsRemoteAndRemoteBranches(t *testing.T) {
	g, err := NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()

	up := newUpstream(t, map[string]string{"a": "a"})
	runGit(t, "-C", up, "branch", "side")
	clone := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, g.Clone(ctx, up, clone))

	remote, err := g.LsRemoteHeads(ctx, up)
	require.NoError(t, err)
	local, err := g.RemoteBranches(ctx, clone)
	require.NoError(t, err)

	head := runGit(t, "-C", up, "rev-parse", "HEAD")
	assert.Equal(t, map[string]string{"main": head, "side": head}, remote)
	assert.Equal(t, remote, local, "origin/HEAD is left out")

	_, err = g.LsRemoteHeads(ctx, filepath.Join(t.TempDir(), "missing"))
	assert.ErrorIs(t, err, ErrLsRemoteFailed)
	_, err = g.RemoteBranches(ctx, t.TempDir())
	assert.Error(t, err)
}

func TestLsRemoteHeads_SendsToken(t *testing.T) {
	gitPath, logPath := fakeGit(t)
	g := &Git{GitPath: gitPath, Token: "secret"}
	_, err := g.LsRemoteHeads(context.Background(), "https://github.com/o/r.git")
	require.NoError(t, err)
	log := readLog(t, logPath)
	assert.Contains(t, log, "ARGS ls-remote --heads https://github.com/o/r.git")
	assert.Contains(t, log, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader")
}

func TestFsck(t *testing.T) {
	g, err := NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()

	up := newUpstream(t, map[string]string{"a": "hello"})
	require.NoError(t, g.Fsck(ctx, up))

	// Break a loose object
	blob := runGit(t, "-C", up, "rev-parse", "HEAD:a")
	obj := filepath.Join(up, ".git", "objects", blob[:2], blob[2:])
	require.NoError(t, os.Chmod(obj, 0644))
	require.NoError(t, os.WriteFile(obj, []byte("garbage"), 0644))
	assert.ErrorIs(t, g.Fsck(ctx, up), ErrFsckFailed)

	assert.ErrorIs(t, g.Fsck(ctx, t.TempDir()), ErrFsckFailed)
}

func TestMissingLFSObjects(t *testing.T) {
	lfsPath, err := exec.LookPath("git-lfs")
	if err != nil {
		t.Skip("git-lfs is not installed")
	}
	g, err := NewQuiet()
	require.NoError(t, err)
	ctx := context.Background()
	lfs := &LFS{LFSPath: lfsPath, Git: g, Quiet: true}

	_, err = (&LFS{Git: g}).MissingLFSObjects(ctx, t.TempDir())
	assert.ErrorIs(t, err, ErrLFSNotInstalled)

	runGit(t, "lfs", "install", "--skip-repo")
	up := newUpstream(t, map[string]string{"a.txt": "a"})
	runGit(t, "-C", up, "lfs", "track", "*.bin")
	runGit(t, "-C", up, "checkout", "-q", "-b", "side")
	require.NoError(t, os.WriteFile(filepath.Join(up, "big file.bin"), []byte("lfs data"), 0644))
	runGit(t, "-C", up, "add", ".")
	runGit(t, "-C", up, "-c", "user.email=t@e", "-c", "user.name=T", "commit", "-q", "-m", "lfs")
	runGit(t, "-C", up, "checkout", "-q", "main")

	clone := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, g.Clone(ctx, up, clone))

	missing, err := lfs.MissingLFSObjects(ctx, clone)
	require.NoError(t, err)
	require.Len(t, missing, 1, "plain clone has no LFS objects of other branches")
	assert.True(t, strings.HasPrefix(missing[0], "big file.bin ("))

	require.NoError(t, lfs.FetchAll(ctx, clone))
	missing, err = lfs.MissingLFSObjects(ctx, clone)
	require.NoError(t, err)
	assert.Empty(t, missing)

	// A corrupt object counts as missing
	oid := strings.Fields(runGit(t, "-C", clone, "lfs", "ls-files", "--all", "--long"))[0]
	obj := filepath.Join(clone, ".git", "lfs", "objects", oid[0:2], oid[2:4], oid)
	require.NoError(t, os.WriteFile(obj, []byte("tampered"), 0644))
	missing, err = lfs.MissingLFSObjects(ctx, clone)
	require.NoError(t, err)
	assert.Len(t, missing, 1)

	// An empty repo has nothing to check
	empty := t.TempDir()
	runGit(t, "init", "-q", empty)
	missing, err = lfs.MissingLFSObjects(ctx, empty)
	require.NoError(t, err)
	assert.Empty(t, missing)

	_, err = lfs.MissingLFSObjects(ctx, filepath.Join(t.TempDir(), "nope"))
	assert.ErrorIs(t, err, ErrLFSListFailed)
}

func TestPullRefs(t *testing.T) {
	g, err := NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()

	up := newUpstream(t, map[string]string{"a": "a"})
	runGit(t, "-C", up, "checkout", "-q", "-b", "gone")
	runGit(t, "-C", up, "commit", "-q", "--allow-empty", "-m", "only on a deleted branch")
	pr := runGit(t, "-C", up, "rev-parse", "HEAD")
	runGit(t, "-C", up, "checkout", "-q", "main")
	runGit(t, "-C", up, "branch", "-q", "-D", "gone")
	runGit(t, "-C", up, "update-ref", "refs/pull/7/head", pr)
	runGit(t, "-C", up, "update-ref", "refs/pull/7/merge", pr)

	clone := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, g.Clone(ctx, up, clone))
	local, err := g.PullRefs(ctx, clone)
	require.NoError(t, err)
	assert.Empty(t, local, "a plain clone has no pull refs")

	require.NoError(t, g.FetchPullRefs(ctx, clone))
	remote, err := g.LsRemotePulls(ctx, up)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"7": pr}, remote, "merge refs are left out")
	local, err = g.PullRefs(ctx, clone)
	require.NoError(t, err)
	assert.Equal(t, remote, local)
	runGit(t, "-C", clone, "cat-file", "-e", pr)

	// A repo without pull requests is fine
	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, g.Clone(ctx, newUpstream(t, map[string]string{"b": "b"}), empty))
	require.NoError(t, g.FetchPullRefs(ctx, empty))

	_, err = g.LsRemotePulls(ctx, filepath.Join(t.TempDir(), "missing"))
	assert.ErrorIs(t, err, ErrLsRemoteFailed)
	_, err = g.PullRefs(ctx, t.TempDir())
	assert.Error(t, err)
}
