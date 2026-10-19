package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

// lfsUpstream makes a repo whose only LFS file sits on a side branch
func lfsUpstream(t *testing.T) string {
	t.Helper()
	runGit(t, "lfs", "install", "--skip-repo")
	up := newUpstream(t, map[string]string{"a.txt": "a"})
	runGit(t, "-C", up, "lfs", "track", "*.bin")
	runGit(t, "-C", up, "checkout", "-q", "-b", "side")
	require.NoError(t, os.WriteFile(filepath.Join(up, "big.bin"), []byte("lfs data"), 0644))
	runGit(t, "-C", up, "add", ".")
	runGit(t, "-C", up, "commit", "-q", "-m", "lfs")
	runGit(t, "-C", up, "checkout", "-q", "main")
	return up
}

func TestSync_FetchesLFSObjectsOfAllBranches(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs is not installed")
	}
	g, err := git.NewQuiet()
	require.NoError(t, err)

	up := lfsUpstream(t)
	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target, LFSAll: true})

	result, err := syncer.SyncRepoWithData(context.Background(), localRepo("owner", "lfs", up))
	require.NoError(t, err)
	assert.Empty(t, result.Warnings)

	clone := filepath.Join(target, "owner", "lfs")
	oid := strings.Fields(runGit(t, "-C", clone, "lfs", "ls-files", "--all", "--long"))[0]
	assert.FileExists(t, filepath.Join(clone, ".git", "lfs", "objects", oid[0:2], oid[2:4], oid))
}

func TestSync_LFSAllOffKeepsOldBehaviour(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs is not installed")
	}
	g, err := git.NewQuiet()
	require.NoError(t, err)

	up := lfsUpstream(t)
	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target})

	_, err = syncer.SyncRepoWithData(context.Background(), localRepo("owner", "lfs", up))
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(target, "owner", "lfs", ".git", "lfs", "objects"))
}

func TestAfterSync_LFSMissingIsWarning(t *testing.T) {
	// Calls afterSync directly: a full clone would try to install git-lfs
	repoDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, ".gitattributes"), []byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0644))

	syncer := New(github.NewMockClient(), &git.Git{}, &Options{LFSAll: true})
	syncer.lfs.LFSPath = ""

	result := NewResult()
	syncer.afterSync(context.Background(), localRepo("owner", "lfs", ""), repoDir, true, result)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "git-lfs is not installed")

	// Nothing new from fast sync, but the LFS files were never fetched: still a warning
	result = NewResult()
	syncer.afterSync(context.Background(), localRepo("owner", "lfs", ""), repoDir, false, result)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "git-lfs is not installed")
}

// An LFS download that never finished must be retried even when the repo has nothing new
func TestAfterSync_RetriesLFSUntilItSucceeds(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs is not installed")
	}
	g, err := git.NewQuiet()
	require.NoError(t, err)

	up := lfsUpstream(t)
	target := t.TempDir()
	// A clone without LFS stands in for a download that failed
	_, err = New(github.NewMockClient(), g, &Options{Target: target}).SyncRepoWithData(context.Background(), localRepo("owner", "lfs", up))
	require.NoError(t, err)
	clone := filepath.Join(target, "owner", "lfs")
	oid := strings.Fields(runGit(t, "-C", clone, "lfs", "ls-files", "--all", "--long"))[0]
	object := filepath.Join(clone, ".git", "lfs", "objects", oid[0:2], oid[2:4], oid)
	require.NoFileExists(t, object)

	syncer := New(github.NewMockClient(), g, &Options{Target: target, LFSAll: true})
	result := NewResult()
	syncer.afterSync(context.Background(), localRepo("owner", "lfs", up), clone, false, result)
	assert.Empty(t, result.Warnings)
	assert.FileExists(t, object)

	// Once it succeeded, an unchanged repo is left alone
	require.NoError(t, os.Remove(object))
	syncer.afterSync(context.Background(), localRepo("owner", "lfs", up), clone, false, result)
	assert.NoFileExists(t, object)
}

// Unchanged repos skip the fetch, so the old token must be removed from the origin URL anyway
func TestAfterSync_RemovesOldTokenFromOrigin(t *testing.T) {
	g, err := git.NewQuiet()
	require.NoError(t, err)

	up := newUpstream(t, map[string]string{"a.txt": "a"})
	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target})
	_, err = syncer.SyncRepoWithData(context.Background(), localRepo("owner", "repo", up))
	require.NoError(t, err)
	clone := filepath.Join(target, "owner", "repo")
	runGit(t, "-C", clone, "remote", "set-url", "origin", "https://oauth2:old-token@github.com/owner/repo.git")

	result := NewResult()
	syncer.afterSync(context.Background(), localRepo("owner", "repo", up), clone, false, result)
	assert.Empty(t, result.Warnings)
	assert.Equal(t, "https://github.com/owner/repo.git", strings.TrimSpace(runGit(t, "-C", clone, "remote", "get-url", "origin")))
}
