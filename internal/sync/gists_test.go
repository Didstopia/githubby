package sync

import (
	"context"
	"encoding/json"
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

func gistMock(t *testing.T, gists ...*gh.Gist) *github.MockClient {
	m := github.NewMockClient()
	m.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) {
		return &gh.User{Login: gh.Ptr("Dids")}, nil
	}
	m.ListGistsFunc = func(ctx context.Context) ([]*gh.Gist, error) { return gists, nil }
	return m
}

func TestSyncGists(t *testing.T) {
	g, err := git.NewQuiet()
	if err != nil {
		t.Skip("git not installed")
	}

	up := newUpstream(t, map[string]string{"note.md": "secret note"})
	secret := &gh.Gist{ID: gh.Ptr("abc123"), Description: gh.Ptr("my secret"), Public: gh.Ptr(false), GitPullURL: gh.Ptr(up)}
	broken := &gh.Gist{ID: gh.Ptr("broken"), GitPullURL: gh.Ptr(filepath.Join(t.TempDir(), "missing"))}

	target := t.TempDir()
	syncer := New(gistMock(t, secret, broken), g, &Options{Target: target})

	result := NewResult()
	require.NoError(t, syncer.SyncGists(context.Background(), result))
	assert.Equal(t, []string{"gist abc123"}, result.Gists)
	assert.Contains(t, result.Failed, "gist broken")

	body, err := os.ReadFile(filepath.Join(target, "Dids.gists", "abc123", "note.md"))
	require.NoError(t, err)
	assert.Equal(t, "secret note", string(body))

	var meta map[string]any
	raw, err := os.ReadFile(filepath.Join(target, "Dids.gists", "abc123.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &meta))
	assert.Equal(t, "my secret", meta["description"])

	// Second run fetches, and the gist is not taken for an archived repo
	result = NewResult()
	require.NoError(t, New(gistMock(t, secret), g, &Options{Target: target}).SyncGists(context.Background(), result))
	assert.Equal(t, []string{"gist abc123"}, result.Gists)
	assert.Empty(t, result.Failed)

	repoResult, err := New(github.NewMockClient(), g, &Options{Target: target}).SyncRepos(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, repoResult.Archived)
}

func TestSyncGists_DryRunAndErrors(t *testing.T) {
	gist := &gh.Gist{ID: gh.Ptr("x"), GitPullURL: gh.Ptr("https://gist.github.com/x.git")}

	t.Run("dry run lists only", func(t *testing.T) {
		target := t.TempDir()
		result := NewResult()
		require.NoError(t, New(gistMock(t, gist), &git.Git{}, &Options{Target: target, DryRun: true}).SyncGists(context.Background(), result))
		assert.Equal(t, []string{"gist x"}, result.Gists)
		assert.NoDirExists(t, filepath.Join(target, "Dids"))
	})

	t.Run("list fails", func(t *testing.T) {
		m := gistMock(t)
		m.ListGistsFunc = func(ctx context.Context) ([]*gh.Gist, error) { return nil, errors.New("403") }
		err := New(m, &git.Git{}, &Options{}).SyncGists(context.Background(), NewResult())
		assert.ErrorContains(t, err, "failed to list gists")
	})

	t.Run("user lookup fails", func(t *testing.T) {
		m := gistMock(t)
		m.GetAuthenticatedUserFunc = func(ctx context.Context) (*gh.User, error) { return nil, errors.New("401") }
		err := New(m, &git.Git{}, &Options{}).SyncGists(context.Background(), NewResult())
		assert.ErrorContains(t, err, "authenticated user")
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := New(gistMock(t, gist), &git.Git{}, &Options{Target: t.TempDir()}).SyncGists(ctx, NewResult())
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestIsGistPath(t *testing.T) {
	assert.True(t, isGistPath("Dids.gists/abc"))
	assert.False(t, isGistPath("Dids.gists"))
	assert.False(t, isGistPath("Dids/repo"))
	assert.False(t, isGistPath("Dids/gists"), "a repo named gists is a repo")
	assert.False(t, isGistPath("Dids/gists/abc"))
}
