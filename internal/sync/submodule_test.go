package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

func TestSync_ChecksOutSubmodules(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}

	child := newUpstream(t, map[string]string{"child.txt": "child"})
	parent := newUpstream(t, map[string]string{"README": "parent"})
	runGit(t, "-C", parent, "submodule", "add", "-q", child, "lib/child")
	runGit(t, "-C", parent, "commit", "-q", "-m", "add submodule")

	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target, Submodules: true})

	result, err := syncer.SyncRepoWithData(context.Background(), localRepo("owner", "parent", parent))
	require.NoError(t, err)
	assert.Equal(t, []string{"owner/parent"}, result.Cloned)
	assert.Empty(t, result.Warnings)

	body, err := os.ReadFile(filepath.Join(target, "owner", "parent", "lib", "child", "child.txt"))
	require.NoError(t, err)
	assert.Equal(t, "child", string(body))
}

func TestSync_SubmoduleFailureIsWarning(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}

	child := newUpstream(t, map[string]string{"child.txt": "child"})
	parent := newUpstream(t, map[string]string{"README": "parent"})
	runGit(t, "-C", parent, "submodule", "add", "-q", child, "lib/child")
	runGit(t, "-C", parent, "commit", "-q", "-m", "add submodule")
	require.NoError(t, os.RemoveAll(child))

	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target, Submodules: true})

	result, err := syncer.SyncRepoWithData(context.Background(), localRepo("owner", "parent", parent))
	require.NoError(t, err)
	assert.Equal(t, []string{"owner/parent"}, result.Cloned)
	assert.Empty(t, result.Failed)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "owner/parent: submodules:")
}

func TestSync_SubmodulesOffByDefault(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}

	child := newUpstream(t, map[string]string{"child.txt": "child"})
	parent := newUpstream(t, map[string]string{"README": "parent"})
	runGit(t, "-C", parent, "submodule", "add", "-q", child, "lib/child")
	runGit(t, "-C", parent, "commit", "-q", "-m", "add submodule")

	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target})

	_, err = syncer.SyncRepoWithData(context.Background(), localRepo("owner", "parent", parent))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(target, "owner", "parent", "lib", "child", "child.txt"))
	assert.True(t, os.IsNotExist(err))
}
