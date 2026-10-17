package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gh "github.com/google/go-github/v86/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Didstopia/githubby/internal/git"
	"github.com/Didstopia/githubby/internal/github"
)

// repoWithWiki creates an upstream repo plus a wiki repo next to it
func repoWithWiki(t *testing.T, withWiki bool) *gh.Repository {
	t.Helper()
	upstream := newUpstream(t, map[string]string{"README": "repo"})
	if withWiki {
		wiki := newUpstream(t, map[string]string{"Home.md": "wiki home"})
		require.NoError(t, os.Rename(wiki, upstream+".wiki.git"))
	}
	repo := localRepo("owner", "repo", upstream)
	repo.HasWiki = gh.Ptr(true)
	return repo
}

func TestSync_ClonesAndFetchesWiki(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	repo := repoWithWiki(t, true)
	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target, Wikis: true})

	result, err := syncer.SyncRepoWithData(context.Background(), repo)
	require.NoError(t, err)
	assert.Equal(t, []string{"owner/repo.wiki"}, result.Wikis)
	body, err := os.ReadFile(filepath.Join(target, "owner", "repo.wiki", "Home.md"))
	require.NoError(t, err)
	assert.Equal(t, "wiki home", string(body))

	// Second run fetches the existing wiki and doesn't call it archived
	result, err = syncer.SyncRepoWithData(context.Background(), repo)
	require.NoError(t, err)
	assert.Equal(t, []string{"owner/repo.wiki"}, result.Wikis)
	assert.Empty(t, result.Failed)
	assert.Empty(t, result.Archived)
}

func TestSync_MissingWikiIsSkippedQuietly(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}
	repo := repoWithWiki(t, false)
	target := t.TempDir()
	syncer := New(github.NewMockClient(), g, &Options{Target: target, Wikis: true})

	result, err := syncer.SyncRepoWithData(context.Background(), repo)
	require.NoError(t, err)
	assert.Equal(t, []string{"owner/repo"}, result.Cloned)
	assert.Empty(t, result.Wikis)
	assert.Empty(t, result.Failed)
	assert.Empty(t, result.Warnings)
}

func TestSync_WikiOffOrNoWikiFlag(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}

	t.Run("option off", func(t *testing.T) {
		target := t.TempDir()
		syncer := New(github.NewMockClient(), g, &Options{Target: target})
		result, err := syncer.SyncRepoWithData(context.Background(), repoWithWiki(t, true))
		require.NoError(t, err)
		assert.Empty(t, result.Wikis)
		assert.NoDirExists(t, filepath.Join(target, "owner", "repo.wiki"))
	})

	t.Run("repo has no wiki", func(t *testing.T) {
		target := t.TempDir()
		repo := repoWithWiki(t, true)
		repo.HasWiki = gh.Ptr(false)
		syncer := New(github.NewMockClient(), g, &Options{Target: target, Wikis: true})
		result, err := syncer.SyncRepoWithData(context.Background(), repo)
		require.NoError(t, err)
		assert.Empty(t, result.Wikis)
	})
}

func TestWikiHelpers(t *testing.T) {
	repo := createMockRepo("repo", "owner/repo", false)
	assert.Equal(t, "https://github.com/owner/repo.wiki.git", WikiURL(repo))
	assert.Equal(t, filepath.Join("t", "owner", "repo.wiki"), WikiPath("t", repo))

	assert.True(t, IsRepoNotFound(errors.New("fatal: repository 'https://github.com/o/r.wiki.git/' not found")))
	assert.True(t, IsRepoNotFound(errors.New("fatal: repository '/x' does not exist")))
	assert.True(t, IsRepoNotFound(errors.New("fatal: '/x.wiki.git' does not appear to be a git repository")))
	assert.False(t, IsRepoNotFound(errors.New("fatal: unable to access: timeout")))
}
