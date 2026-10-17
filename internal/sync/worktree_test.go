package sync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

// worktreeSyncer returns a syncer set up like the CLI backup, plus its target
func worktreeSyncer(t *testing.T) (*Syncer, string) {
	t.Helper()
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	target := t.TempDir()
	return New(github.NewMockClient(), g, &Options{Target: target, Submodules: true, UpdateWorkTree: true}), target
}

func syncOnce(t *testing.T, s *Syncer, repo *gh.Repository) *Result {
	t.Helper()
	result, err := s.SyncRepoWithData(context.Background(), repo)
	require.NoError(t, err)
	require.Empty(t, result.Failed)
	return result
}

func TestSync_FastForwardsCheckoutAndSubmodules(t *testing.T) {
	s, target := worktreeSyncer(t)
	parent := newUpstream(t, map[string]string{"README": "v1"})
	repo := localRepo("owner", "parent", parent)
	syncOnce(t, s, repo)

	// A newer commit adds a submodule
	child := newUpstream(t, map[string]string{"child.txt": "child"})
	runGit(t, "-C", parent, "submodule", "add", "-q", child, "lib/child")
	runGit(t, "-C", parent, "commit", "-q", "-m", "add submodule")

	result := syncOnce(t, s, repo)
	assert.Empty(t, result.Warnings)
	local := filepath.Join(target, "owner", "parent")
	assert.Equal(t, runGit(t, "-C", parent, "rev-parse", "HEAD"), runGit(t, "-C", local, "rev-parse", "HEAD"))
	body, err := os.ReadFile(filepath.Join(local, "lib", "child", "child.txt"))
	require.NoError(t, err)
	assert.Equal(t, "child", string(body))
}

func TestSync_FastForwardsWhenFastSyncSkips(t *testing.T) {
	s, target := worktreeSyncer(t)
	parent := newUpstream(t, map[string]string{"README": "v1"})
	repo := localRepo("owner", "parent", parent)
	syncOnce(t, s, repo)

	// An older version only fetched, so the checkout is behind while FETCH_HEAD is fresh
	require.NoError(t, os.WriteFile(filepath.Join(parent, "README"), []byte("v2"), 0644))
	runGit(t, "-C", parent, "commit", "-q", "-am", "v2")
	local := filepath.Join(target, "owner", "parent")
	runGit(t, "-C", local, "fetch", "-q", "--all")

	repo.PushedAt = &gh.Timestamp{Time: time.Now().Add(-time.Hour)}
	result := syncOnce(t, s, repo)
	assert.Equal(t, []string{"owner/parent"}, result.UpToDate)
	assert.Equal(t, runGit(t, "-C", parent, "rev-parse", "HEAD"), runGit(t, "-C", local, "rev-parse", "HEAD"))
}

func TestSync_DirtyWorkTreeIsLeftAlone(t *testing.T) {
	s, target := worktreeSyncer(t)
	parent := newUpstream(t, map[string]string{"README": "v1"})
	repo := localRepo("owner", "parent", parent)
	syncOnce(t, s, repo)

	local := filepath.Join(target, "owner", "parent")
	before := runGit(t, "-C", local, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(local, "README"), []byte("mine"), 0644))

	require.NoError(t, os.WriteFile(filepath.Join(parent, "README"), []byte("v2"), 0644))
	runGit(t, "-C", parent, "commit", "-q", "-am", "v2")

	result := syncOnce(t, s, repo)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "owner/parent: working tree: local changes, working tree not updated")
	assert.Equal(t, before, runGit(t, "-C", local, "rev-parse", "HEAD"))
	body, err := os.ReadFile(filepath.Join(local, "README"))
	require.NoError(t, err)
	assert.Equal(t, "mine", string(body))
	// The new commit is still backed up
	assert.Equal(t, runGit(t, "-C", parent, "rev-parse", "HEAD"), runGit(t, "-C", local, "rev-parse", "origin/main"))
}

func TestSync_SwitchesToNewDefaultBranch(t *testing.T) {
	s, target := worktreeSyncer(t)
	parent := newUpstream(t, map[string]string{"README": "v1"})
	repo := localRepo("owner", "parent", parent)
	repo.DefaultBranch = gh.Ptr("main")
	syncOnce(t, s, repo)

	// GitHub's default branch becomes trunk, which has a newer commit
	runGit(t, "-C", parent, "checkout", "-q", "-b", "trunk")
	require.NoError(t, os.WriteFile(filepath.Join(parent, "README"), []byte("trunk"), 0644))
	runGit(t, "-C", parent, "commit", "-q", "-am", "trunk")
	repo.DefaultBranch = gh.Ptr("trunk")

	result := syncOnce(t, s, repo)
	assert.Empty(t, result.Warnings)
	local := filepath.Join(target, "owner", "parent")
	assert.Equal(t, "trunk", runGit(t, "-C", local, "symbolic-ref", "--short", "HEAD"))
	assert.Equal(t, runGit(t, "-C", parent, "rev-parse", "trunk"), runGit(t, "-C", local, "rev-parse", "HEAD"))
	assert.Equal(t, "origin/trunk", strings.TrimSpace(runGit(t, "-C", local, "rev-parse", "--abbrev-ref", "trunk@{upstream}")))
}
