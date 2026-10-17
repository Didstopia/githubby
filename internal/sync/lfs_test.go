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

	// Nothing new from fast sync: no LFS step at all
	result = NewResult()
	syncer.afterSync(context.Background(), localRepo("owner", "lfs", ""), repoDir, false, result)
	assert.Empty(t, result.Warnings)
}
